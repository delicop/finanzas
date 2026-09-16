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

/* ----------------------------- planes ----------------------------------- */

func (s *Store) ListarPlanes(ctx context.Context) ([]Plan, error) {
	const q = `
		SELECT p.id, p.nombre, p.precio_mensual::text, p.activo, p.creado_en,
		       (SELECT count(*) FROM usuarios u WHERE u.plan_id = p.id)
		FROM planes p
		ORDER BY p.activo DESC, p.precio_mensual, lower(p.nombre)`

	filas, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listando planes: %w", err)
	}
	defer filas.Close()

	lista := []Plan{}
	for filas.Next() {
		var p Plan
		if err := filas.Scan(&p.ID, &p.Nombre, &p.PrecioMensual, &p.Activo,
			&p.CreadoEn, &p.Clientes); err != nil {
			return nil, fmt.Errorf("leyendo plan: %w", err)
		}
		lista = append(lista, p)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo planes: %w", err)
	}
	return lista, nil
}

func (s *Store) CrearPlan(ctx context.Context, nombre, precio string) (*Plan, error) {
	const q = `
		INSERT INTO planes (nombre, precio_mensual)
		VALUES ($1, $2::numeric)
		RETURNING id, nombre, precio_mensual::text, activo, creado_en, 0`

	var p Plan
	err := s.db.QueryRowContext(ctx, q, nombre, precio).
		Scan(&p.ID, &p.Nombre, &p.PrecioMensual, &p.Activo, &p.CreadoEn, &p.Clientes)
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("creando plan: %w", err)
	}
	return &p, nil
}

// ActualizarPlan cambia nombre, precio y estado.
//
// Subir el precio afecta lo que se ESPERA cobrar de aqui en adelante, nunca lo
// ya cobrado: los pagos guardan su propia copia del monto y del nombre del plan.
func (s *Store) ActualizarPlan(ctx context.Context, id int64, nombre, precio string, activo bool) (*Plan, error) {
	const q = `
		UPDATE planes
		SET nombre = $2, precio_mensual = $3::numeric, activo = $4, actualizado_en = now()
		WHERE id = $1
		RETURNING id, nombre, precio_mensual::text, activo, creado_en,
		          (SELECT count(*) FROM usuarios u WHERE u.plan_id = planes.id)`

	var p Plan
	err := s.db.QueryRowContext(ctx, q, id, nombre, precio, activo).
		Scan(&p.ID, &p.Nombre, &p.PrecioMensual, &p.Activo, &p.CreadoEn, &p.Clientes)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("actualizando plan: %w", err)
	}
	return &p, nil
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

// RegistrarPago deja constancia de que un cliente pago un mes.
//
// El monto y el nombre del plan NO se reciben del cliente HTTP: se leen de la
// base en la misma consulta. Si vinieran de afuera, un error en el formulario
// podria registrar que alguien pago $1 y cuadrar mal los ingresos para siempre.
// Si hay que cobrar algo distinto del precio de lista, para eso esta 'monto'
// como parametro opcional.
func (s *Store) RegistrarPago(ctx context.Context, usuarioID int64, periodo, monto, pagadoEn, nota string) (*Pago, error) {
	const q = `
		INSERT INTO pagos (usuario_id, cliente_email, plan_nombre, monto, periodo, pagado_en, nota)
		SELECT u.id, u.email, p.nombre,
		       coalesce(nullif($3, '')::numeric, p.precio_mensual),
		       $2::date, $4::date, $5
		FROM usuarios u
		JOIN planes p ON p.id = u.plan_id
		WHERE u.id = $1
		RETURNING id, usuario_id, cliente_email, plan_nombre, monto::text,
		          to_char(periodo, 'YYYY-MM'), to_char(pagado_en, 'YYYY-MM-DD'), nota`

	var pago Pago
	err := s.db.QueryRowContext(ctx, q, usuarioID, periodo, monto, pagadoEn, nota).
		Scan(&pago.ID, &pago.UsuarioID, &pago.ClienteEmail, &pago.PlanNombre,
			&pago.Monto, &pago.Periodo, &pago.PagadoEn, &pago.Nota)

	// Sin filas = el JOIN no encontro plan. O el usuario no existe, o no tiene
	// uno asignado; en ambos casos no hay nada que cobrarle.
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSinPlan
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrPagoDuplicado
		}
		return nil, fmt.Errorf("registrando pago: %w", err)
	}
	return &pago, nil
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

