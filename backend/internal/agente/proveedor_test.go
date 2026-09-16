package agente_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finanzas/internal/agente"
)

// Estas pruebas no necesitan base de datos ni internet: levantan un servidor
// de mentiras que responde como responderia DeepSeek u OpenRouter. Lo que se
// esta probando es el contrato con el proveedor, que es justo donde un cambio
// silencioso rompe el chat entero.

const llaveDePrueba = "sk-llave-secreta-de-prueba"

func pasoSimple(texto string) []agente.Paso {
	return []agente.Paso{{Rol: agente.RolUsuario, Contenido: texto}}
}

func servidorFalso(t *testing.T, manejar http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(manejar)
	t.Cleanup(srv.Close)
	return srv
}

func TestCompletarMandaLlaveHistorialYSistema(t *testing.T) {
	var recibido struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens int `json:"max_tokens"`
	}
	var autorizacion, ruta string

	srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		autorizacion = r.Header.Get("Authorization")
		ruta = r.URL.Path
		cuerpo, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(cuerpo, &recibido); err != nil {
			t.Errorf("el cuerpo no es JSON válido: %v", err)
		}
		_, _ = io.WriteString(w, `{
			"choices": [{"message": {"role": "assistant", "content": "  Claro que sí.  "}}],
			"usage": {"prompt_tokens": 120, "completion_tokens": 8}
		}`)
	})

	p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "modelo-de-prueba", 5*time.Second)

	res, err := p.Completar(context.Background(), "Eres el asistente.", []agente.Paso{
		{Rol: agente.RolUsuario, Contenido: "hola"},
		{Rol: agente.RolAgente, Contenido: "¿en qué te ayudo?"},
		{Rol: agente.RolUsuario, Contenido: "¿cómo registro un gasto?"},
	}, nil)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}

	if ruta != "/chat/completions" {
		t.Errorf("ruta = %q, se esperaba /chat/completions", ruta)
	}
	if autorizacion != "Bearer "+llaveDePrueba {
		t.Errorf("Authorization = %q", autorizacion)
	}
	if recibido.Model != "modelo-de-prueba" {
		t.Errorf("model = %q", recibido.Model)
	}
	if recibido.MaxTokens <= 0 {
		t.Error("se mandó sin techo de tokens: una respuesta larga se cobra igual que una útil")
	}

	// El mensaje de sistema va primero y los roles del dominio se traducen a
	// los que entiende la API.
	esperados := []struct{ rol, contenido string }{
		{"system", "Eres el asistente."},
		{"user", "hola"},
		{"assistant", "¿en qué te ayudo?"},
		{"user", "¿cómo registro un gasto?"},
	}
	if len(recibido.Messages) != len(esperados) {
		t.Fatalf("se mandaron %d mensajes, se esperaban %d", len(recibido.Messages), len(esperados))
	}
	for i, e := range esperados {
		if recibido.Messages[i].Role != e.rol || recibido.Messages[i].Content != e.contenido {
			t.Errorf("mensaje %d = {%q, %q}, se esperaba {%q, %q}",
				i, recibido.Messages[i].Role, recibido.Messages[i].Content, e.rol, e.contenido)
		}
	}

	if res.Contenido != "Claro que sí." {
		t.Errorf("contenido = %q (se esperaba sin espacios alrededor)", res.Contenido)
	}
	if res.TokensEntrada != 120 || res.TokensSalida != 8 {
		t.Errorf("tokens = %d/%d, se esperaba 120/8", res.TokensEntrada, res.TokensSalida)
	}
}

