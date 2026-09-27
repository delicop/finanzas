// Package tiendas maneja el CRUD de las tiendas del usuario: los locales
// desde donde mueve su plata.
//
// No es una seccion que tenga toda cuenta: existe solo para los clientes cuyo
// plan la incluye (planes.incluye_tiendas), igual que el asistente con IA.
// Quien decide es el backend en cada peticion, no el menu de la app.
package tiendas

import (
	"errors"
	"time"
)

type Tienda struct {
	ID            int64     `json:"id"`
	Nombre        string    `json:"nombre"`
	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`

	// Como va la tienda, sumando TODOS sus cierres de caja. Es el resumen que
	// se ve sin tener que abrirla.
	//
	//	Ventas  = lo que vendio segun sus hojas
	//	Salidas = compras + gastos + descuentos + vales
	//	Queda   = Ventas - Salidas
	//
	// Las sumas las hace Postgres sobre columnas NUMERIC; aqui son texto.
	Ventas  string `json:"ventas"`
	Salidas string `json:"salidas"`
	Queda   string `json:"queda"`

	Cierres int `json:"cierres"`
	// El dia del ultimo cierre (AAAA-MM-DD). nil mientras no haya ninguno.
	UltimoCierre *string `json:"ultimo_cierre"`
}

var (
	ErrNoEncontrada    = errors.New("tienda no encontrada")
	ErrNombreDuplicado = errors.New("ya existe una tienda con ese nombre")
	// Borrar una tienda con cierres se rechaza: esas hojas son registros de
	// plata y no pueden irse con ella.
	ErrTieneCierres = errors.New("la tienda tiene cierres registrados")
)
