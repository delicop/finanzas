package suscripciones

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Aqui NO hay filtro por usuario_id, y es a proposito: estas tablas no son de
// nadie, son del servidor. Lo que las protege es RequireAdmin en el router.

/* --------------------------- piezas de SQL ------------------------------ */

// precioDelCiclo es lo que le toca pagar a un cliente de una vez, segun como
// paga su plan. Espera los alias u (usuarios) y p (planes).
const precioDelCiclo = `CASE WHEN u.ciclo_pago = 'anual' THEN p.precio_anual ELSE p.precio_mensual END`

// aporteMensual es lo que un cliente vale por mes: su precio mensual, o la
// doceava parte del anual. Es la pieza del MRR.
//
// Se redondea a centavos en Postgres (NUMERIC) y no en Go: 120.000 / 12 da
// exacto, pero 100.000 / 12 no, y ese redondeo tiene que ser siempre el mismo.
const aporteMensual = `round(CASE WHEN u.ciclo_pago = 'anual' THEN p.precio_anual / 12 ELSE p.precio_mensual END, 2)`

// cubierto dice si el cliente u tiene un pago que incluye el mes $1.
// Los rangos son [periodo, cubre_hasta): el mes de cubre_hasta ya no entra.
const cubierto = `EXISTS (SELECT 1 FROM pagos g
                          WHERE g.usuario_id = u.id
                            AND g.periodo <= $1::date
                            AND g.cubre_hasta > $1::date)`

/* ----------------------------- planes ----------------------------------- */

const columnasPlan = `p.id, p.nombre, p.precio_mensual::text,
	coalesce(p.precio_anual::text, ''), p.incluye_ia, p.activo, p.creado_en,
	(SELECT count(*) FROM usuarios u WHERE u.plan_id = p.id)`

func escanearPlan(fila interface{ Scan(...any) error }) (*Plan, error) {
	var p Plan
	err := fila.Scan(&p.ID, &p.Nombre, &p.PrecioMensual, &p.PrecioAnual,
		&p.IncluyeIA, &p.Activo, &p.CreadoEn, &p.Clientes)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListarPlanes(ctx context.Context) ([]Plan, error) {
	q := `SELECT ` + columnasPlan + `
		FROM planes p
		ORDER BY p.activo DESC, p.precio_mensual, lower(p.nombre)`

	filas, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listando planes: %w", err)
	}
	defer filas.Close()

	lista := []Plan{}
	for filas.Next() {
		p, err := escanearPlan(filas)
		if err != nil {
			return nil, fmt.Errorf("leyendo plan: %w", err)
		}
		lista = append(lista, *p)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo planes: %w", err)
	}
	return lista, nil
}

// DatosPlan son los campos editables de un plan, ya validados.
type DatosPlan struct {
	Nombre        string
	PrecioMensual string
	PrecioAnual   string // vacio = sin opcion anual
	IncluyeIA     bool
	Activo        bool
}

func (s *Store) CrearPlan(ctx context.Context, d DatosPlan) (*Plan, error) {
	q := `
		WITH p AS (
			INSERT INTO planes (nombre, precio_mensual, precio_anual, incluye_ia)
			VALUES ($1, $2::numeric, nullif($3, '')::numeric, $4)
			RETURNING *
		)
		SELECT ` + columnasPlan + ` FROM p`

	p, err := escanearPlan(s.db.QueryRowContext(ctx, q, d.Nombre, d.PrecioMensual, d.PrecioAnual, d.IncluyeIA))
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("creando plan: %w", err)
	}
	return p, nil
}

