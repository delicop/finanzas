// Package auth maneja el usuario, el login y la proteccion de rutas con JWT.
package auth

import (
	"errors"
	"time"
)

type Usuario struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Nombre       string    `json:"nombre"`
	PasswordHash string    `json:"-"` // el guion evita que se serialice NUNCA
	CreadoEn     time.Time `json:"creado_en"`
}

var (
	ErrCredenciales   = errors.New("credenciales invalidas")
	ErrNoEncontrado   = errors.New("usuario no encontrado")
	ErrEmailDuplicado = errors.New("ya existe un usuario con ese email")
)
