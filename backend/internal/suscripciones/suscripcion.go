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

// Los dos ciclos de cobro. El del cliente vive en usuarios.ciclo_pago; el de
// cada pago, en pagos.ciclo (una copia: si el cliente cambia a anual, sus
// pagos mensuales viejos siguen siendo mensuales).
const (
	CicloMensual = "mensual"
	CicloAnual   = "anual"
)

func CicloValido(c string) bool { return c == CicloMensual || c == CicloAnual }

type Plan struct {
	ID            int64  `json:"id"`
	Nombre        string `json:"nombre"`
	PrecioMensual string `json:"precio_mensual"` // string, como todo el dinero
	// Vacio = el plan no se vende por año.
	PrecioAnual string `json:"precio_anual"`
	// Si los clientes del plan pueden usar el asistente con IA.
	IncluyeIA bool      `json:"incluye_ia"`
	Activo    bool      `json:"activo"`
	CreadoEn  time.Time `json:"creado_en"`
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
	Ciclo        string `json:"ciclo"`
	Periodo      string `json:"periodo"` // AAAA-MM, primer mes que cubre
	// Ultimo mes que cubre (AAAA-MM). Igual a Periodo si el pago es mensual.
	CubreHasta string `json:"cubre_hasta"`
	PagadoEn   string `json:"pagado_en"` // AAAA-MM-DD
	Nota       string `json:"nota"`
}

// Pendiente es un cliente activo con plan que no tiene cubierto el mes.
// Monto es lo que le toca pagar segun su ciclo: el precio mensual o el anual.
type Pendiente struct {
	UsuarioID  int64  `json:"usuario_id"`
	Email      string `json:"email"`
	Nombre     string `json:"nombre"`
	PlanNombre string `json:"plan_nombre"`
	Ciclo      string `json:"ciclo"`
	Monto      string `json:"monto"`
}

// Resumen es el tablero del negocio para un mes.
type Resumen struct {
	Periodo string `json:"periodo"` // AAAA-MM

	// El ingreso mensual recurrente (MRR): lo que vale al mes la cartera de
	// clientes activos con plan. Un cliente anual aporta la doceava parte de
	// su precio: es lo que "gana" cada mes aunque pague todo junto una vez.
	Esperado string `json:"esperado"`
	// Lo que de verdad entro en los pagos registrados en este mes. Un pago
	// anual cuenta entero aqui, en el mes en que se hizo.
	Cobrado string `json:"cobrado"`
	// Lo que falta. Nunca es "esperado - cobrado" a secas: ver el comentario
	// en store.Resumen.
	Pendiente string `json:"pendiente"`

	ClientesActivos int `json:"clientes_activos"`
	ClientesConPlan int `json:"clientes_con_plan"`
	// Cuantos tienen el mes cubierto: pagaron este mes, o pagaron un año que
	// incluye este mes.
	ClientesPagaron int `json:"clientes_pagaron"`
	ClientesAnuales int `json:"clientes_anuales"`

	PorPlan    []ResumenPlan `json:"por_plan"`
	Pendientes []Pendiente   `json:"pendientes"`
}

type ResumenPlan struct {
	PlanID      int64  `json:"plan_id"`
	Nombre      string `json:"nombre"`
	Precio      string `json:"precio_mensual"`
	PrecioAnual string `json:"precio_anual"`
	Clientes    int    `json:"clientes"`
	Esperado    string `json:"esperado"`
	Cobrado     string `json:"cobrado"`
}

var (
	ErrNoEncontrado    = errors.New("no encontrado")
	ErrNombreDuplicado = errors.New("ya existe un plan con ese nombre")
	ErrPlanEnUso       = errors.New("el plan tiene clientes asignados")
	ErrPagoDuplicado   = errors.New("ese cliente ya tiene cubierto ese periodo")
	ErrSinPlan         = errors.New("el cliente no tiene plan asignado")
	// Quitarle el precio anual a un plan que alguien esta pagando por año
	// dejaria a ese cliente con un ciclo que su plan ya no ofrece.
	ErrPlanConAnuales = errors.New("el plan tiene clientes que pagan por año")
)