// ActualizarPlan cambia nombre, precios, IA y estado.
//
// Subir el precio afecta lo que se ESPERA cobrar de aqui en adelante, nunca lo
// ya cobrado: los pagos guardan su propia copia del monto y del nombre del plan.
//
// Quitar la IA corta el asistente a todos los clientes del plan desde ya: se
// revisa en cada mensaje, no al iniciar sesion.
//
// Quitar el precio anual se rechaza si alguien lo esta pagando por año: ese
// cliente quedaria con un ciclo que su plan ya no ofrece y sin precio que
// cobrarle. Primero hay que pasarlo a mensual.
func (s *Store) ActualizarPlan(ctx context.Context, id int64, d DatosPlan) (*Plan, error) {
	q := `
		WITH p AS (
			UPDATE planes
			SET nombre = $2, precio_mensual = $3::numeric,
			    precio_anual = nullif($4, '')::numeric,
			    incluye_ia = $5, activo = $6, actualizado_en = now()
			WHERE id = $1
			  AND ($4 <> '' OR NOT EXISTS (
			        SELECT 1 FROM usuarios WHERE plan_id = $1 AND ciclo_pago = 'anual'))
			RETURNING *
		)
		SELECT ` + columnasPlan + ` FROM p`

	p, err := escanearPlan(s.db.QueryRowContext(ctx, q, id, d.Nombre, d.PrecioMensual,
		d.PrecioAnual, d.IncluyeIA, d.Activo))

	if errors.Is(err, sql.ErrNoRows) {
		// Sin filas: o el plan no existe, o lo freno la regla de los anuales.
		var existe bool
		if e := s.db.QueryRowContext(ctx,
			`SELECT exists(SELECT 1 FROM planes WHERE id = $1)`, id).Scan(&existe); e != nil {
			return nil, fmt.Errorf("verificando plan: %w", e)
		}
		if !existe {
			return nil, ErrNoEncontrado
		}
		return nil, ErrPlanConAnuales
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("actualizando plan: %w", err)
	}
	return p, nil
}

func (s *Store) EliminarPlan(ctx context.Context, id int64) error {
	resultado, err := s.db.ExecContext(ctx, `DELETE FROM planes WHERE id = $1`, id)
	if err != nil {
		// 23503 = foreign_key_violation: el RESTRICT de usuarios.plan_id.
		// Es exactamente lo que queremos que pase, traducido a un 409 con
		// un mensaje que se entienda.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrPlanEnUso
		}
		return fmt.Errorf("eliminando plan: %w", err)
	}
	filas, err := resultado.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando borrado de plan: %w", err)
	}
	if filas == 0 {
		return ErrNoEncontrado
	}
	return nil
}

/* ------------------------------ pagos ----------------------------------- */

const columnasPago = `id, usuario_id, cliente_email, plan_nombre, monto::text, ciclo,
	to_char(periodo, 'YYYY-MM'),
	to_char(cubre_hasta - interval '1 month', 'YYYY-MM'),
	to_char(pagado_en, 'YYYY-MM-DD'), nota`

