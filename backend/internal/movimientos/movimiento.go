// Package movimientos maneja los movimientos de dinero y sus facturas.
package movimientos

import (
	"errors"
	"time"
)

// Los tipos posibles. Se guardan sin tildes en la base de datos (son valores
// tecnicos); el frontend los muestra como "Recibí", "Pagué", "Presté"...
const (
	TipoRecibi = "recibi" // entro plata
	TipoPague  = "pague"  // salio plata

	// Las dos deudas, que son simetricas: mismo a_quien, mismo estado, mismos
	// abonos, mismas cuotas. Lo unico que cambia es de que lado esta la plata.
	TipoPreste      = "preste"       // salio plata y TE LA DEBEN
	TipoMePrestaron = "me_prestaron" // entro plata y TU LA DEBES

	// Pasar plata de un medio a otro, de una categoria a otra, o las dos. No
	// es ni ingreso ni gasto: la misma plata cambia de bolsillo. Sale por
	// medio_pago_id y entra por medio_cobro_id (que puede ser el mismo si lo
	// que cambia es la categoria), y el balance general no se mueve.
	TipoTraslado = "traslado"
)

const (
	EstadoPendiente = "pendiente" // no ha abonado nada
	EstadoParcial   = "parcial"   // abono algo, falta el resto
	EstadoPagado    = "pagado"    // saldado
)

// FormatoFecha es el formato ISO que usa toda la API (YYYY-MM-DD).
const FormatoFecha = "2006-01-02"

type Movimiento struct {
	ID              int64  `json:"id"`
	CategoriaID     int64  `json:"categoria_id"`
	CategoriaNombre string `json:"categoria_nombre"`

	// Solo para los traslados entre categorías: a qué categoría ENTRÓ la
	// plata. En ese caso CategoriaID es de dónde salió. nil = la misma.
	CategoriaDestinoID     *int64  `json:"categoria_destino_id"`
	CategoriaDestinoNombre *string `json:"categoria_destino_nombre"`

	// Medio de pago/recaudo: por donde entro o salio la plata. En un traslado
	// es el ORIGEN.
	MedioPagoID     *int64  `json:"medio_pago_id"`
	MedioPagoNombre *string `json:"medio_pago_nombre"`

	// Solo para los traslados: a que medio ENTRO la plata.
	//
	// Antes esta columna guardaba "por donde te devolvieron el prestamo". Ese
	// dato ahora vive en cada abono, que es donde tiene que estar: un prestamo
	// se puede devolver en tres pedazos y por tres medios distintos.
	MedioCobroID     *int64  `json:"medio_cobro_id"`
	MedioCobroNombre *string `json:"medio_cobro_nombre"`

	Tipo string `json:"tipo"`

	// Monto es string a proposito: viene de una columna NUMERIC y Go nunca
	// hace cuentas con el. Ver el paquete internal/dinero.
	Monto       string `json:"monto"`
	Fecha       string `json:"fecha"`
	Descripcion string `json:"descripcion"`

	// Punteros para que el JSON muestre null (y no "") cuando el tipo
	// no es una deuda y estos campos no aplican.
	AQuien *string `json:"a_quien"`
	Estado *string `json:"estado"`

	// CobrarEl es el dia acordado: cuando te devuelven (preste) o cuando te
	// toca pagar (me_prestaron). Opcional, AAAA-MM-DD. Ese dia la app avisa.
	CobrarEl *string `json:"cobrar_el"`

	// Lo que ya se abono y lo que falta. Los calcula Postgres sumando la tabla
	// de abonos; en un movimiento que no es deuda van en "0.00".
	Abonado string `json:"abonado"`
	Saldo   string `json:"saldo"`
	Abonos  int    `json:"abonos"`

	// Cuantas cuotas tiene el acuerdo de pago (0 = no hay acuerdo).
	Cuotas int `json:"cuotas"`

	Factura *Factura `json:"factura"`

	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`
}

type Factura struct {
	Nombre string `json:"nombre"` // nombre original del archivo
	Tipo   string `json:"tipo"`   // mime real detectado
	URL    string `json:"url"`    // endpoint de descarga (requiere token)
}

var (
	ErrNoEncontrado      = errors.New("movimiento no encontrado")
	ErrCategoriaInvalida = errors.New("la categoria no existe")
	ErrSinFactura        = errors.New("el movimiento no tiene factura")
	ErrYaTieneFactura    = errors.New("el movimiento ya tiene una factura")
	ErrNoEsPrestamo      = errors.New("solo los prestamos tienen estado de pago")
	ErrMedioInvalido     = errors.New("el medio de pago no existe")
	ErrCobroAntes        = errors.New("la fecha de cobro no puede ser anterior al prestamo")
	ErrMismoMedio        = errors.New("el origen y el destino del traslado son el mismo medio")
	ErrCategoriaDestino  = errors.New("la categoria destino no existe")
	ErrAbonoDeMas        = errors.New("el abono es mayor que el saldo")
	ErrSinSaldo          = errors.New("la deuda ya esta saldada")
)

// EsTipoValido evita que llegue cualquier string a la base de datos.
func EsTipoValido(t string) bool {
	switch t {
	case TipoRecibi, TipoPague, TipoPreste, TipoMePrestaron, TipoTraslado:
		return true
	}
	return false
}

// EsDeuda: los dos tipos que llevan a_quien, estado, fecha acordada, abonos y
// cuotas. Se pregunta en muchos sitios; tenerlo en una funcion evita que
// alguno se olvide de me_prestaron y deje media funcionalidad sin el otro lado.
func EsDeuda(t string) bool { return t == TipoPreste || t == TipoMePrestaron }

func EsEstadoValido(e string) bool {
	return e == EstadoPendiente || e == EstadoParcial || e == EstadoPagado
}

// Los mensajes de error de tipo y estado se repiten en el handler, en la
// exportacion y en el asistente. En una constante para que los tres digan lo
// mismo y para que agregar un tipo no deje mensajes viejos por ahi.
const (
	TiposValidosMsg   = "Tipo inválido: usa recibi, pague, preste, me_prestaron o traslado"
	EstadosValidosMsg = "Estado inválido: usa pagado, parcial o pendiente"
	MismoOrigenMsg    = "El origen y el destino son iguales: cambia el medio, la categoría o las dos"
)
