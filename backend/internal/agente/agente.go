// Package agente es el chat con el asistente de la app: el usuario escribe en
// lenguaje normal y un modelo de lenguaje le responde.
//
// La regla que manda sobre todas las demas: el agente solo ve los datos del
// usuario de la sesion. El id sale del context (lo pone el middleware de auth
// a partir del JWT) y nunca de algo que venga en el cuerpo de la peticion ni,
// mas adelante, de lo que proponga el modelo. Todo lo que este paquete lea de
// la base pasa por el mismo filtro por usuario_id que el resto de la app.
package agente

import (
	"encoding/json"
	"errors"
	"time"
)

// Los dos roles que guarda la base (lo fija tambien un CHECK). El rol
// "sistema" no se guarda: las instrucciones se arman en cada llamada, asi
// cambiarlas no obliga a migrar los hilos viejos.
const (
	RolUsuario = "usuario"
	RolAgente  = "agente"

	// RolHerramienta solo existe DENTRO de una respuesta, mientras el modelo
	// consulta datos. No se guarda en la base: al hilo llega el texto final,
	// no el ir y venir con las herramientas.
	RolHerramienta = "herramienta"
)

const (
	// MaxCaracteresMensaje frena dos cosas a la vez: un pegado accidental de
	// diez paginas y el costo de mandarlas al proveedor.
	MaxCaracteresMensaje = 2000

	// MensajesDeContexto es cuantos mensajes del hilo se le mandan al modelo.
	// El contexto se paga por token en cada llamada, asi que una conversacion
	// larga no puede crecer sin techo: se manda la cola, que es lo que de
	// verdad se esta hablando.
	MensajesDeContexto = 20

	// VigenciaPropuesta es cuanto dura una propuesta sin confirmar.
	//
	// Caduca a proposito: un "pagué 45 mil de almuerzo" confirmado tres dias
	// despues entra con la fecha de hoy y descuadra el mes. Si paso un dia,
	// mejor que lo vuelva a dictar.
	VigenciaPropuesta = 24 * time.Hour

	// MaxRondas son las veces que el modelo puede pedir datos antes de tener
	// que contestar. Cada ronda es una llamada mas al proveedor —o sea, plata
	// y segundos—, y con estas herramientas dos vueltas sobran: una para
	// consultar y otra para redactar. El techo existe para que una pregunta
	// rara no se convierta en un bucle caro.
	MaxRondas = 4
)

// Mensaje es una linea del hilo, tal como la pinta el frontend.
type Mensaje struct {
	ID        int64     `json:"id"`
	Rol       string    `json:"rol"`
	Contenido string    `json:"contenido"`
	CreadoEn  time.Time `json:"creado_en"`

	// Herramientas son las que se consultaron para armar esta respuesta. La
	// app las muestra debajo del mensaje: quien lee una cifra tiene derecho a
	// saber de donde salio.
	Herramientas []string `json:"herramientas,omitempty"`
}

// MensajeNuevo es lo que se guarda en el hilo. Es una struct y no siete
// parametros sueltos porque asi agregar un dato mas no obliga a revisar cada
// llamada para contar comas.
type MensajeNuevo struct {
	Rol           string
	Contenido     string
	TokensEntrada int
	TokensSalida  int
	Herramientas  []string
}

// Paso es una linea de la conversacion TAL COMO se le manda al modelo. Ademas
// de los mensajes del hilo incluye lo que no se guarda: la peticion de datos
// del agente y el resultado de cada herramienta.
type Paso struct {
	Rol       string
	Contenido string

	// Llamadas: el agente pidio ejecutar estas herramientas (rol agente).
	Llamadas []Llamada

	// LlamadaID y Nombre: este paso ES el resultado de una (rol herramienta).
	LlamadaID string
	Nombre    string
}

// Llamada es una herramienta que el modelo quiere ejecutar, con sus
// argumentos tal como los escribio.
//
// Ojo con lo que NO esta aqui: el id del usuario. Los argumentos del modelo
// nunca dicen de quien son los datos; eso lo pone el servidor a partir del
// JWT. Es la regla del paquete, y es lo que hace que una alucinacion del
// modelo no pueda convertirse en una fuga.
type Llamada struct {
	ID         string
	Nombre     string
	Argumentos json.RawMessage
}

