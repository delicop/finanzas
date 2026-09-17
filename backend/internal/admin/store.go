// Package admin es el panel del dueno del servidor: crear usuarios, resetear
// contrasenas y desactivar cuentas.
//
// Es un paquete aparte de auth porque responde otra pregunta. auth contesta
// "¿quien eres y puedes entrar?"; admin contesta "¿quienes existen y que
// puede hacer cada uno?". Mezclarlos dejaria las rutas de administracion
// colgando del mismo router que el login, que es publico.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"finanzas/internal/auth"
)

var (
	ErrNoEncontrado = errors.New("usuario no encontrado")

	// ErrUltimoAdmin protege la unica puerta de entrada al panel. Sin esta
	// regla se puede dejar el servidor sin ningun administrador activo, y
	// entonces la unica forma de volver a entrar es por psql a mano.
	ErrUltimoAdmin = errors.New("debe quedar al menos un administrador activo")

	ErrPlanNoExiste = errors.New("el plan no existe")
	// Se pidio cobro anual en un plan que no tiene precio anual.
	ErrPlanSinAnual = errors.New("el plan no se vende por año")
)

// UsuarioAdmin es la ficha que ve el administrador: el usuario mas cuanta
// informacion tiene cargada, para saber a quien esta tocando antes de tocarlo.
type UsuarioAdmin struct {
	auth.Usuario
	Movimientos int `json:"movimientos"`
	Categorias  int `json:"categorias"`

	// El plan que le cobras. Nulo mientras no le asignes uno: los usuarios que
	// ya existian no tienen, y a un admin no te cobras a ti mismo.
	PlanID     *int64 `json:"plan_id"`
	PlanNombre string `json:"plan_nombre"`
	// Lo que paga de una vez segun su ciclo: el precio mensual o el anual.
	PlanPrecio string `json:"plan_precio"`
	// "mensual" o "anual".
	Ciclo string `json:"ciclo"`
	// Si su plan incluye el asistente.
	PlanIA bool `json:"plan_ia"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Listar devuelve todas las cuentas con su conteo de datos.
//
// Los conteos van como subconsultas y no como JOIN + GROUP BY: con dos tablas
// distintas colgando del mismo usuario, el JOIN multiplica las filas y los
// totales salen inflados (cada movimiento se contaria una vez por categoria).
func (s *Store) Listar(ctx context.Context) ([]UsuarioAdmin, error) {
	const q = `
		SELECT u.id, u.email, u.nombre, u.rol, u.activo, u.creado_en,
		       (SELECT count(*) FROM movimientos m WHERE m.usuario_id = u.id),
		       (SELECT count(*) FROM categorias  c WHERE c.usuario_id = u.id),
		       u.plan_id, coalesce(p.nombre, ''),
		       coalesce((CASE WHEN u.ciclo_pago = 'anual' THEN p.precio_anual
		                      ELSE p.precio_mensual END)::text, ''),
		       u.ciclo_pago, coalesce(p.incluye_ia, false)
		FROM usuarios u
		LEFT JOIN planes p ON p.id = u.plan_id
		ORDER BY u.creado_en`

	filas, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listando usuarios: %w", err)
	}
	defer filas.Close()

	// Slice inicializado (no nil) para que el JSON sea [] y no null.
	lista := []UsuarioAdmin{}
	for filas.Next() {
		var u UsuarioAdmin
		if err := filas.Scan(&u.ID, &u.Email, &u.Nombre, &u.Rol, &u.Activo,
			&u.CreadoEn, &u.Movimientos, &u.Categorias,
			&u.PlanID, &u.PlanNombre, &u.PlanPrecio, &u.Ciclo, &u.PlanIA); err != nil {
			return nil, fmt.Errorf("leyendo usuario: %w", err)
		}
		lista = append(lista, u)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo usuarios: %w", err)
	}
	return lista, nil
}

// Cambios son los campos que el admin puede tocar de otra cuenta.
// Punteros para distinguir "no lo mando" de "lo mando vacio o en false".
type Cambios struct {
	Nombre *string
	Rol    *string
	Activo *bool
}

// Actualizar aplica los cambios garantizando que no se quede sin admins.
//
// Todo va dentro de una transaccion con un candado previo sobre las filas de
// los administradores activos. Sin ese FOR UPDATE, dos peticiones simultaneas
// que degradan a dos admins distintos veria cada una que "todavia queda el
// otro" y las dos pasarian: el servidor terminaria sin ninguno.
func (s *Store) Actualizar(ctx context.Context, id int64, c Cambios) (*UsuarioAdmin, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion: %w", err)
	}
	// Rollback despues de un Commit exitoso no hace nada: es el patron normal
	// para no tener que acordarse de deshacer en cada return de error.
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`SELECT 1 FROM usuarios WHERE rol = $1 AND activo FOR UPDATE`, auth.RolAdmin,
	); err != nil {
		return nil, fmt.Errorf("bloqueando administradores: %w", err)
	}

	const q = `
		UPDATE usuarios
		SET nombre = coalesce($2, nombre),
		    rol    = coalesce($3, rol),
		    activo = coalesce($4, activo)
		WHERE id = $1
		RETURNING id`

	var actualizado int64
	err = tx.QueryRowContext(ctx, q, id, c.Nombre, c.Rol, c.Activo).Scan(&actualizado)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("actualizando usuario: %w", err)
	}

	var admins int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM usuarios WHERE rol = $1 AND activo`, auth.RolAdmin,
	).Scan(&admins); err != nil {
		return nil, fmt.Errorf("contando administradores: %w", err)
	}
	if admins == 0 {
		return nil, ErrUltimoAdmin
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando cambios: %w", err)
	}

	return s.PorID(ctx, id)
}

