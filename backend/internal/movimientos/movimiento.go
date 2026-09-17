// Package movimientos maneja los movimientos de dinero y sus facturas.
package movimientos

import (
	"errors"
	"time"
)

// Los tres tipos posibles. Se guardan sin tildes en la base de datos
// (son valores tecnicos); el frontend los muestra como "Recibí", "Pagué", "Presté".
const (
	TipoRecibi = "recibi" // entro plata
	TipoPague  = "pague"  // salio plata
	TipoPreste = "preste" // salio plata y alguien la debe
)

const (
	EstadoPendiente = "pendiente"
	EstadoPagado    = "pagado"
)

// FormatoFecha es el formato ISO que usa toda la API (YYYY-MM-DD).
const FormatoFecha = "2006-01-02"

type Movimiento struct {
	ID              int64  `json:"id"`
	CategoriaID     int64  `json:"categoria_id"`
	CategoriaNombre string `json:"categoria_nombre"`

	// Medio de pago/recaudo: por donde entro o salio la plata.
	// Es opcional: los movimientos viejos no lo tienen y no siempre se sabe.
	MedioPagoID     *int64  `json:"medio_pago_id"`
	MedioPagoNombre *string `json:"medio_pago_nombre"`

	// Por donde te devolvieron el prestamo. Solo aplica a 'preste' pagado:
	// prestas en efectivo y te pueden pagar por transferencia.
	MedioCobroID     *int64  `json:"medio_cobro_id"`
	MedioCobroNombre *string `json:"medio_cobro_nombre"`

	Tipo string `json:"tipo"`

	// Monto es string a proposito: viene de una columna NUMERIC y Go nunca
	// hace cuentas con el. Ver el paquete internal/dinero.
	Monto       string `json:"monto"`
	Fecha       string `json:"fecha"`
	Descripcion string `json:"descripcion"`

	// Punteros para que el JSON muestre null (y no "") cuando el tipo
	// no es 'preste' y estos campos no aplican.
	AQuien *string `json:"a_quien"`
	Estado *string `json:"estado"`

	// CobrarEl es el dia en que quedaron de devolver el prestamo (AAAA-MM-DD).
	// Opcional, y solo para 'preste'. Ese dia la app avisa.
	CobrarEl *string `json:"cobrar_el"`

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
	ErrNoEsPrestamo      = errors.New("solo los movimientos de tipo presté tienen estado")
	ErrMedioInvalido     = errors.New("el medio de pago no existe")
	ErrCobroAntes        = errors.New("la fecha de cobro no puede ser anterior al préstamo")
)

// EsTipoValido evita que llegue cualquier string a la base de datos.
func EsTipoValido(t string) bool {
	return t == TipoRecibi || t == TipoPague || t == TipoPreste
}

func EsEstadoValido(e string) bool {
	return e == EstadoPendiente || e == EstadoPagado
}
