// Package registro guarda en la base de datos los errores del servidor.
//
// A proposito NO hay pantalla para verlos: al cliente no le sirve de nada
// enterarse de una falla tecnica y mostrarsela solo genera ruido.
//
// Si se consultan desde afuera (curl) con un TOKEN DE MANTENIMIENTO aparte,
// distinto del login del cliente: asi la sesion del cliente no puede llegar
// a ellos ni por URL. Ver README, seccion "Revisar errores".
package registro

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Error es una falla del servidor. Se guarda en la base y se consulta
// desde afuera con el token de mantenimiento (ver middleware.go).
type Error struct {
	ID         int64      `json:"id"`
	OcurridoEn time.Time  `json:"ocurrido_en"`
	Contexto   string     `json:"contexto"`
	Mensaje    string     `json:"mensaje"`
	Ruta       string     `json:"ruta"`
	Metodo     string     `json:"metodo"`
	RequestID  string     `json:"request_id"`
	UsuarioID  *int64     `json:"usuario_id"`
	EsPanico   bool       `json:"es_panico"`
	Traza      string     `json:"traza"`
	Resuelto   bool       `json:"resuelto"`
	ResueltoEn *time.Time `json:"resuelto_en"`
}

var ErrNoEncontrado = errors.New("error no encontrado")

// RetencionDias: los errores más viejos que esto se borran solos.
// En una Raspberry el disco es chico y una falla en bucle podría llenarlo.
const RetencionDias = 30

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Registrar guarda un error. Nunca devuelve error a quien lo llama.
//
// Es a propósito: esto se llama JUSTO cuando algo ya falló. Si además
// reventara aquí, el usuario recibiría una respuesta rota por culpa del
// sistema de registro. Si no se puede guardar, queda al menos en el log.
func (s *Store) Registrar(e Error) {
	// Contexto propio y corto: el del request puede estar ya cancelado
	// (el usuario cerró la pestaña) y entonces no se guardaría nada.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const q = `
		INSERT INTO errores
			(contexto, mensaje, ruta, metodo, request_id, usuario_id, es_panico, traza)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := s.db.ExecContext(ctx, q,
		recortar(e.Contexto, 200),
		recortar(e.Mensaje, 4000),
		recortar(e.Ruta, 500),
		e.Metodo,
		recortar(e.RequestID, 100),
		e.UsuarioID,
		e.EsPanico,
		recortar(e.Traza, 20000),
	)
	if err != nil {
		slog.Error("no se pudo guardar el error en la base", "error", err, "contexto_original", e.Contexto)
	}
}

type Filtros struct {
	SoloPendientes bool
	Limite         int
	Offset         int
}

func (s *Store) Listar(ctx context.Context, f Filtros) ([]Error, int, error) {
	where := ""
	if f.SoloPendientes {
		where = "WHERE NOT resuelto"
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM errores "+where).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contando errores: %w", err)
	}

	if f.Limite <= 0 || f.Limite > 200 {
		f.Limite = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	q := `
		SELECT id, ocurrido_en, contexto, mensaje, ruta, metodo, request_id,
		       usuario_id, es_panico, traza, resuelto, resuelto_en
		FROM errores ` + where + `
		ORDER BY ocurrido_en DESC, id DESC
		LIMIT $1 OFFSET $2`

	filas, err := s.db.QueryContext(ctx, q, f.Limite, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listando errores: %w", err)
	}
	defer filas.Close()

	lista := []Error{}
	for filas.Next() {
		e, err := escanear(filas)
		if err != nil {
			return nil, 0, err
		}
		lista = append(lista, *e)
	}
	if err := filas.Err(); err != nil {
		return nil, 0, fmt.Errorf("recorriendo errores: %w", err)
	}

	return lista, total, nil
}

func (s *Store) Conteo(ctx context.Context) (pendientes, total int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE NOT resuelto), count(*) FROM errores`).Scan(&pendientes, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("contando errores: %w", err)
	}
	return pendientes, total, nil
}

func (s *Store) MarcarResuelto(ctx context.Context, id int64, resuelto bool) (*Error, error) {
	const q = `
		UPDATE errores
		SET resuelto = $1,
		    resuelto_en = CASE WHEN $1 THEN now() ELSE NULL END
		WHERE id = $2
		RETURNING id, ocurrido_en, contexto, mensaje, ruta, metodo, request_id,
		          usuario_id, es_panico, traza, resuelto, resuelto_en`

	e, err := escanear(s.db.QueryRowContext(ctx, q, resuelto, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// BorrarResueltos limpia del historial los que ya se corrigieron.
func (s *Store) BorrarResueltos(ctx context.Context) (int64, error) {
	resultado, err := s.db.ExecContext(ctx, `DELETE FROM errores WHERE resuelto`)
	if err != nil {
		return 0, fmt.Errorf("borrando errores resueltos: %w", err)
	}
	return resultado.RowsAffected()
}

// escaneable abstrae *sql.Row y *sql.Rows: ambos tienen Scan igual.
type escaneable interface {
	Scan(dest ...any) error
}

func escanear(fila escaneable) (*Error, error) {
	var (
		e          Error
		usuarioID  sql.NullInt64
		resueltoEn sql.NullTime
	)
	err := fila.Scan(&e.ID, &e.OcurridoEn, &e.Contexto, &e.Mensaje, &e.Ruta,
		&e.Metodo, &e.RequestID, &usuarioID, &e.EsPanico, &e.Traza,
		&e.Resuelto, &resueltoEn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("leyendo error: %w", err)
	}
	if usuarioID.Valid {
		e.UsuarioID = &usuarioID.Int64
	}
	if resueltoEn.Valid {
		e.ResueltoEn = &resueltoEn.Time
	}
	return &e, nil
}

// LimpiarViejos borra los errores que pasaron el tiempo de retención.
// Se llama al arrancar y cada cierto tiempo desde main.
func (s *Store) LimpiarViejos(ctx context.Context) (int64, error) {
	q := fmt.Sprintf(`DELETE FROM errores WHERE ocurrido_en < now() - interval '%d days'`, RetencionDias)

	resultado, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("limpiando errores viejos: %w", err)
	}
	return resultado.RowsAffected()
}

func recortar(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(recortado)"
}
