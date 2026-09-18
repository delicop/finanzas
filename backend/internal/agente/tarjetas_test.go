package agente

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// El bug que arreglan estas dos piezas: el usuario anota algo, la tarjeta le
// aparece, la guarda, y en el mensaje siguiente el agente le dice "revisa la
// tarjeta y dale aceptar" — pero en pantalla no hay ninguna. Pasa porque del
// ir y venir con las herramientas no queda nada en el hilo: lo único que el
// modelo vuelve a leer es su propia frase del turno anterior.

func tarjeta(estado string, guardada bool, datos map[string]any) PropuestaConEstado {
	crudos, err := json.Marshal(datos)
	if err != nil {
		panic(err)
	}
	return PropuestaConEstado{
		ID:       1,
		Tipo:     TipoPropuestaMovimiento,
		Datos:    crudos,
		Estado:   estado,
		CreadaEn: time.Now(),
		Guardada: guardada,
	}
}

func TestElEstadoDeLasTarjetasDiceEnQueQuedoCadaUna(t *testing.T) {
	casos := []struct {
		nombre  string
		tarjeta PropuestaConEstado
		espera  string
	}{
		{
			nombre:  "confirmada y guardada",
			tarjeta: tarjeta(EstadoPropuestaConfirmada, true, map[string]any{"monto": "45000.00"}),
			espera:  "CONFIRMÓ",
		},
		{
			nombre:  "descartada",
			tarjeta: tarjeta(EstadoPropuestaDescartada, false, map[string]any{"monto": "45000.00"}),
			espera:  "DESCARTÓ",
		},
		{
			nombre:  "pendiente",
			tarjeta: tarjeta(EstadoPropuestaPendiente, false, map[string]any{"monto": "45000.00"}),
			espera:  "PENDIENTE",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			estado := estadoDeLasTarjetas([]PropuestaConEstado{caso.tarjeta})
			if !strings.Contains(estado, caso.espera) {
				t.Errorf("el estado no dice %q:\n%s", caso.espera, estado)
			}
		})
	}
}

// Sin tarjetas pendientes el modelo tiene que leerlo en una frase, no
// deducirlo de una lista vacía.
func TestElEstadoAvisaCuandoNoQuedaNingunaTarjetaEnPantalla(t *testing.T) {
	confirmada := tarjeta(EstadoPropuestaConfirmada, true, map[string]any{"monto": "45000.00"})

	estado := estadoDeLasTarjetas([]PropuestaConEstado{confirmada})
	if !strings.Contains(estado, "NO hay ninguna tarjeta en pantalla") {
		t.Errorf("no avisa que la pantalla quedó sin tarjetas:\n%s", estado)
	}

	vacio := estadoDeLasTarjetas(nil)
	if !strings.Contains(vacio, "todavía no has preparado ninguna tarjeta") {
		t.Errorf("sin tarjetas el estado no lo dice:\n%s", vacio)
	}
}

// El estado viaja dentro del mensaje de sistema: si no llegara ahí, el modelo
// no lo vería nunca.
func TestLasInstruccionesLlevanElEstadoDeLasTarjetas(t *testing.T) {
	pendiente := tarjeta(EstadoPropuestaPendiente, false, map[string]any{
		"monto":       "45000.00",
		"descripcion": "almuerzo",
	})

	sistema := instrucciones("Ana", time.Now(), []PropuestaConEstado{pendiente})

	if !strings.Contains(sistema, "ESTADO DE LAS TARJETAS") {
		t.Error("el mensaje de sistema no trae la sección del estado de las tarjetas")
	}
	if !strings.Contains(sistema, "almuerzo") {
		t.Errorf("el estado no describe la tarjeta pendiente:\n%s", sistema)
	}
}

// La red de seguridad: aunque el prompt se lo diga, el modelo puede terminar
// mandando a una tarjeta que no existe. Eso no puede salir a pantalla.
func TestSeDetectaCuandoElAgenteMandaAUnaTarjetaQueNoExiste(t *testing.T) {
	pendiente := []PropuestaConEstado{tarjeta(EstadoPropuestaPendiente, false, nil)}
	resuelta := []PropuestaConEstado{tarjeta(EstadoPropuestaConfirmada, true, nil)}
	nueva := []PropuestaNueva{{Tipo: TipoPropuestaMovimiento}}

	casos := []struct {
		nombre    string
		contenido string
		nuevas    []PropuestaNueva
		tarjetas  []PropuestaConEstado
		espera    bool
	}{
		{
			nombre:    "manda a confirmar sin haber preparado nada",
			contenido: "Listo, te la dejé preparada: revisa la tarjeta y dale a guardar.",
			espera:    true,
		},
		{
			nombre:    "la tarjeta del turno pasado ya la guardó el usuario",
			contenido: "Perfecto, confírmala ahí abajo.",
			tarjetas:  resuelta,
			espera:    true,
		},
		{
			nombre:    "acaba de prepararla en este turno",
			contenido: "Te preparé el gasto de $45.000 en Personal, confírmalo ahí.",
			nuevas:    nueva,
			espera:    false,
		},
		{
			nombre:    "la tarjeta de antes sigue pendiente en pantalla",
			contenido: "Confírmala y seguimos.",
			tarjetas:  pendiente,
			espera:    false,
		},
		{
			nombre:    "decir que NO hay tarjetas no es mandar a ninguna",
			contenido: "No tienes ninguna tarjeta pendiente por confirmar.",
			tarjetas:  resuelta,
			espera:    false,
		},
		{
			nombre:    "pedir el dato que falta es la respuesta correcta",
			contenido: "¿En qué categoría lo pongo: Personal o Negocio?",
			tarjetas:  resuelta,
			espera:    false,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			if hay := mandaAUnaTarjetaQueNoExiste(caso.contenido, caso.nuevas, caso.tarjetas); hay != caso.espera {
				t.Errorf("mandaAUnaTarjetaQueNoExiste(%q) = %v, se esperaba %v", caso.contenido, hay, caso.espera)
			}
		})
	}
}

// Una tarjeta pendiente pero caducada ya NO está en pantalla: la app dejó de
// pintarla y confirmarla devuelve 409.
func TestUnaTarjetaCaducadaNoCuentaComoTarjetaEnPantalla(t *testing.T) {
	caducada := tarjeta(EstadoPropuestaPendiente, false, nil)
	caducada.CreadaEn = time.Now().Add(-VigenciaPropuesta - time.Hour)
	caducada.Caducada = true

	if !mandaAUnaTarjetaQueNoExiste("Confírmala ahí y listo.", nil, []PropuestaConEstado{caducada}) {
		t.Error("una tarjeta caducada se está tomando como si el usuario todavía la viera")
	}
	if estado := estadoDeLasTarjetas([]PropuestaConEstado{caducada}); !strings.Contains(estado, "CADUCÓ") {
		t.Errorf("el estado no dice que caducó:\n%s", estado)
	}
}
