// Package auth maneja el usuario, el login y la proteccion de rutas con JWT.
package auth

import (
	"errors"
	"time"
)

// Roles posibles. Son dos y nada mas: el dueno del servidor y todos los demas.
const (
	RolAdmin   = "admin"
	RolUsuario = "usuario"
)

func RolValido(rol string) bool {
	return rol == RolAdmin || rol == RolUsuario
}

type Usuario struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Nombre       string    `json:"nombre"`
	Rol          string    `json:"rol"`
	Activo       bool      `json:"activo"`
	PasswordHash string    `json:"-"` // el guion evita que se serialice NUNCA
	CreadoEn     time.Time `json:"creado_en"`
}

func (u *Usuario) EsAdmin() bool { return u.Rol == RolAdmin }

var (
	ErrCredenciales   = errors.New("credenciales invalidas")
	ErrNoEncontrado   = errors.New("usuario no encontrado")
	ErrEmailDuplicado = errors.New("ya existe un usuario con ese email")
	ErrInactivo       = errors.New("la cuenta esta desactivada")
)
