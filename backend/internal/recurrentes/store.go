package recurrentes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// columnas se repite en varias consultas; en una constante para que el SELECT
// y el Scan no se desincronicen al agregar un campo.
const columnas = `
	r.id, r.categoria_id, c.nombre, r.medio_pago_id, mp.nombre,
	r.tipo, r.monto::text, r.descripcion, r.frecuencia, r.dia,
	to_char(r.desde, 'YYYY-MM-DD'), to_char(r.hasta, 'YYYY-MM-DD'),
	r.activo, r.creado_en, r.actualizado_en,
	coalesce(p.cuantas, 0)`

const uniones = `
	JOIN categorias c   ON c.id  = r.categoria_id
	JOIN medios_pago mp ON mp.id = r.medio_pago_id
	LEFT JOIN LATERAL (
		SELECT count(*) AS cuantas
		FROM recurrentes_ocurrencias o
		WHERE o.recurrente_id = r.id AND o.estado = 'pendiente'
	) p ON true`

// --------------------------------------------------------------------------
// Las plantillas
// --------------------------------------------------------------------------

func (s *Store) Listar(ctx context.Context, usuarioID int64) ([]Recurrente, error) {
	q := `SELECT ` + columnas + ` FROM recurrentes r ` + uniones + `
		WHERE r.usuario_id = $1
		ORDER BY r.activo DESC, lower(r.descripcion), r.id`

	filas, err := s.db.QueryContext(ctx, q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando recurrentes: %w", err)
	}
	defer filas.Close()

	ahora := time.Now()
	lista := []Recurrente{}
	for filas.Next() {
		r, err := escanear(filas)
		if err != nil {
			return nil, err
		}
		// La proxima fecha se calcula en Go y no en SQL: es la misma funcion
		// de calendario que usa el generador, con sus meses de 28 dias y sus
		// quincenas. Tenerla dos veces escrita (una aqui y otra en SQL) seria
		// garantia de que algun dia digan cosas distintas.
		r.ProximaFecha = ProximoVencimiento(*r, ahora)
		lista = append(lista, *r)
	}
	return lista, filas.Err()
}

func (s *Store) PorID(ctx context.Context, usuarioID, id int64) (*Recurrente, error) {
	q := `SELECT ` + columnas + ` FROM recurrentes r ` + uniones + `
		WHERE r.id = $1 AND r.usuario_id = $2`

	r, err := escanear(s.db.QueryRowContext(ctx, q, id, usuarioID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, err
	}
	r.ProximaFecha = ProximoVencimiento(*r, time.Now())
	return r, nil
}

// Crear inserta la plantilla verificando en la MISMA consulta que la categoria
// y el medio sean del usuario.
//
// Mismo truco que en movimientos: el INSERT ... SELECT no inserta nada si las
// CTE no devuelven filas, y asi no hay una ventana entre "verificar" y
// "escribir" en la que la categoria pueda desaparecer.
func (s *Store) Crear(ctx context.Context, usuarioID int64, d Datos) (*Recurrente, error) {
	const q = `
		WITH cat AS (
			SELECT id FROM categorias WHERE id = $2 AND usuario_id = $1
		), medio AS (
			SELECT id FROM medios_pago WHERE id = $3 AND usuario_id = $1
		)
		INSERT INTO recurrentes
			(usuario_id, categoria_id, medio_pago_id, tipo, monto, descripcion,
			 frecuencia, dia, desde, hasta, activo)
		SELECT $1, cat.id, medio.id, $4, $5::numeric, $6, $7, $8, $9::date, $10::date, $11
		FROM cat, medio
		RETURNING id`

	var id int64
	err := s.db.QueryRowContext(ctx, q,
		usuarioID, d.CategoriaID, d.MedioPagoID, d.Tipo, d.Monto, d.Descripcion,
		d.Frecuencia, d.Dia, d.Desde, d.Hasta, d.Activo).Scan(&id)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, s.porQueNoEntro(ctx, usuarioID, d)
	}
	if err != nil {
		return nil, fmt.Errorf("creando recurrente: %w", err)
	}
	return s.PorID(ctx, usuarioID, id)
}