// ListarPagos devuelve los cobros de un mes (periodo AAAA-MM-01).
func (s *Store) ListarPagos(ctx context.Context, periodo string) ([]Pago, error) {
	const q = `
		SELECT id, usuario_id, cliente_email, plan_nombre, monto::text,
		       to_char(periodo, 'YYYY-MM'), to_char(pagado_en, 'YYYY-MM-DD'), nota
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
		var p Pago
		if err := filas.Scan(&p.ID, &p.UsuarioID, &p.ClienteEmail, &p.PlanNombre,
			&p.Monto, &p.Periodo, &p.PagadoEn, &p.Nota); err != nil {
			return nil, fmt.Errorf("leyendo pago: %w", err)
		}
		lista = append(lista, p)
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
// pagas un mes atrasado en este periodo, esa resta daria un pendiente negativo
// o escondido. Lo pendiente se calcula por separado: la suma de los planes de
// los clientes activos que NO tienen un pago registrado este mes. Asi la cifra
// responde a "¿a quien le falta cobrarle?", que es la pregunta real.
func (s *Store) Resumen(ctx context.Context, periodo string) (*Resumen, error) {
	r := Resumen{Periodo: periodo[:7]}

	const qTotales = `
		SELECT
		  (SELECT count(*) FROM usuarios WHERE activo AND rol = 'usuario'),
		  (SELECT count(*) FROM usuarios WHERE activo AND plan_id IS NOT NULL),
		  coalesce((SELECT sum(p.precio_mensual)
		            FROM usuarios u JOIN planes p ON p.id = u.plan_id
		            WHERE u.activo), 0)::text,
		  coalesce((SELECT sum(monto) FROM pagos WHERE periodo = $1::date), 0)::text,
		  (SELECT count(*) FROM pagos WHERE periodo = $1::date),
		  coalesce((SELECT sum(p.precio_mensual)
		            FROM usuarios u JOIN planes p ON p.id = u.plan_id
		            WHERE u.activo
		              AND NOT EXISTS (SELECT 1 FROM pagos g
		                              WHERE g.usuario_id = u.id AND g.periodo = $1::date)), 0)::text`

	if err := s.db.QueryRowContext(ctx, qTotales, periodo).Scan(
		&r.ClientesActivos, &r.ClientesConPlan, &r.Esperado,
		&r.Cobrado, &r.ClientesPagaron, &r.Pendiente,
	); err != nil {
		return nil, fmt.Errorf("calculando resumen del negocio: %w", err)
	}

	const qPorPlan = `
		SELECT p.id, p.nombre, p.precio_mensual::text,
		       count(u.id),
		       coalesce(sum(p.precio_mensual) FILTER (WHERE u.id IS NOT NULL), 0)::text,
		       coalesce((SELECT sum(g.monto) FROM pagos g
		                 WHERE g.periodo = $1::date AND g.plan_nombre = p.nombre), 0)::text
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
		if err := filas.Scan(&rp.PlanID, &rp.Nombre, &rp.Precio,
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

// Pendientes son los clientes activos con plan a los que falta cobrarles el mes.
func (s *Store) Pendientes(ctx context.Context, periodo string) ([]Pendiente, error) {
	const q = `
		SELECT u.id, u.email, u.nombre, p.nombre, p.precio_mensual::text
		FROM usuarios u
		JOIN planes p ON p.id = u.plan_id
		WHERE u.activo
		  AND NOT EXISTS (SELECT 1 FROM pagos g
		                  WHERE g.usuario_id = u.id AND g.periodo = $1::date)
		ORDER BY p.precio_mensual DESC, lower(u.email)`

	filas, err := s.db.QueryContext(ctx, q, periodo)
	if err != nil {
		return nil, fmt.Errorf("listando pendientes: %w", err)
	}
	defer filas.Close()

	lista := []Pendiente{}
	for filas.Next() {
		var p Pendiente
		if err := filas.Scan(&p.UsuarioID, &p.Email, &p.Nombre, &p.PlanNombre, &p.Monto); err != nil {
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
