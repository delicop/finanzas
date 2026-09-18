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
		SELECT id, email, nombre, rol, activo, password_hash, creado_en
		FROM usuarios
		WHERE email = $1`

	var u Usuario
	// $1 es un parametro preparado: el valor viaja aparte del SQL,
	// por eso aqui no existe la inyeccion SQL.
	err := s.db.QueryRowContext(ctx, q, NormalizarEmail(email)).
		Scan(&u.ID, &u.Email, &u.Nombre, &u.Rol, &u.Activo, &u.PasswordHash, &u.CreadoEn)

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
		SELECT id, email, nombre, rol, activo, password_hash, creado_en
		FROM usuarios
		WHERE id = $1`

	var u Usuario
	err := s.db.QueryRowContext(ctx, q, id).
		Scan(&u.ID, &u.Email, &u.Nombre, &u.Rol, &u.Activo, &u.PasswordHash, &u.CreadoEn)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando usuario por id: %w", err)
	}
	return &u, nil
}

// Crear inserta un usuario. El rol se pasa explicito para que quien llama
// tenga que decidirlo: un descuido con un valor por defecto aqui seria
// repartir permisos de administrador sin querer.
func (s *Store) Crear(ctx context.Context, email, nombre, rol, passwordHash string) (*Usuario, error) {
	const q = `
		INSERT INTO usuarios (email, nombre, rol, password_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, nombre, rol, activo, password_hash, creado_en`

	var u Usuario
	err := s.db.QueryRowContext(ctx, q, NormalizarEmail(email), nombre, rol, passwordHash).
		Scan(&u.ID, &u.Email, &u.Nombre, &u.Rol, &u.Activo, &u.PasswordHash, &u.CreadoEn)

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
// RegistrarAcceso anota que esta cuenta acaba de abrir la app.
//
// Se llama al entrar con la clave y tambien cuando el frontend valida el token
// guardado (GET /me), que es lo que pasa cada vez que alguien abre la app sin
// tener que volver a escribir la contrasena. Con solo el login, quien entra
// todos los dias con la sesion abierta se veria como si no volviera nunca.
//
// El UPDATE lleva su propio freno: solo escribe si lo anotado ya esta viejo.
// Sin eso, cada refresco de pantalla seria una escritura mas sobre la misma
// fila, y para contestar "¿lo usan?" sobra con saber el dia.
//
// No devuelve error a proposito: esto es telemetria del panel, no parte de
// entrar. Que falle no puede impedirle a nadie usar la app.
func (s *Store) RegistrarAcceso(ctx context.Context, id int64) {
	const q = `
		UPDATE usuarios
		SET ultimo_acceso = now()
		WHERE id = $1
		  AND (ultimo_acceso IS NULL OR ultimo_acceso < now() - interval '15 minutes')`

	_, _ = s.db.ExecContext(ctx, q, id)
}

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

// TieneIA dice si el plan del usuario incluye el asistente con IA.
//
// Sin plan no hay asistente: cada mensaje le cuesta plata al dueño del
// servidor, y lo que no se esta cobrando no se regala. Un plan desactivado
// sigue valiendo para quien ya lo tiene, igual que su precio.
//
// Vive aqui y no en suscripciones porque la usan dos paquetes que ya dependen
// de auth (el propio /me y el agente): un solo sitio donde cambiar la regla.
func (s *Store) TieneIA(ctx context.Context, usuarioID int64) (bool, error) {
	const q = `
		SELECT coalesce(p.incluye_ia, false)
		FROM usuarios u
		LEFT JOIN planes p ON p.id = u.plan_id
		WHERE u.id = $1`

	var ia bool
	err := s.db.QueryRowContext(ctx, q, usuarioID).Scan(&ia)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("consultando si el plan incluye IA: %w", err)
	}
	return ia, nil
}