func (s *Store) PorID(ctx context.Context, id int64) (*UsuarioAdmin, error) {
	const q = `
		SELECT u.id, u.email, u.nombre, u.rol, u.activo, u.creado_en,
		       (SELECT count(*) FROM movimientos m WHERE m.usuario_id = u.id),
		       (SELECT count(*) FROM categorias  c WHERE c.usuario_id = u.id),
		       u.plan_id, coalesce(p.nombre, ''),
		       coalesce((CASE WHEN u.ciclo_pago = 'anual' THEN p.precio_anual
		                      ELSE p.precio_mensual END)::text, ''),
		       u.ciclo_pago, coalesce(p.incluye_ia, false)
		FROM usuarios u
		LEFT JOIN planes p ON p.id = u.plan_id
		WHERE u.id = $1`

	var u UsuarioAdmin
	err := s.db.QueryRowContext(ctx, q, id).Scan(&u.ID, &u.Email, &u.Nombre, &u.Rol,
		&u.Activo, &u.CreadoEn, &u.Movimientos, &u.Categorias,
		&u.PlanID, &u.PlanNombre, &u.PlanPrecio, &u.Ciclo, &u.PlanIA)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando usuario: %w", err)
	}
	return &u, nil
}

// Eliminar borra la cuenta y TODO lo que cuelga de ella.
//
// Las filas se las lleva el ON DELETE CASCADE de la base, pero las facturas
// viven en el disco y ahi no llega ninguna llave foranea: por eso la funcion
// devuelve sus rutas, para que el handler las borre. Si no, cada cliente
// eliminado dejaria sus archivos ocupando la tarjeta de la Raspberry para
// siempre, sin una sola fila que dijera de quien eran.
//
// El orden importa: primero se leen las rutas, despues se borra. Al reves ya
// no habria a quien preguntarle cuales eran.
func (s *Store) Eliminar(ctx context.Context, id int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Mismo candado que en Actualizar: sin el, dos borrados simultaneos de dos
	// admins distintos verian cada uno que "todavia queda el otro".
	if _, err := tx.ExecContext(ctx,
		`SELECT 1 FROM usuarios WHERE rol = $1 AND activo FOR UPDATE`, auth.RolAdmin,
	); err != nil {
		return nil, fmt.Errorf("bloqueando administradores: %w", err)
	}

	filas, err := tx.QueryContext(ctx,
		`SELECT factura_ruta FROM movimientos
		 WHERE usuario_id = $1 AND factura_ruta IS NOT NULL`, id)
	if err != nil {
		return nil, fmt.Errorf("listando facturas del usuario: %w", err)
	}

	rutas := []string{}
	for filas.Next() {
		var ruta string
		if err := filas.Scan(&ruta); err != nil {
			filas.Close()
			return nil, fmt.Errorf("leyendo ruta de factura: %w", err)
		}
		rutas = append(rutas, ruta)
	}
	if err := filas.Err(); err != nil {
		filas.Close()
		return nil, fmt.Errorf("recorriendo facturas: %w", err)
	}
	filas.Close()

	var borrado int64
	err = tx.QueryRowContext(ctx, `DELETE FROM usuarios WHERE id = $1 RETURNING id`, id).Scan(&borrado)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("eliminando usuario: %w", err)
	}

	var admins int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM usuarios WHERE rol = $1 AND activo`, auth.RolAdmin,
	).Scan(&admins); err != nil {
		return nil, fmt.Errorf("contando administradores: %w", err)
	}
	if admins == 0 {
		return nil, ErrUltimoAdmin
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando borrado: %w", err)
	}

	return rutas, nil
}

// AsignarPlan pone (o quita, con nil) el plan que se le cobra a un cliente, y
// como lo paga: por mes o por año.
//
// Va en su propio metodo y no dentro de Cambios porque el plan tiene un tercer
// estado: "quitarselo". Con un PATCH parcial, null significaria a la vez "no me
// lo mandaste" y "ponlo en nulo", y esas dos cosas no pueden confundirse cuando
// lo que esta en juego es si a alguien se le sigue cobrando o no.
//
// Sin plan, el ciclo vuelve a mensual: no queda un "anual" colgando que
// sorprenda el dia que se le asigne otro plan.
func (s *Store) AsignarPlan(ctx context.Context, id int64, planID *int64, ciclo string) (*UsuarioAdmin, error) {
	if planID == nil {
		ciclo = "mensual"
	}

	// La regla "anual solo si el plan tiene precio anual" va en el WHERE de la
	// misma consulta que hace el cambio: no hay ventana entre revisar y escribir.
	const q = `
		UPDATE usuarios SET plan_id = $2, ciclo_pago = $3
		WHERE id = $1
		  AND ($3 = 'mensual' OR EXISTS (
		        SELECT 1 FROM planes WHERE id = $2 AND precio_anual IS NOT NULL))
		RETURNING id`

	var actualizado int64
	err := s.db.QueryRowContext(ctx, q, id, planID, ciclo).Scan(&actualizado)
	if errors.Is(err, sql.ErrNoRows) {
		// Sin filas: el usuario no existe, o el plan no se vende por año
		// (o no existe, que para este caso es lo mismo que no tener anual).
		if _, errPorID := s.PorID(ctx, id); errPorID != nil {
			return nil, errPorID
		}
		var existe bool
		if e := s.db.QueryRowContext(ctx,
			`SELECT exists(SELECT 1 FROM planes WHERE id = $1)`, planID).Scan(&existe); e != nil {
			return nil, fmt.Errorf("verificando plan: %w", e)
		}
		if !existe {
			return nil, ErrPlanNoExiste
		}
		return nil, ErrPlanSinAnual
	}
	if err != nil {
		// 23503 = foreign_key_violation: mandaron un plan que no existe.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, ErrPlanNoExiste
		}
		return nil, fmt.Errorf("asignando plan: %w", err)
	}

	return s.PorID(ctx, id)
}