// Que el proveedor se caiga o se sature NO es un error de la app: tiene que
// distinguirse para responderle al usuario "intenta en un minuto" (503) en vez
// de un 500 seco.
func TestProveedorCaidoOSaturadoSeDistingue(t *testing.T) {
	casos := map[string]int{
		"saturado":         http.StatusTooManyRequests,
		"error interno":    http.StatusBadGateway,
		"en mantenimiento": http.StatusServiceUnavailable,
	}

	for nombre, status := range casos {
		t.Run(nombre, func(t *testing.T) {
			srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error": {"message": "vuelve luego"}}`)
			})

			p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 5*time.Second)
			_, err := p.Completar(context.Background(), "s", pasoSimple("hola"), nil)

			if !errors.Is(err, agente.ErrProveedorNoDisponible) {
				t.Fatalf("error = %v, se esperaba ErrProveedorNoDisponible", err)
			}
		})
	}
}

// Una llave vencida sí es culpa nuestra: no puede disfrazarse de "el
// proveedor no está disponible", porque entonces nadie la iría a arreglar.
func TestLlaveRechazadaNoSeDisfrazaDeCaida(t *testing.T) {
	srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error": {"message": "invalid api key"}}`)
	})

	p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 5*time.Second)
	_, err := p.Completar(context.Background(), "s", pasoSimple("hola"), nil)

	if err == nil {
		t.Fatal("se esperaba un error")
	}
	if errors.Is(err, agente.ErrProveedorNoDisponible) {
		t.Error("un 401 no es una caída del proveedor: es configuración por arreglar")
	}
	// El error termina en la bitácora, que un admin puede leer desde la app:
	// la llave no puede viajar ahí.
	if strings.Contains(err.Error(), llaveDePrueba) {
		t.Errorf("el error filtra la llave del proveedor: %v", err)
	}
}

func TestRespuestaVaciaNoSeGuarda(t *testing.T) {
	casos := map[string]string{
		"sin choices":         `{"choices": []}`,
		"contenido en blanco": `{"choices": [{"message": {"role": "assistant", "content": "   "}}]}`,
	}

	for nombre, cuerpo := range casos {
		t.Run(nombre, func(t *testing.T) {
			srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, cuerpo)
			})

			p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 5*time.Second)
			_, err := p.Completar(context.Background(), "s", pasoSimple("hola"), nil)

			if !errors.Is(err, agente.ErrRespuestaVacia) {
				t.Fatalf("error = %v, se esperaba ErrRespuestaVacia", err)
			}
		})
	}
}

