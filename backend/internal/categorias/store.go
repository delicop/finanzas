package categorias

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

// Fijate que TODAS las consultas filtran por usuario_id.
// Hoy hay un solo usuario, pero si el id viniera solo de la URL, cualquiera
// con un token valido podria leer o borrar datos de otro usuario (eso se
// llama IDOR). Filtrar por el id que salio del JWT lo cierra de raiz.

func (s *Store) Listar(ctx context.Context, usuarioID int64) ([]Categoria, error) {
	const q = `
		SELECT c.id, c.nombre, c.creado_en, c.actualizado_en,
		       count(m.id) AS movimientos
		FROM categorias c
		LEFT JOIN movimientos m ON m.categoria_id = c.id
		WHERE c.usuario_id = $1
		GROUP BY c.id
		ORDER BY lower(c.nombre)`

	filas, err := s.db.QueryContext(ctx, q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando categorias: %w", err)
	}
	defer filas.Close()

	// Slice inicializado (no nil) para que el JSON sea [] y no null
	// cuando todavia no hay categorias.
	lista := []Categoria{}
	for filas.Next() {
		var c Categoria
		if err := filas.Scan(&c.ID, &c.Nombre, &c.CreadoEn, &c.ActualizadoEn, &c.Movimientos); err != nil {
			return nil, fmt.Errorf("leyendo categoria: %w", err)
		}
		lista = append(lista, c)
	}
	// Err() atrapa fallos que ocurrieron a mitad de la iteracion
	// y que el bucle por si solo no reporta.
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo categorias: %w", err)
	}
	return lista, nil
}

func (s *Store) PorID(ctx context.Context, usuarioID, id int64) (*Categoria, error) {
	const q = `
		SELECT id, nombre, creado_en, actualizado_en
		FROM categorias
		WHERE id = $1 AND usuario_id = $2`

	var c Categoria
	err := s.db.QueryRowContext(ctx, q, id, usuarioID).
		Scan(&c.ID, &c.Nombre, &c.CreadoEn, &c.ActualizadoEn)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		return nil, fmt.Errorf("consultando categoria: %w", err)
	}
	return &c, nil
}

func (s *Store) Crear(ctx context.Context, usuarioID int64, nombre string) (*Categoria, error) {
	const q = `
		INSERT INTO categorias (usuario_id, nombre)
		VALUES ($1, $2)
		RETURNING id, nombre, creado_en, actualizado_en`

	var c Categoria
	err := s.db.QueryRowContext(ctx, q, usuarioID, nombre).
		Scan(&c.ID, &c.Nombre, &c.CreadoEn, &c.ActualizadoEn)

	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("creando categoria: %w", err)
	}
	return &c, nil
}

func (s *Store) Actualizar(ctx context.Context, usuarioID, id int64, nombre string) (*Categoria, error) {
	const q = `
		UPDATE categorias
		SET nombre = $1, actualizado_en = now()
		WHERE id = $2 AND usuario_id = $3
		RETURNING id, nombre, creado_en, actualizado_en`

	var c Categoria
	err := s.db.QueryRowContext(ctx, q, nombre, id, usuarioID).
		Scan(&c.ID, &c.Nombre, &c.CreadoEn, &c.ActualizadoEn)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("actualizando categoria: %w", err)
	}
	return &c, nil
}

func (s *Store) Eliminar(ctx context.Context, usuarioID, id int64) error {
	const q = `DELETE FROM categorias WHERE id = $1 AND usuario_id = $2`

	resultado, err := s.db.ExecContext(ctx, q, id, usuarioID)
	if err != nil {
		// 23503 = foreign_key_violation: hay movimientos apuntando a esta
		// categoria y la FK esta declarada como ON DELETE RESTRICT.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrTieneMovimientos
		}
		return fmt.Errorf("eliminando categoria: %w", err)
	}

	filas, err := resultado.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando borrado: %w", err)
	}
	if filas == 0 {
		return ErrNoEncontrada
	}
	return nil
}

func esViolacionUnica(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