// Herramienta es lo que se le ofrece al modelo: un nombre, para que sirve y
// el esquema JSON de sus argumentos.
type Herramienta struct {
	Nombre      string
	Descripcion string
	Parametros  map[string]any
}

// Conversacion es el hilo abierto del usuario con sus mensajes.
type Conversacion struct {
	ID       int64     `json:"id"`
	Mensajes []Mensaje `json:"mensajes"`
}

// Guardada es una conversacion terminada, tal como aparece en la lista.
type Guardada struct {
	ID          int64     `json:"id"`
	Titulo      string    `json:"titulo"`
	CreadaEn    time.Time `json:"creada_en"`
	ArchivadaEn time.Time `json:"archivada_en"`
	Mensajes    int       `json:"mensajes"`
}

// GuardadaConMensajes es una conversacion terminada abierta para leerla.
type GuardadaConMensajes struct {
	Guardada
	Hilo []Mensaje `json:"hilo"`
}

// Los dos tipos de propuesta que el agente sabe preparar.
const (
	TipoPropuestaMovimiento   = "movimiento"
	TipoPropuestaMarcarPagado = "marcar_pagado"
)

const (
	EstadoPropuestaPendiente  = "pendiente"
	EstadoPropuestaConfirmada = "confirmada"
	EstadoPropuestaDescartada = "descartada"
)

// Propuesta es una escritura que el agente dejo lista y el usuario todavia no
// ha confirmado. Hasta que la confirme, en la base de datos del dinero no ha
// pasado absolutamente nada.
type Propuesta struct {
	ID   int64  `json:"id"`
	Tipo string `json:"tipo"`

	// Datos es lo que pinta la tarjeta. Su forma depende del tipo y la unica
	// que la interpreta es la app; aqui viaja tal cual.
	Datos    json.RawMessage `json:"datos"`
	CreadaEn time.Time       `json:"creada_en"`
}

// PropuestaNueva es lo que prepara una herramienta antes de guardarse.
type PropuestaNueva struct {
	Tipo  string
	Datos any
}

// Respuesta es lo que devuelve el proveedor del modelo: o un texto para el
// usuario, o la peticion de ejecutar herramientas (nunca las dos cosas a la
// vez, en la practica).
type Respuesta struct {
	Contenido     string
	Llamadas      []Llamada
	TokensEntrada int
	TokensSalida  int
}

var (
	ErrNoEncontrada = errors.New("conversacion no encontrada")
	// Otra peticion abrio la conversacion un instante antes.
	ErrYaAbierta = errors.New("ya hay una conversacion abierta")

	// ErrLimiteDiario: el usuario ya gasto sus mensajes del dia. Existe
	// porque cada mensaje le cuesta plata al dueno del servidor y un cliente
	// con un script podria dejarle la factura del mes en la mano.
	ErrLimiteDiario = errors.New("limite diario de mensajes alcanzado")

	// ErrProveedorNoDisponible: el modelo no contesto (cayo, se saturo o se
	// acabo el tiempo). Se separa del resto de errores porque no es culpa de
	// la app ni del usuario, y se le responde 503 en vez de 500.
	ErrProveedorNoDisponible = errors.New("el proveedor del modelo no respondio")

	// ErrRespuestaVacia: contesto, pero sin texto. Guardar un mensaje en
	// blanco dejaria el hilo con un globo vacio para siempre.
	ErrRespuestaVacia = errors.New("el modelo devolvio una respuesta vacia")

	// ErrDemasiadasRondas: el modelo se quedo pidiendo datos y nunca redacto
	// la respuesta. Cortar es preferible a seguir pagando llamadas.
	ErrDemasiadasRondas = errors.New("el modelo no terminó de responder")

	ErrPropuestaNoEncontrada = errors.New("propuesta no encontrada")

	// ErrPropuestaResuelta: ya se confirmo o se descarto. Es lo que frena el
	// doble clic: la segunda vez no crea un segundo movimiento.
	ErrPropuestaResuelta = errors.New("la propuesta ya fue resuelta")

	// ErrPropuestaCaducada: paso mas de VigenciaPropuesta sin confirmarla.
	ErrPropuestaCaducada = errors.New("la propuesta caducó")
)
