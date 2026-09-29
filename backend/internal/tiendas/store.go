package tiendas

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

// Igual que en medios y categorias: TODAS las consultas filtran por el
// usuario_id que salio del JWT, nunca por uno que venga de la URL. Es lo que
// impide que con un id ajeno se lea o se borre la tienda de otro.

// Listar trae las tiendas con el resumen de todos sus cierres.
//
// El LATERAL suma las listas de CADA cierre (compras, gastos, descuentos y
// vales) y el GROUP BY las junta por tienda. Los pagos por Nequi no entran,
// igual que en la hoja (ver docs/decisiones.md).
//
// LEFT en los dos: una tienda recien creada sale en ceros, no desaparece.
func (s *Store) Listar(ctx context.Context, usuarioID int64) ([]Tienda, error) {
	const q = `
		SELECT t.id, t.nombre, t.creado_en, t.actualizado_en,
		       coalesce(sum(c.venta_tienda), 0)::numeric(14,2)::text,
		       coalesce(sum(l.salidas), 0)::numeric(14,2)::text,
		       (coalesce(sum(c.venta_tienda), 0) - coalesce(sum(l.salidas), 0))::numeric(14,2)::text,
		       count(c.id),
		       max(c.fecha)
		FROM tiendas t
		LEFT JOIN cierres c ON c.tienda_id = t.id AND c.usuario_id = t.usuario_id
		LEFT JOIN LATERAL (
			SELECT coalesce(sum(monto) FILTER (
			           WHERE grupo IN ('compra', 'gasto', 'descuento', 'vale')
			       ), 0) AS salidas
			FROM cierre_lineas WHERE cierre_id = c.id
		) l ON true
		WHERE t.usuario_id = $1
		GROUP BY t.id
		ORDER BY lower(t.nombre)`

	filas, err := s.db.QueryContext(ctx, q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando tiendas: %w", err)
	}
	defer filas.Close()

	// Slice inicializado (no nil) para que el JSON sea [] y no null cuando
	// todavia no hay ninguna.
	lista := []Tienda{}
	for filas.Next() {
		var (
			t      Tienda
			ultimo sql.NullTime
		)
		if err := filas.Scan(&t.ID, &t.Nombre, &t.CreadoEn, &t.ActualizadoEn,
			&t.Ventas, &t.Salidas, &t.Queda, &t.Cierres, &ultimo); err != nil {
			return nil, fmt.Errorf("leyendo tienda: %w", err)
		}
		if ultimo.Valid {
			dia := ultimo.Time.Format("2006-01-02")
			t.UltimoCierre = &dia
		}
		lista = append(lista, t)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo tiendas: %w", err)
	}
	return lista, nil
}

func (s *Store) Crear(ctx context.Context, usuarioID int64, nombre string) (*Tienda, error) {
	const q = `
		INSERT INTO tiendas (usuario_id, nombre)
		VALUES ($1, $2)
		RETURNING id, nombre, creado_en, actualizado_en`

	var t Tienda
	err := s.db.QueryRowContext(ctx, q, usuarioID, nombre).
		Scan(&t.ID, &t.Nombre, &t.CreadoEn, &t.ActualizadoEn)
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("creando tienda: %w", err)
	}
	return &t, nil
}

func (s *Store) Actualizar(ctx context.Context, usuarioID, id int64, nombre string) (*Tienda, error) {
	const q = `
		UPDATE tiendas
		SET nombre = $3, actualizado_en = now()
		WHERE id = $1 AND usuario_id = $2
		RETURNING id, nombre, creado_en, actualizado_en`

	var t Tienda
	err := s.db.QueryRowContext(ctx, q, id, usuarioID, nombre).
		Scan(&t.ID, &t.Nombre, &t.CreadoEn, &t.ActualizadoEn)

	if errors.Is(err, sql.ErrNoRows) {
		// Sin filas es "no es tuya o no existe", y las dos cosas se responden
		// igual: quien pregunta no tiene por que enterarse de cual de las dos.
		return nil, ErrNoEncontrada
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrNombreDuplicado
		}
		return nil, fmt.Errorf("actualizando tienda: %w", err)
	}
	return &t, nil
}

func (s *Store) Eliminar(ctx context.Context, usuarioID, id int64) error {
	const q = `DELETE FROM tiendas WHERE id = $1 AND usuario_id = $2`

	resultado, err := s.db.ExecContext(ctx, q, id, usuarioID)
	if err != nil {
		// 23503 = foreign_key_violation: la tienda tiene cierres y la base lo
		// impide. No se borran en cascada a proposito: se perderian hojas de
		// arqueo que si existieron.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrTieneCierres
		}
		return fmt.Errorf("eliminando tienda: %w", err)
	}
	filas, err := resultado.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando borrado de tienda: %w", err)
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