func escanearPago(fila interface{ Scan(...any) error }) (*Pago, error) {
	var p Pago
	err := fila.Scan(&p.ID, &p.UsuarioID, &p.ClienteEmail, &p.PlanNombre, &p.Monto,
		&p.Ciclo, &p.Periodo, &p.CubreHasta, &p.PagadoEn, &p.Nota)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// RegistrarPago deja constancia de que un cliente pago, desde el mes periodo.
//
// El ciclo, el monto y el nombre del plan NO se reciben del cliente HTTP: se
// leen de la base en la misma consulta. Si vinieran de afuera, un error en el
// formulario podria registrar que alguien pago $1 y cuadrar mal los ingresos
// para siempre. Si hay que cobrar algo distinto del precio de lista, para eso
// esta 'monto' como parametro opcional.
//
// Un pago mensual cubre ese mes; uno anual, ese mes y los once siguientes.
// Que dos pagos no cubran el mismo mes lo garantiza la base (pagos_sin_solapar).
func (s *Store) RegistrarPago(ctx context.Context, usuarioID int64, periodo, monto, pagadoEn, nota string) (*Pago, error) {
	q := `
		INSERT INTO pagos (usuario_id, cliente_email, plan_nombre, monto, ciclo,
		                   periodo, cubre_hasta, pagado_en, nota)
		SELECT u.id, u.email, p.nombre,
		       coalesce(nullif($3, '')::numeric, ` + precioDelCiclo + `),
		       u.ciclo_pago,
		       $2::date,
		       ($2::date + CASE WHEN u.ciclo_pago = 'anual'
		                        THEN interval '12 months'
		                        ELSE interval '1 month' END)::date,
		       $4::date, $5
		FROM usuarios u
		JOIN planes p ON p.id = u.plan_id
		WHERE u.id = $1
		  -- Un anual sin precio anual no tiene que cobrarse. AsignarPlan y
		  -- ActualizarPlan impiden llegar aqui, pero si pasara, se trata como
		  -- "sin plan" en vez de insertar un monto nulo.
		  AND (u.ciclo_pago = 'mensual' OR p.precio_anual IS NOT NULL)
		RETURNING ` + columnasPago

	pago, err := escanearPago(s.db.QueryRowContext(ctx, q, usuarioID, periodo, monto, pagadoEn, nota))

	// Sin filas = el JOIN no encontro plan. O el usuario no existe, o no tiene
	// uno asignado; en ambos casos no hay nada que cobrarle.
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSinPlan
	}
	if err != nil {
		if esSolapado(err) {
			return nil, ErrPagoDuplicado
		}
		return nil, fmt.Errorf("registrando pago: %w", err)
	}
	return pago, nil
}

