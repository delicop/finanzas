// Package suscripciones es el lado NEGOCIO de la app: los planes que vendes,
// quien esta en cada uno y quien ya pago el mes.
//
// No confundir con movimientos/categorias/medios_pago, que son las finanzas
// PERSONALES de cada cliente. Aquellas las ve su dueno; estas solo un admin.
package suscripciones

import (
	"errors"
	"time"
)

type Plan struct {
	ID            int64     `json:"id"`
	Nombre        string    `json:"nombre"`
	PrecioMensual string    `json:"precio_mensual"` // string, como todo el dinero
	Activo        bool      `json:"activo"`
	CreadoEn      time.Time `json:"creado_en"`
	// Cuantos clientes lo tienen. Es lo primero que se mira antes de cambiarle
	// el precio a un plan o de intentar borrarlo.
	Clientes int `json:"clientes"`
}

type Pago struct {
	ID           int64  `json:"id"`
	UsuarioID    *int64 `json:"usuario_id"` // null si el cliente ya no existe
	ClienteEmail string `json:"cliente_email"`
	PlanNombre   string `json:"plan_nombre"`
	Monto        string `json:"monto"`
	Periodo      string `json:"periodo"`   // AAAA-MM
	PagadoEn     string `json:"pagado_en"` // AAAA-MM-DD
	Nota         string `json:"nota"`
}

// Pendiente es un cliente activo con plan que todavia no ha pagado el mes.
type Pendiente struct {
	UsuarioID  int64  `json:"usuario_id"`
	Email      string `json:"email"`
	Nombre     string `json:"nombre"`
	PlanNombre string `json:"plan_nombre"`
	Monto      string `json:"monto"`
}

// Resumen es el tablero del negocio para un mes.
type Resumen struct {
	Periodo string `json:"periodo"` // AAAA-MM

	// Lo que DEBERIAS facturar este mes: la suma de los planes de todos los
	// clientes activos que tienen uno. Es el famoso MRR.
	Esperado string `json:"esperado"`
	// Lo que de verdad entro.
	Cobrado string `json:"cobrado"`
	// Lo que falta. Nunca es "esperado - cobrado" a secas: ver el comentario
	// en store.Resumen.
	Pendiente string `json:"pendiente"`

	ClientesActivos int `json:"clientes_activos"`
	ClientesConPlan int `json:"clientes_con_plan"`
	ClientesPagaron int `json:"clientes_pagaron"`

	PorPlan    []ResumenPlan `json:"por_plan"`
	Pendientes []Pendiente   `json:"pendientes"`
}

type ResumenPlan struct {
	PlanID   int64  `json:"plan_id"`
	Nombre   string `json:"nombre"`
	Precio   string `json:"precio_mensual"`
	Clientes int    `json:"clientes"`
	Esperado string `json:"esperado"`
	Cobrado  string `json:"cobrado"`
}

var (
	ErrNoEncontrado    = errors.New("no encontrado")
	ErrNombreDuplicado = errors.New("ya existe un plan con ese nombre")
	ErrPlanEnUso       = errors.New("el plan tiene clientes asignados")
	ErrPagoDuplicado   = errors.New("ese cliente ya pago ese mes")
	ErrSinPlan         = errors.New("el cliente no tiene plan asignado")
)
