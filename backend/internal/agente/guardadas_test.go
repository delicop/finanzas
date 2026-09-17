package agente_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"finanzas/internal/agente"
	"finanzas/internal/httpx"
)

// pedir hace una petición cualquiera al sub-router como la haría la app.
func (e *entorno) pedir(t *testing.T, usuarioID int64, metodo, ruta string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(metodo, ruta, nil).
		WithContext(httpx.ConUsuarioID(context.Background(), usuarioID))
	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	return res
}

func (e *entorno) terminar(t *testing.T, usuarioID int64) bool {
	t.Helper()
	res := e.pedir(t, usuarioID, http.MethodPost, "/terminar")
	if res.Code != http.StatusOK {
		t.Fatalf("terminar: %d — %s", res.Code, res.Body.String())
	}
	var cuerpo struct {
		Guardada bool `json:"guardada"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &cuerpo); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	return cuerpo.Guardada
}

func (e *entorno) guardadas(t *testing.T, usuarioID int64) []agente.Guardada {
	t.Helper()
	res := e.pedir(t, usuarioID, http.MethodGet, "/guardadas")
	if res.Code != http.StatusOK {
		t.Fatalf("guardadas: %d — %s", res.Code, res.Body.String())
	}
	var lista []agente.Guardada
	if err := json.Unmarshal(res.Body.Bytes(), &lista); err != nil {
		t.Fatalf("lista ilegible: %v", err)
	}
	return lista
}

// Terminar deja el chat limpio, pero la conversación no se pierde: queda en
// la lista con la primera pregunta como título y se puede releer entera.
func TestTerminarGuardaLaConversacion(t *testing.T) {
	e := nuevoEntorno(t, 10)
	ctx := context.Background()

	e.enviar(t, e.ana, "¿cuánto gasté este mes?")
	e.enviar(t, e.ana, "gracias")

	if !e.terminar(t, e.ana) {
		t.Fatal("terminar dijo que no había nada que guardar")
	}

	// El chat queda vacío...
	if _, err := e.store.Activa(ctx, e.ana); !errors.Is(err, agente.ErrNoEncontrada) {
		t.Errorf("sigue habiendo una conversación abierta (err = %v)", err)
	}

	// ...y la anterior está en la lista.
	lista := e.guardadas(t, e.ana)
	if len(lista) != 1 {
		t.Fatalf("guardadas = %d, se esperaba 1", len(lista))
	}
	if lista[0].Titulo != "¿cuánto gasté este mes?" {
		t.Errorf("título = %q", lista[0].Titulo)
	}
	if lista[0].Mensajes != 4 { // dos preguntas y dos respuestas
		t.Errorf("mensajes = %d, se esperaban 4", lista[0].Mensajes)
	}

	res := e.pedir(t, e.ana, http.MethodGet, fmt.Sprintf("/guardadas/%d", lista[0].ID))
	if res.Code != http.StatusOK {
		t.Fatalf("abrir guardada: %d", res.Code)
	}
	var g agente.GuardadaConMensajes
	if err := json.Unmarshal(res.Body.Bytes(), &g); err != nil {
		t.Fatalf("guardada ilegible: %v", err)
	}
	if len(g.Hilo) != 4 || g.Hilo[0].Contenido != "¿cuánto gasté este mes?" {
		t.Errorf("hilo = %+v", g.Hilo)
	}

	// El siguiente mensaje abre un hilo nuevo, y el modelo ya no relee el viejo.
	e.proveedor.llamadas = 0
	e.enviar(t, e.ana, "hola de nuevo")
	conv, err := e.store.Activa(ctx, e.ana)
	if err != nil {
		t.Fatalf("conversación nueva: %v", err)
	}
	if conv.ID == lista[0].ID || len(conv.Mensajes) != 2 {
		t.Errorf("el hilo nuevo arrastró el viejo: id=%d, mensajes=%d", conv.ID, len(conv.Mensajes))
	}
	if len(e.proveedor.pasos) != 1 {
		t.Errorf("al modelo le llegaron %d mensajes, se esperaba solo el nuevo", len(e.proveedor.pasos))
	}
}

// Un chat sin nada escrito no se guarda: llenaría la lista de basura.
func TestTerminarSinMensajesNoGuardaNada(t *testing.T) {
	e := nuevoEntorno(t, 10)

	if e.terminar(t, e.ana) {
		t.Error("sin conversación dijo que guardó algo")
	}

	if _, err := e.store.Crear(context.Background(), e.ana); err != nil {
		t.Fatalf("creando conversación: %v", err)
	}
	if e.terminar(t, e.ana) {
		t.Error("una conversación vacía quedó guardada")
	}
	if n := len(e.guardadas(t, e.ana)); n != 0 {
		t.Errorf("guardadas = %d, se esperaba 0", n)
	}
}

// Una tarjeta sin confirmar no puede quedar flotando en el chat nuevo, sin la
// conversación que la explica.
func TestTerminarDescartaLasTarjetasPendientes(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	e.terminar(t, e.ana)

	pendientes, err := e.store.PropuestasPendientes(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("pendientes: %v", err)
	}
	if len(pendientes) != 0 {
		t.Errorf("quedaron %d tarjetas pendientes después de terminar", len(pendientes))
	}
	if res := e.confirmar(t, e.ana, propuesta.ID, nil); res.Code == http.StatusCreated || res.Code == http.StatusOK {
		t.Errorf("se pudo confirmar una tarjeta de una conversación terminada (%d)", res.Code)
	}
	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Errorf("se crearon %d movimientos", n)
	}
}

// La base no deja abrir dos conversaciones a la vez (dos pestañas escribiendo).
func TestSoloUnaConversacionAbierta(t *testing.T) {
	e := nuevoEntorno(t, 10)
	ctx := context.Background()

	if _, err := e.store.Crear(ctx, e.ana); err != nil {
		t.Fatalf("primera: %v", err)
	}
	if _, err := e.store.Crear(ctx, e.ana); !errors.Is(err, agente.ErrYaAbierta) {
		t.Errorf("se abrió una segunda conversación (err = %v)", err)
	}
	// La de Beto no choca con la de Ana.
	if _, err := e.store.Crear(ctx, e.beto); err != nil {
		t.Errorf("Beto no pudo abrir la suya: %v", err)
	}
}

// Las guardadas de Ana no se ven ni se borran desde la cuenta de Beto: 404,
// como si no existieran.
func TestLasGuardadasSonDeCadaQuien(t *testing.T) {
	e := nuevoEntorno(t, 10)

	e.enviar(t, e.ana, "algo privado")
	e.terminar(t, e.ana)
	id := e.guardadas(t, e.ana)[0].ID
	ruta := fmt.Sprintf("/guardadas/%d", id)

	if n := len(e.guardadas(t, e.beto)); n != 0 {
		t.Errorf("Beto ve %d guardadas ajenas", n)
	}
	if res := e.pedir(t, e.beto, http.MethodGet, ruta); res.Code != http.StatusNotFound {
		t.Errorf("Beto leyó la guardada de Ana: %d", res.Code)
	}
	if res := e.pedir(t, e.beto, http.MethodDelete, ruta); res.Code != http.StatusNotFound {
		t.Errorf("Beto borró la guardada de Ana: %d", res.Code)
	}

	// Ana sí la borra, y una segunda vez ya no existe.
	if res := e.pedir(t, e.ana, http.MethodDelete, ruta); res.Code != http.StatusNoContent {
		t.Errorf("Ana no pudo borrarla: %d", res.Code)
	}
	if res := e.pedir(t, e.ana, http.MethodDelete, ruta); res.Code != http.StatusNotFound {
		t.Errorf("borrar dos veces devolvió %d", res.Code)
	}
}

// La conversación abierta no se borra por la ruta de guardadas.
func TestLaAbiertaNoSeBorraComoGuardada(t *testing.T) {
	e := nuevoEntorno(t, 10)

	e.enviar(t, e.ana, "hola")
	conv, err := e.store.Activa(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("activa: %v", err)
	}

	res := e.pedir(t, e.ana, http.MethodDelete, fmt.Sprintf("/guardadas/%d", conv.ID))
	if res.Code != http.StatusNotFound {
		t.Errorf("borró la conversación abierta: %d", res.Code)
	}
}

// El título se recorta: la lista no puede traer un párrafo por fila.
func TestElTituloSeRecorta(t *testing.T) {
	e := nuevoEntorno(t, 10)

	e.enviar(t, e.ana, strings.Repeat("á", agente.LargoTitulo+50))
	e.terminar(t, e.ana)

	titulo := e.guardadas(t, e.ana)[0].Titulo
	if n := len([]rune(titulo)); n != agente.LargoTitulo {
		t.Errorf("título de %d letras, se esperaban %d", n, agente.LargoTitulo)
	}
}
