package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Store es la unica capa que habla SQL para usuarios.
// Los handlers nunca escriben SQL: asi el dia que cambies una consulta,
// solo tocas este archivo.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// NormalizarEmail evita que "Juan@Mail.com " y "juan@mail.com" sean 2 cuentas.
func NormalizarEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Store) PorEmail(ctx context.Context, email string) (*Usuario, error) {
	const q = `
		SELECT id, email, nombre, password_hash, creado_en
		FROM usuarios
		WHERE email = $1`

	var u Usuario
	// $1 es un parametro preparado: el valor viaja aparte del SQL,
	// por eso aqui no existe la inyeccion SQL.
	err := s.db.QueryRowContext(ctx, q, NormalizarEmail(email)).
		Scan(&u.ID, &u.Email, &u.Nombre, &u.PasswordHash, &u.CreadoEn)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando usuario por email: %w", err)
	}
	return &u, nil
}

func (s *Store) PorID(ctx context.Context, id int64) (*Usuario, error) {
	const q = `
		SELECT id, email, nombre, password_hash, creado_en
		FROM usuarios
		WHERE id = $1`

	var u Usuario
	err := s.db.QueryRowContext(ctx, q, id).
		Scan(&u.ID, &u.Email, &u.Nombre, &u.PasswordHash, &u.CreadoEn)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando usuario por id: %w", err)
	}
	return &u, nil
}

func (s *Store) Crear(ctx context.Context, email, nombre, passwordHash string) (*Usuario, error) {
	const q = `
		INSERT INTO usuarios (email, nombre, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, email, nombre, password_hash, creado_en`

	var u Usuario
	err := s.db.QueryRowContext(ctx, q, NormalizarEmail(email), nombre, passwordHash).
		Scan(&u.ID, &u.Email, &u.Nombre, &u.PasswordHash, &u.CreadoEn)

	if err != nil {
		// 23505 = unique_violation. Traducimos el error de Postgres a un
		// error de dominio para que quien llama no tenga que saber de codigos.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailDuplicado
		}
		return nil, fmt.Errorf("creando usuario: %w", err)
	}
	return &u, nil
}

func (s *Store) Contar(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM usuarios`).Scan(&n); err != nil {
		return 0, fmt.Errorf("contando usuarios: %w", err)
	}
	return n, nil
}

// ActualizarPassword cambia el hash de la contrasena.
func (s *Store) ActualizarPassword(ctx context.Context, id int64, passwordHash string) error {
	const q = `UPDATE usuarios SET password_hash = $1 WHERE id = $2`

	resultado, err := s.db.ExecContext(ctx, q, passwordHash, id)
	if err != nil {
		return fmt.Errorf("actualizando contrasena: %w", err)
	}
	filas, err := resultado.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificando cambio de contrasena: %w", err)
	}
	if filas == 0 {
		return ErrNoEncontrado
	}
	return nil
}
