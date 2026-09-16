// Package medios maneja el CRUD de los medios de pago/recaudo del usuario.
package medios

import (
	"errors"
	"time"
)

type Medio struct {
	ID            int64     `json:"id"`
	Nombre        string    `json:"nombre"`
	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`

	// Solo se llena en el listado: cuantos movimientos lo usan.
	// Le sirve al frontend para avisar antes de intentar borrarlo.
	Movimientos int `json:"movimientos"`
}

var (
	ErrNoEncontrado     = errors.New("medio de pago no encontrado")
	ErrNombreDuplicado  = errors.New("ya existe un medio de pago con ese nombre")
	ErrTieneMovimientos = errors.New("el medio de pago tiene movimientos asociados")
)