func (s *Store) Actualizar(ctx context.Context, usuarioID, id int64, d Datos) (*Recurrente, error) {
	const q = `
		WITH cat AS (
			SELECT id FROM categorias WHERE id = $3 AND usuario_id = $1
		), medio AS (
			SELECT id FROM medios_pago WHERE id = $4 AND usuario_id = $1
		)
		UPDATE recurrentes r
		SET categoria_id   = cat.id,
		    medio_pago_id  = medio.id,
		    tipo           = $5,
		    monto          = $6::numeric,
		    descripcion    = $7,
		    frecuencia     = $8,
		    dia            = $9,
		    desde          = $10::date,
		    hasta          = $11::date,
		    activo         = $12,
		    actualizado_en = now()
		FROM cat, medio
		WHERE r.id = $2 AND r.usuario_id = $1
		RETURNING r.id`

	var actualizado int64
	err := s.db.QueryRowContext(ctx, q,
		usuarioID, id, d.CategoriaID, d.MedioPagoID, d.Tipo, d.Monto, d.Descripcion,
		d.Frecuencia, d.Dia, d.Desde, d.Hasta, d.Activo).Scan(&actualizado)

	if errors.Is(err, sql.ErrNoRows) {
		existe, errExiste := s.existe(ctx, usuarioID, id)
		if errExiste != nil {
			return nil, errExiste
		}
		if !existe {
			return nil, ErrNoEncontrado
		}
		return nil, s.porQueNoEntro(ctx, usuarioID, d)
	}
	if err != nil {
		return nil, fmt.Errorf("actualizando recurrente: %w", err)
	}
	return s.PorID(ctx, usuarioID, id)
}

// Eliminar borra la plantilla y, en cascada, sus ocurrencias.
//
// Los MOVIMIENTOS que salieron de confirmarlas NO se borran: son plata que de
// verdad se movio. La ocurrencia era solo el recordatorio.
func (s *Store) Eliminar(ctx context.Context, usuarioID, id int64) error {
	const q = `DELETE FROM recurrentes WHERE id = $1 AND usuario_id = $2 RETURNING id`

	var borrado int64
	err := s.db.QueryRowContext(ctx, q, id, usuarioID).Scan(&borrado)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoEncontrado
	}
	if err != nil {
		return fmt.Errorf("eliminando recurrente: %w", err)
	}
	return nil
}

func (s *Store) porQueNoEntro(ctx context.Context, usuarioID int64, d Datos) error {
	var categoriaOK bool
	err := s.db.QueryRowContext(ctx,
		`SELECT exists(SELECT 1 FROM categorias WHERE id = $1 AND usuario_id = $2)`,
		d.CategoriaID, usuarioID).Scan(&categoriaOK)
	if err != nil {
		return fmt.Errorf("verificando categoria: %w", err)
	}
	if !categoriaOK {
		return ErrCategoriaInvalida
	}
	return ErrMedioInvalido
}

func (s *Store) existe(ctx context.Context, usuarioID, id int64) (bool, error) {
	var existe bool
	err := s.db.QueryRowContext(ctx,
		`SELECT exists(SELECT 1 FROM recurrentes WHERE id = $1 AND usuario_id = $2)`,
		id, usuarioID).Scan(&existe)
	if err != nil {
		return false, fmt.Errorf("verificando recurrente: %w", err)
	}
	return existe, nil
}

type escaneable interface {
	Scan(dest ...any) error
}

func escanear(fila escaneable) (*Recurrente, error) {
	var (
		r     Recurrente
		hasta sql.NullString
	)
	err := fila.Scan(
		&r.ID, &r.CategoriaID, &r.CategoriaNombre, &r.MedioPagoID, &r.MedioPagoNombre,
		&r.Tipo, &r.Monto, &r.Descripcion, &r.Frecuencia, &r.Dia,
		&r.Desde, &hasta, &r.Activo, &r.CreadoEn, &r.ActualizadoEn, &r.Pendientes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("leyendo recurrente: %w", err)
	}
	if hasta.Valid {
		r.Hasta = &hasta.String
	}
	return &r, nil
}

// --------------------------------------------------------------------------
// Las ocurrencias
// --------------------------------------------------------------------------

const columnasOcurrencia = `
	o.id, o.recurrente_id, to_char(o.fecha, 'YYYY-MM-DD'), o.estado,
	r.tipo, r.monto::text, r.descripcion,
	r.categoria_id, c.nombre, r.medio_pago_id, mp.nombre,
	o.movimiento_id, o.creada_en`