// Algunos proveedores responden 200 con el error metido en el cuerpo. Si no se
// mira, el usuario ve un globo vacío y nadie se entera de que falló.
func TestErrorDentroDeUn200(t *testing.T) {
	srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"error": {"message": "sin saldo", "type": "billing"}}`)
	})

	p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 5*time.Second)
	_, err := p.Completar(context.Background(), "s", pasoSimple("hola"), nil)

	if !errors.Is(err, agente.ErrProveedorNoDisponible) {
		t.Fatalf("error = %v, se esperaba ErrProveedorNoDisponible", err)
	}
}

// Sin timeout propio, un proveedor que no contesta deja la petición colgada
// hasta que el router la corta a los 30s, con el usuario mirando la pantalla.
func TestTimeoutPropio(t *testing.T) {
	bloquear := make(chan struct{})
	srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		<-bloquear
	})
	t.Cleanup(func() { close(bloquear) })

	p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 100*time.Millisecond)

	inicio := time.Now()
	_, err := p.Completar(context.Background(), "s", pasoSimple("hola"), nil)

	if !errors.Is(err, agente.ErrProveedorNoDisponible) {
		t.Fatalf("error = %v, se esperaba ErrProveedorNoDisponible", err)
	}
	if transcurrido := time.Since(inicio); transcurrido > 2*time.Second {
		t.Errorf("tardó %v: el timeout no se está aplicando", transcurrido)
	}
}

// --------------------------------------------------------------------------
// Herramientas
// --------------------------------------------------------------------------

// El formato de las herramientas es donde más fácil se rompe la integración:
// los argumentos viajan como un STRING con JSON adentro, y el resultado vuelve
// atado al id de la llamada.
func TestPideYRecibeHerramientas(t *testing.T) {
	var recibido struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}

	srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(cuerpo, &recibido); err != nil {
			t.Errorf("cuerpo ilegible: %v", err)
		}
		// Así responde el modelo cuando quiere datos: sin texto y con la
		// llamada. Que `content` venga vacío es normal, no un error.
		_, _ = io.WriteString(w, `{
			"choices": [{
				"finish_reason": "tool_calls",
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_abc",
						"type": "function",
						"function": {"name": "listar_movimientos", "arguments": "{\"tipo\":\"pague\"}"}
					}]
				}
			}],
			"usage": {"prompt_tokens": 300, "completion_tokens": 25}
		}`)
	})

	p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 5*time.Second)

	// Una conversación que ya trae una vuelta de herramientas adentro.
	pasos := []agente.Paso{
		{Rol: agente.RolUsuario, Contenido: "¿cuánto gasté?"},
		{Rol: agente.RolAgente, Llamadas: []agente.Llamada{
			{ID: "call_previo", Nombre: "resumen", Argumentos: json.RawMessage(`{}`)},
		}},
		{Rol: agente.RolHerramienta, LlamadaID: "call_previo", Nombre: "resumen", Contenido: `{"totales":{}}`},
	}
	herramientas := []agente.Herramienta{{
		Nombre:      "listar_movimientos",
		Descripcion: "Lista los movimientos",
		Parametros:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}

	res, err := p.Completar(context.Background(), "sistema", pasos, herramientas)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}

	// Lo que se mandó: el catálogo con su descripción...
	if len(recibido.Tools) != 1 || recibido.Tools[0].Function.Name != "listar_movimientos" {
		t.Fatalf("herramientas enviadas = %+v", recibido.Tools)
	}
	if recibido.Tools[0].Type != "function" || recibido.Tools[0].Function.Description == "" {
		t.Errorf("la herramienta va incompleta: %+v", recibido.Tools[0])
	}

	// ...y la conversación con sus roles traducidos.
	if len(recibido.Messages) != 4 {
		t.Fatalf("se mandaron %d mensajes, se esperaban 4 (sistema + 3 pasos)", len(recibido.Messages))
	}
	pedido := recibido.Messages[2]
	if pedido.Role != "assistant" || len(pedido.ToolCalls) != 1 || pedido.ToolCalls[0].ID != "call_previo" {
		t.Errorf("la petición de datos no se mandó bien: %+v", pedido)
	}
	resultado := recibido.Messages[3]
	if resultado.Role != "tool" || resultado.ToolCallID != "call_previo" {
		t.Errorf("el resultado no volvió atado a su llamada: %+v", resultado)
	}

	// Y lo que se leyó de vuelta.
	if len(res.Llamadas) != 1 {
		t.Fatalf("llamadas = %+v", res.Llamadas)
	}
	if res.Llamadas[0].ID != "call_abc" || res.Llamadas[0].Nombre != "listar_movimientos" {
		t.Errorf("llamada = %+v", res.Llamadas[0])
	}
	if string(res.Llamadas[0].Argumentos) != `{"tipo":"pague"}` {
		t.Errorf("argumentos = %s", res.Llamadas[0].Argumentos)
	}
	if res.TokensEntrada != 300 {
		t.Errorf("tokens de entrada = %d", res.TokensEntrada)
	}
}

// Sin argumentos, algunos proveedores mandan "" en vez de "{}". Si eso llegara
// tal cual a la herramienta, fallaría al decodificarlo.
func TestArgumentosVaciosSeNormalizan(t *testing.T) {
	srv := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices": [{"message": {"role": "assistant", "content": "",
			"tool_calls": [{"id": "c1", "type": "function",
			                "function": {"name": "resumen", "arguments": ""}}]}}]}`)
	})

	p := agente.NuevoProveedorHTTP(srv.URL, llaveDePrueba, "m", 5*time.Second)
	res, err := p.Completar(context.Background(), "s", pasoSimple("¿cuánto tengo?"), nil)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if string(res.Llamadas[0].Argumentos) != "{}" {
		t.Errorf("argumentos = %q, se esperaba {}", res.Llamadas[0].Argumentos)
	}
}
