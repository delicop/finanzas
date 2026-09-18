// Package recurrentes son los gastos e ingresos que se repiten: el arriendo,
// el internet, la cuota del local, el sueldo.
//
// LA DECISION QUE ORDENA TODO EL PAQUETE: la app NO crea el movimiento sola.
// Cuando a un recurrente le toca, deja una OCURRENCIA pendiente y un aviso, y
// el usuario confirma con un clic — pudiendo corregir el monto antes, que es
// lo que pasa con el recibo de la luz todos los meses.
//
// Un gasto inventado es peor que un gasto olvidado: el olvidado se nota al
// cuadrar el mes, el inventado no se nota nunca.
package recurrentes

import (
	"errors"
	"time"
)

// Cada cuanto se repite.
const (
	Mensual   = "mensual"
	Quincenal = "quincenal"
	Semanal   = "semanal"
)

// Estados de una ocurrencia.
const (
	Pendiente  = "pendiente"
	Confirmada = "confirmada"
	Descartada = "descartada"
)

const formatoFecha = "2006-01-02"

// VentanaDias es cuanto hacia atras se ponen al dia las ocurrencias.
//
// Sirve para dos cosas a la vez: si el servidor estuvo apagado una semana, al
// volver genera lo que falto; y si alguien crea un recurrente con fecha de
// inicio de hace tres anios, no le aparecen 36 arriendos por confirmar.
const VentanaDias = 60