const unionesOcurrencia = `
	JOIN recurrentes r  ON r.id  = o.recurrente_id
	JOIN categorias c   ON c.id  = r.categoria_id
	JOIN medios_pago mp ON mp.id = r.medio_pago_id`

// Pendientes son las ocurrencias sin resolver del usuario, de la mas vieja a
// la mas nueva: lo primero que toca confirmar va arriba.
func (s *Store) Pendientes(ctx context.Context, usuarioID int64) ([]Ocurrencia, error) {
	q := `SELECT ` + columnasOcurrencia + ` FROM recurrentes_ocurrencias o ` + unionesOcurrencia + `
		WHERE o.usuario_id = $1 AND o.estado = 'pendiente'
		ORDER BY o.fecha, o.id`

	filas, err := s.db.QueryContext(ctx, q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando ocurrencias pendientes: %w", err)
	}
	defer filas.Close()

	lista := []Ocurrencia{}
	for filas.Next() {
		o, err := escanearOcurrencia(filas)
		if err != nil {
			return nil, err
		}
		lista = append(lista, *o)
	}
	return lista, filas.Err()
}

func (s *Store) Ocurrencia(ctx context.Context, usuarioID, id int64) (*Ocurrencia, error) {
	q := `SELECT ` + columnasOcurrencia + ` FROM recurrentes_ocurrencias o ` + unionesOcurrencia + `
		WHERE o.id = $1 AND o.usuario_id = $2`

	o, err := escanearOcurrencia(s.db.QueryRowContext(ctx, q, id, usuarioID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOcurrenciaNoExiste
	}
	return o, err
}

// Generar crea las ocurrencias que le faltan a un recurrente hasta `hasta`.
//
// El ON CONFLICT DO NOTHING contra el indice unico (recurrente_id, fecha) ES
// la regla de idempotencia. La tarea corre cada hora y vuelve a evaluarlo todo;
// lo que impide que el arriendo aparezca veinte veces es el indice, no que la
// tarea lleve bien la cuenta de por donde iba.
func (s *Store) Generar(ctx context.Context, r Recurrente, usuarioID int64, hasta time.Time) ([]Ocurrencia, error) {
	if !r.Activo {
		return nil, nil
	}

	desde := hasta.AddDate(0, 0, -VentanaDias)
	fechas := Vencimientos(r, desde, hasta)
	if len(fechas) == 0 {
		return nil, nil
	}

	const q = `
		INSERT INTO recurrentes_ocurrencias (usuario_id, recurrente_id, fecha)
		VALUES ($1, $2, $3::date)
		ON CONFLICT (recurrente_id, fecha) DO NOTHING
		RETURNING id`

	var nuevas []Ocurrencia
	for _, f := range fechas {
		var id int64
		err := s.db.QueryRowContext(ctx, q, usuarioID, r.ID, f.Format(formatoFecha)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue // ya existia: la tarea corriendo otra vez, no un fallo
		}
		if err != nil {
			return nuevas, fmt.Errorf("generando ocurrencia de %d: %w", r.ID, err)
		}

		o, err := s.Ocurrencia(ctx, usuarioID, id)
		if err != nil {
			return nuevas, err
		}
		nuevas = append(nuevas, *o)
	}
	return nuevas, nil
}

// Resolver marca la ocurrencia como confirmada o descartada.
//
// El UPDATE lleva `AND estado = 'pendiente'`: es lo que decide quien gana si
// llegan dos clics a la vez (o dos pestañas abiertas). El segundo se encuentra
// sin filas y recibe ErrYaResuelta, en vez de crear un segundo arriendo.
func (s *Store) Resolver(ctx context.Context, usuarioID, id int64, estado string, movimientoID *int64) error {
	const q = `
		UPDATE recurrentes_ocurrencias
		SET estado = $3, movimiento_id = $4, resuelta_en = now()
		WHERE id = $1 AND usuario_id = $2 AND estado = 'pendiente'
		RETURNING id`

	var resuelta int64
	err := s.db.QueryRowContext(ctx, q, id, usuarioID, estado, movimientoID).Scan(&resuelta)
	if errors.Is(err, sql.ErrNoRows) {
		existe, errExiste := s.existeOcurrencia(ctx, usuarioID, id)
		if errExiste != nil {
			return errExiste
		}
		if !existe {
			return ErrOcurrenciaNoExiste
		}
		return ErrYaResuelta
	}
	if err != nil {
		return fmt.Errorf("resolviendo ocurrencia: %w", err)
	}
	return nil
}

// DevolverAPendiente deshace la reserva cuando la escritura del movimiento
// fallo. Sin esto, la ocurrencia quedaria marcada como confirmada sin que
// exista el gasto, y no volveria a proponerse nunca.
func (s *Store) DevolverAPendiente(ctx context.Context, usuarioID, id int64) error {
	const q = `
		UPDATE recurrentes_ocurrencias
		SET estado = 'pendiente', movimiento_id = NULL, resuelta_en = NULL
		WHERE id = $1 AND usuario_id = $2`

	if _, err := s.db.ExecContext(ctx, q, id, usuarioID); err != nil {
		return fmt.Errorf("devolviendo la ocurrencia a pendiente: %w", err)
	}
	return nil
}

// AnotarMovimiento deja el rastro de que movimiento salio de esta ocurrencia.
func (s *Store) AnotarMovimiento(ctx context.Context, usuarioID, id, movimientoID int64) error {
	const q = `
		UPDATE recurrentes_ocurrencias
		SET movimiento_id = $3
		WHERE id = $1 AND usuario_id = $2`

	if _, err := s.db.ExecContext(ctx, q, id, usuarioID, movimientoID); err != nil {
		return fmt.Errorf("anotando el movimiento de la ocurrencia: %w", err)
	}
	return nil
}

func (s *Store) existeOcurrencia(ctx context.Context, usuarioID, id int64) (bool, error) {
	var existe bool
	err := s.db.QueryRowContext(ctx,
		`SELECT exists(SELECT 1 FROM recurrentes_ocurrencias WHERE id = $1 AND usuario_id = $2)`,
		id, usuarioID).Scan(&existe)
	if err != nil {
		return false, fmt.Errorf("verificando ocurrencia: %w", err)
	}
	return existe, nil
}

func escanearOcurrencia(fila escaneable) (*Ocurrencia, error) {
	var (
		o            Ocurrencia
		movimientoID sql.NullInt64
	)
	err := fila.Scan(
		&o.ID, &o.RecurrenteID, &o.Fecha, &o.Estado,
		&o.Tipo, &o.Monto, &o.Descripcion,
		&o.CategoriaID, &o.CategoriaNombre, &o.MedioPagoID, &o.MedioPagoNombre,
		&movimientoID, &o.CreadaEn,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("leyendo ocurrencia: %w", err)
	}
	if movimientoID.Valid {
		o.MovimientoID = &movimientoID.Int64
	}
	return &o, nil
}

// --------------------------------------------------------------------------
// Para la tarea de fondo
// --------------------------------------------------------------------------

// Activos son todos los recurrentes activos de TODOS los usuarios, con su
// dueño. Lo usa el generador, que no trabaja para nadie en particular.
func (s *Store) Activos(ctx context.Context) (map[int64][]Recurrente, error) {
	q := `SELECT r.usuario_id, ` + columnas + ` FROM recurrentes r ` + uniones + `
		JOIN usuarios u ON u.id = r.usuario_id
		WHERE r.activo AND u.activo
		ORDER BY r.usuario_id, r.id`

	filas, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listando recurrentes activos: %w", err)
	}
	defer filas.Close()

	porUsuario := map[int64][]Recurrente{}
	for filas.Next() {
		var (
			usuarioID int64
			r         Recurrente
			hasta     sql.NullString
		)
		err := filas.Scan(&usuarioID,
			&r.ID, &r.CategoriaID, &r.CategoriaNombre, &r.MedioPagoID, &r.MedioPagoNombre,
			&r.Tipo, &r.Monto, &r.Descripcion, &r.Frecuencia, &r.Dia,
			&r.Desde, &hasta, &r.Activo, &r.CreadoEn, &r.ActualizadoEn, &r.Pendientes,
		)
		if err != nil {
			return nil, fmt.Errorf("leyendo recurrente activo: %w", err)
		}
		if hasta.Valid {
			r.Hasta = &hasta.String
		}
		porUsuario[usuarioID] = append(porUsuario[usuarioID], r)
	}
	return porUsuario, filas.Err()
}