func (s *Store) EliminarPago(ctx context.Context, id int64) error {
	resultado, err := s.db.ExecContext(ctx, `DELETE FROM pagos WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("eliminando pago: %w", err)
	}
	filas, err := resultado.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando borrado de pago: %w", err)
	}
	if filas == 0 {
		return ErrNoEncontrado
	}
	return nil
}

// ListarPagos devuelve los cobros REGISTRADOS en un mes (periodo AAAA-MM-01).
//
// Es la caja de ese mes: un pago anual hecho en enero aparece en enero y no en
// los once meses siguientes, aunque los cubra.
func (s *Store) ListarPagos(ctx context.Context, periodo string) ([]Pago, error) {
	q := `SELECT ` + columnasPago + `
		FROM pagos
		WHERE periodo = $1::date
		ORDER BY pagado_en DESC, id DESC`

	filas, err := s.db.QueryContext(ctx, q, periodo)
	if err != nil {
		return nil, fmt.Errorf("listando pagos: %w", err)
	}
	defer filas.Close()

	lista := []Pago{}
	for filas.Next() {
		p, err := escanearPago(filas)
		if err != nil {
			return nil, fmt.Errorf("leyendo pago: %w", err)
		}
		lista = append(lista, *p)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo pagos: %w", err)
	}
	return lista, nil
}

/* ----------------------------- resumen ---------------------------------- */

// Resumen arma el tablero del negocio para un mes.
//
// Sobre "pendiente": NO es esperado - cobrado. Si un cliente te paga de mas, o
// paga un año entero este mes, esa resta daria un pendiente negativo o
// escondido. Lo pendiente se calcula por separado: lo que le toca pagar a cada
// cliente activo que NO tiene este mes cubierto. Asi la cifra responde a "¿a
// quien le falta cobrarle, y cuanto?", que es la pregunta real.
func (s *Store) Resumen(ctx context.Context, periodo string) (*Resumen, error) {
	r := Resumen{Periodo: periodo[:7]}

	qTotales := `
		SELECT
		  (SELECT count(*) FROM usuarios WHERE activo AND rol = 'usuario'),
		  (SELECT count(*) FROM usuarios WHERE activo AND plan_id IS NOT NULL),
		  (SELECT count(*) FROM usuarios WHERE activo AND plan_id IS NOT NULL
		                                   AND ciclo_pago = 'anual'),
		  coalesce((SELECT sum(` + aporteMensual + `)
		            FROM usuarios u JOIN planes p ON p.id = u.plan_id
		            WHERE u.activo), 0)::numeric(14,2)::text,
		  coalesce((SELECT sum(monto) FROM pagos WHERE periodo = $1::date), 0)::numeric(14,2)::text,
		  (SELECT count(*) FROM usuarios u
		   WHERE u.activo AND u.plan_id IS NOT NULL AND ` + cubierto + `),
		  coalesce((SELECT sum(` + precioDelCiclo + `)
		            FROM usuarios u JOIN planes p ON p.id = u.plan_id
		            WHERE u.activo AND NOT ` + cubierto + `), 0)::numeric(14,2)::text`

	if err := s.db.QueryRowContext(ctx, qTotales, periodo).Scan(
		&r.ClientesActivos, &r.ClientesConPlan, &r.ClientesAnuales, &r.Esperado,
		&r.Cobrado, &r.ClientesPagaron, &r.Pendiente,
	); err != nil {
		return nil, fmt.Errorf("calculando resumen del negocio: %w", err)
	}

	qPorPlan := `
		SELECT p.id, p.nombre, p.precio_mensual::text, coalesce(p.precio_anual::text, ''),
		       count(u.id),
		       coalesce(sum(` + aporteMensual + `) FILTER (WHERE u.id IS NOT NULL), 0)::numeric(14,2)::text,
		       coalesce((SELECT sum(g.monto) FROM pagos g
		                 WHERE g.periodo = $1::date AND g.plan_nombre = p.nombre), 0)::numeric(14,2)::text
		FROM planes p
		LEFT JOIN usuarios u ON u.plan_id = p.id AND u.activo
		GROUP BY p.id
		ORDER BY p.precio_mensual DESC, lower(p.nombre)`

	filas, err := s.db.QueryContext(ctx, qPorPlan, periodo)
	if err != nil {
		return nil, fmt.Errorf("resumen por plan: %w", err)
	}
	defer filas.Close()

	r.PorPlan = []ResumenPlan{}
	for filas.Next() {
		var rp ResumenPlan
		if err := filas.Scan(&rp.PlanID, &rp.Nombre, &rp.Precio, &rp.PrecioAnual,
			&rp.Clientes, &rp.Esperado, &rp.Cobrado); err != nil {
			return nil, fmt.Errorf("leyendo resumen de plan: %w", err)
		}
		r.PorPlan = append(r.PorPlan, rp)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo resumen por plan: %w", err)
	}

	pendientes, err := s.Pendientes(ctx, periodo)
	if err != nil {
		return nil, err
	}
	r.Pendientes = pendientes

	return &r, nil
}

// Pendientes son los clientes activos con plan que no tienen cubierto el mes.
func (s *Store) Pendientes(ctx context.Context, periodo string) ([]Pendiente, error) {
	q := `
		SELECT u.id, u.email, u.nombre, p.nombre, u.ciclo_pago,
		       (` + precioDelCiclo + `)::text
		FROM usuarios u
		JOIN planes p ON p.id = u.plan_id
		WHERE u.activo AND NOT ` + cubierto + `
		ORDER BY ` + precioDelCiclo + ` DESC, lower(u.email)`

	filas, err := s.db.QueryContext(ctx, q, periodo)
	if err != nil {
		return nil, fmt.Errorf("listando pendientes: %w", err)
	}
	defer filas.Close()

	lista := []Pendiente{}
	for filas.Next() {
		var p Pendiente
		if err := filas.Scan(&p.UsuarioID, &p.Email, &p.Nombre, &p.PlanNombre,
			&p.Ciclo, &p.Monto); err != nil {
			return nil, fmt.Errorf("leyendo pendiente: %w", err)
		}
		lista = append(lista, p)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo pendientes: %w", err)
	}
	return lista, nil
}

func esViolacionUnica(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// esSolapado: 23P01 = exclusion_violation, la de pagos_sin_solapar.
func esSolapado(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23P01" || pgErr.Code == "23505")
}