// Recurrente es la plantilla: que se repite, cada cuanto y con que datos.
type Recurrente struct {
	ID int64 `json:"id"`

	CategoriaID     int64  `json:"categoria_id"`
	CategoriaNombre string `json:"categoria_nombre"`
	MedioPagoID     int64  `json:"medio_pago_id"`
	MedioPagoNombre string `json:"medio_pago_nombre"`

	Tipo        string `json:"tipo"`  // recibi | pague
	Monto       string `json:"monto"` // texto, como todo el dinero de la app
	Descripcion string `json:"descripcion"`

	Frecuencia string `json:"frecuencia"`
	// Dia: del mes (1..31) en mensual y quincenal, o de la semana (1=lunes)
	// en semanal.
	Dia int `json:"dia"`

	Desde string  `json:"desde"`
	Hasta *string `json:"hasta"`

	Activo bool `json:"activo"`

	// ProximaFecha es cuando vuelve a tocar, contando desde hoy. nil si el
	// recurrente esta pausado o ya se paso su fecha de fin.
	ProximaFecha *string `json:"proxima_fecha"`

	// Pendientes son las ocurrencias suyas sin resolver. La app las pinta
	// junto a la plantilla para que se vea de un vistazo que falta confirmar.
	Pendientes int `json:"pendientes"`

	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`
}

// Datos son los campos editables de un recurrente.
type Datos struct {
	CategoriaID int64
	MedioPagoID int64
	Tipo        string
	Monto       string
	Descripcion string
	Frecuencia  string
	Dia         int
	Desde       string
	Hasta       *string
	Activo      bool
}

// Ocurrencia es una vez concreta en que le toco a un recurrente.
type Ocurrencia struct {
	ID           int64  `json:"id"`
	RecurrenteID int64  `json:"recurrente_id"`
	Fecha        string `json:"fecha"`
	Estado       string `json:"estado"`

	// Copia de lo que dice la plantilla HOY, para pintar la tarjeta sin tener
	// que consultar el recurrente aparte.
	Tipo            string `json:"tipo"`
	Monto           string `json:"monto"`
	Descripcion     string `json:"descripcion"`
	CategoriaID     int64  `json:"categoria_id"`
	CategoriaNombre string `json:"categoria_nombre"`
	MedioPagoID     int64  `json:"medio_pago_id"`
	MedioPagoNombre string `json:"medio_pago_nombre"`

	MovimientoID *int64    `json:"movimiento_id"`
	CreadaEn     time.Time `json:"creada_en"`
}

var (
	ErrNoEncontrado       = errors.New("recurrente no encontrado")
	ErrCategoriaInvalida  = errors.New("la categoria no existe")
	ErrMedioInvalido      = errors.New("el medio de pago no existe")
	ErrOcurrenciaNoExiste = errors.New("esa ocurrencia no existe")
	ErrYaResuelta         = errors.New("esa ocurrencia ya se habia resuelto")
)

// EsFrecuenciaValida evita que llegue cualquier string a la base de datos.
func EsFrecuenciaValida(f string) bool {
	return f == Mensual || f == Quincenal || f == Semanal
}

// DiaValido: el rango depende de la frecuencia. Un "semanal el dia 23" no
// significa nada, y la base tambien lo rechaza.
func DiaValido(frecuencia string, dia int) bool {
	if frecuencia == Semanal {
		return dia >= 1 && dia <= 7
	}
	return dia >= 1 && dia <= 31
}

// --------------------------------------------------------------------------
// El calendario
// --------------------------------------------------------------------------

// Vencimientos son las fechas en que le toco a este recurrente entre `desde` y
// `hasta` (inclusive), respetando ademas su propio rango de vigencia.
//
// Es una funcion pura y sin base de datos a proposito: es la unica parte del
// paquete con logica de calendario de verdad, y asi se puede probar sola, con
// meses de 28, 30 y 31 dias y con anios bisiestos.
func Vencimientos(r Recurrente, desde, hasta time.Time) []time.Time {
	desde, hasta = soloElDia(desde), soloElDia(hasta)

	if inicio, err := time.Parse(formatoFecha, r.Desde); err == nil && inicio.After(desde) {
		desde = inicio
	}
	if r.Hasta != nil {
		fin, err := time.Parse(formatoFecha, *r.Hasta)
		if err == nil && fin.Before(hasta) {
			hasta = fin
		}
	}
	if desde.After(hasta) {
		return nil
	}

	switch r.Frecuencia {
	case Semanal:
		return semanales(r.Dia, desde, hasta)
	case Quincenal:
		return mensuales(r.Dia, desde, hasta, true)
	default:
		return mensuales(r.Dia, desde, hasta, false)
	}
}

// ProximoVencimiento es la siguiente vez que le toca, mirando desde `ahora`.
// nil si el recurrente esta pausado o ya se le paso la fecha de fin.
func ProximoVencimiento(r Recurrente, ahora time.Time) *string {
	if !r.Activo {
		return nil
	}
	// Un anio por delante alcanza para cualquiera de las tres frecuencias; si
	// en 365 dias no le toca ni una vez, es que ya se acabo.
	fechas := Vencimientos(r, soloElDia(ahora), soloElDia(ahora).AddDate(1, 0, 0))
	if len(fechas) == 0 {
		return nil
	}
	proxima := fechas[0].Format(formatoFecha)
	return &proxima
}

// mensuales recorre mes a mes poniendo el dia pedido. Con `quincena` agrega
// ademas el dia 15 dias despues, que es como se paga casi todo lo quincenal.
//
// El detalle que importa: si el dia pedido es el 31 y el mes tiene 30, cae el
// 30. Usar time.Date con dia 31 en abril daria el 1 de mayo — Go desborda al
// mes siguiente en silencio, y el arriendo terminaria saltandose meses.
func mensuales(dia int, desde, hasta time.Time, quincena bool) []time.Time {
	var fechas []time.Time

	// Se empieza un mes antes por si la quincena de ese mes cae dentro de la
	// ventana (el dia 20 de un recurrente "los dias 20" con quincena cae el 5
	// del mes siguiente).
	mes := time.Date(desde.Year(), desde.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	limite := time.Date(hasta.Year(), hasta.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !mes.After(limite) {
		base := enEseMes(mes, dia)
		agregarSiCabe(&fechas, base, desde, hasta)
		if quincena {
			agregarSiCabe(&fechas, base.AddDate(0, 0, 15), desde, hasta)
		}
		mes = mes.AddDate(0, 1, 0)
	}
	return fechas
}

// enEseMes devuelve el dia pedido dentro de ese mes, recortado al ultimo dia
// si el mes es mas corto.
func enEseMes(mes time.Time, dia int) time.Time {
	// El dia 0 del mes SIGUIENTE es el ultimo dia de este. Es la forma de
	// preguntar "¿cuantos dias tiene febrero?" sin tabla ni regla de bisiestos.
	ultimo := time.Date(mes.Year(), mes.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if dia > ultimo {
		dia = ultimo
	}
	return time.Date(mes.Year(), mes.Month(), dia, 0, 0, 0, 0, time.UTC)
}

// semanales recorre dia a dia quedandose con el dia de la semana pedido
// (1 = lunes ... 7 = domingo).
func semanales(dia int, desde, hasta time.Time) []time.Time {
	var fechas []time.Time
	for d := desde; !d.After(hasta); d = d.AddDate(0, 0, 1) {
		// En Go el domingo es 0; aqui es 7.
		numero := int(d.Weekday())
		if numero == 0 {
			numero = 7
		}
		if numero == dia {
			fechas = append(fechas, d)
		}
	}
	return fechas
}

func agregarSiCabe(fechas *[]time.Time, f, desde, hasta time.Time) {
	if !f.Before(desde) && !f.After(hasta) {
		*fechas = append(*fechas, f)
	}
}

func soloElDia(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
