// Package categorias maneja el CRUD de las categorias del usuario.
package categorias

import (
	"errors"
	"time"
)

type Categoria struct {
	ID            int64     `json:"id"`
	Nombre        string    `json:"nombre"`
	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`

	// Solo se llena en el listado: cuantos movimientos la usan.
	// Le sirve al frontend para avisar antes de intentar borrarla.
	Movimientos int `json:"movimientos"`
}

var (
	ErrNoEncontrada     = errors.New("categoria no encontrada")
	ErrNombreDuplicado  = errors.New("ya existe una categoria con ese nombre")
	ErrTieneMovimientos = errors.New("la categoria tiene movimientos asociados")
)
