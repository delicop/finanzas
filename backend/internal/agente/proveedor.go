package agente

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Proveedor es lo unico que sabe hablar con el modelo.
//
// Es una interfaz de un solo metodo, y esta declarada aqui (donde se consume)
// y no junto a la implementacion, que es la costumbre en Go. Da dos cosas:
// cambiar de proveedor sin tocar el handler, y probar el chat completo con una
// implementacion falsa, sin red y sin gastar un peso.
type Proveedor interface {
	Completar(ctx context.Context, sistema string, pasos []Paso, herramientas []Herramienta) (Respuesta, error)
}

// ProveedorHTTP habla con cualquier API compatible con la de OpenAI
// (POST {base}/chat/completions). DeepSeek y OpenRouter lo son, asi que
// cambiar de uno al otro es cambiar dos variables del .env, no codigo.
type ProveedorHTTP struct {
	baseURL string
	apiKey  string
	modelo  string
	cliente *http.Client
}

// NuevoProveedorHTTP arma el cliente una sola vez.
//
// El http.Client se reusa a proposito: trae dentro el pool de conexiones, asi
// que crear uno por peticion abriria un TLS nuevo cada vez. Y lleva Timeout
// propio porque el cliente por defecto de Go NO tiene ninguno: una respuesta
// que nunca llega dejaria la goroutine colgada para siempre.
func NuevoProveedorHTTP(baseURL, apiKey, modelo string, timeout time.Duration) *ProveedorHTTP {
	return &ProveedorHTTP{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		modelo:  modelo,
		cliente: &http.Client{Timeout: timeout},
	}
}

// maxRespuestaBytes limita lo que leemos del proveedor. Es un servicio ajeno:
// si un dia responde un cuerpo enorme (o alguien se mete en medio), no vamos a
// cargarlo entero en memoria.
const maxRespuestaBytes = 1 << 20 // 1 MiB

// Formato de la API estilo OpenAI. Son tipos privados porque nadie fuera de
// este archivo deberia tener que conocerlos: hacia afuera solo salen Mensaje
// y Respuesta, que son los tipos del dominio.
type mensajeAPI struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	// Solo en los mensajes del asistente que piden datos.
	ToolCalls []llamadaAPI `json:"tool_calls,omitempty"`

	// Solo en los mensajes con el resultado de una herramienta.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

type llamadaAPI struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		// Los argumentos viajan como un STRING con JSON adentro, no como un
		// objeto. Es raro, pero es el formato de la API y hay que respetarlo.
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type herramientaAPI struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type peticionAPI struct {
	Model       string       `json:"model"`
	Messages    []mensajeAPI `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`

	// omitempty: en la ronda final se llama sin herramientas, y mandar una
	// lista vacia confunde a algunos proveedores.
	Tools []herramientaAPI `json:"tools,omitempty"`
}

type respuestaAPI struct {
	Choices []struct {
		Message      mensajeAPI `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Temperatura baja: aqui no queremos creatividad. Cuando el agente empiece a
// leer datos, queremos que repita el numero que le dimos, no que lo adorne.
const temperatura = 0.2

// maxTokensRespuesta es el techo de lo que puede contestar. Un chat de
// finanzas responde en un parrafo; sin techo, una alucinacion larga se cobra
// igual que una respuesta util.
const maxTokensRespuesta = 700

func (p *ProveedorHTTP) Completar(ctx context.Context, sistema string, pasos []Paso, herramientas []Herramienta) (Respuesta, error) {
	mensajes := make([]mensajeAPI, 0, len(pasos)+1)
	mensajes = append(mensajes, mensajeAPI{Role: "system", Content: sistema})
	for _, paso := range pasos {
		mensajes = append(mensajes, mensajeDePaso(paso))
	}

	cuerpo, err := json.Marshal(peticionAPI{
		Model:       p.modelo,
		Messages:    mensajes,
		Temperature: temperatura,
		MaxTokens:   maxTokensRespuesta,
		Tools:       herramientasAPI(herramientas),
	})
	if err != nil {
		return Respuesta{}, fmt.Errorf("armando la peticion al modelo: %w", err)
	}

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(cuerpo))
	if err != nil {
		return Respuesta{}, fmt.Errorf("creando la peticion al modelo: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	res, err := p.cliente.Do(req)
	if err != nil {
		// Aqui caen la red caida y el timeout. El error de red se envuelve
		// pero NO se expone al usuario: puede traer la URL del proveedor.
		return Respuesta{}, fmt.Errorf("%w: %v", ErrProveedorNoDisponible, err)
	}
	defer res.Body.Close()

	crudo, err := io.ReadAll(io.LimitReader(res.Body, maxRespuestaBytes))
	if err != nil {
		return Respuesta{}, fmt.Errorf("%w: leyendo la respuesta: %v", ErrProveedorNoDisponible, err)
	}

	if res.StatusCode != http.StatusOK {
		return Respuesta{}, p.errorDeEstado(res.StatusCode, crudo)
	}

	var datos respuestaAPI
	if err := json.Unmarshal(crudo, &datos); err != nil {
		return Respuesta{}, fmt.Errorf("%w: respuesta ilegible: %v", ErrProveedorNoDisponible, err)
	}
	// Algunos proveedores responden 200 con el error adentro del cuerpo.
	if datos.Error != nil {
		return Respuesta{}, fmt.Errorf("%w: %s", ErrProveedorNoDisponible, datos.Error.Message)
	}
	if len(datos.Choices) == 0 {
		return Respuesta{}, ErrRespuestaVacia
	}

	mensaje := datos.Choices[0].Message
	llamadas := llamadasDeAPI(mensaje.ToolCalls)
	contenido := strings.TrimSpace(mensaje.Content)

	// Un mensaje sin texto es normal cuando lo que trae son llamadas a
	// herramientas: el modelo todavia no tiene nada que decir, esta pidiendo
	// datos. Vacio Y sin llamadas si es una respuesta inservible.
	if contenido == "" && len(llamadas) == 0 {
		return Respuesta{}, ErrRespuestaVacia
	}

	return Respuesta{
		Contenido:     contenido,
		Llamadas:      llamadas,
		TokensEntrada: datos.Usage.PromptTokens,
		TokensSalida:  datos.Usage.CompletionTokens,
	}, nil
}

// mensajeDePaso traduce una linea de la conversacion al formato de la API.
func mensajeDePaso(paso Paso) mensajeAPI {
	switch paso.Rol {
	case RolHerramienta:
		// El resultado vuelve atado al id de la llamada: asi el modelo sabe
		// cual de las herramientas que pidio produjo este dato.
		return mensajeAPI{
			Role:       "tool",
			Content:    paso.Contenido,
			ToolCallID: paso.LlamadaID,
			Name:       paso.Nombre,
		}

	case RolAgente:
		msg := mensajeAPI{Role: "assistant", Content: paso.Contenido}
		for _, ll := range paso.Llamadas {
			var api llamadaAPI
			api.ID = ll.ID
			api.Type = "function"
			api.Function.Name = ll.Nombre
			api.Function.Arguments = string(ll.Argumentos)
			msg.ToolCalls = append(msg.ToolCalls, api)
		}
		return msg

	default:
		return mensajeAPI{Role: "user", Content: paso.Contenido}
	}
}

func herramientasAPI(herramientas []Herramienta) []herramientaAPI {
	if len(herramientas) == 0 {
		return nil
	}

	lista := make([]herramientaAPI, 0, len(herramientas))
	for _, h := range herramientas {
		var api herramientaAPI
		api.Type = "function"
		api.Function.Name = h.Nombre
		api.Function.Description = h.Descripcion
		api.Function.Parameters = h.Parametros
		lista = append(lista, api)
	}
	return lista
}

func llamadasDeAPI(llamadas []llamadaAPI) []Llamada {
	if len(llamadas) == 0 {
		return nil
	}

	lista := make([]Llamada, 0, len(llamadas))
	for _, ll := range llamadas {
		argumentos := strings.TrimSpace(ll.Function.Arguments)
		// Una herramienta sin argumentos llega con el string vacio, y eso no
		// es JSON valido: se normaliza a un objeto vacio.
		if argumentos == "" {
			argumentos = "{}"
		}
		lista = append(lista, Llamada{
			ID:         ll.ID,
			Nombre:     ll.Function.Name,
			Argumentos: json.RawMessage(argumentos),
		})
	}
	return lista
}

// errorDeEstado separa "el proveedor esta mal" de "nosotros lo llamamos mal".
//
// La diferencia importa: un 429 o un 502 se le pasan al usuario como "intenta
// de nuevo en un momento" (503), mientras que un 401 es una llave vencida y
// tiene que quedar en la bitacora para que el dueno del servidor la arregle.
func (p *ProveedorHTTP) errorDeEstado(status int, cuerpo []byte) error {
	detalle := recortar(string(cuerpo), 300)

	switch {
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%w: el proveedor respondio %d: %s", ErrProveedorNoDisponible, status, detalle)
	default:
		// 401/403 (llave mala), 400 (modelo inexistente), 402 (sin saldo).
		// Nunca se incluye la llave en el mensaje: el cuerpo del error del
		// proveedor no la trae, y nosotros tampoco la agregamos.
		return fmt.Errorf("el proveedor rechazo la peticion con %d: %s", status, detalle)
	}
}

func recortar(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// RedactorDelModelo redacta textos sueltos con el mismo proveedor del chat.
//
// Existe para los avisos automaticos, que necesitan una frase bien escrita
// pero no una conversacion. Vive aqui para que el paquete que los genera no
// tenga que saber nada del formato del proveedor: le basta con este metodo.
type RedactorDelModelo struct {
	proveedor Proveedor
}

func NuevoRedactor(p Proveedor) *RedactorDelModelo {
	return &RedactorDelModelo{proveedor: p}
}

// Redactar manda una sola pregunta, sin historial y SIN herramientas: aqui el
// modelo no tiene nada que consultar, solo texto que reescribir.
func (r *RedactorDelModelo) Redactar(ctx context.Context, instrucciones, datos string) (string, error) {
	respuesta, err := r.proveedor.Completar(ctx, instrucciones,
		[]Paso{{Rol: RolUsuario, Contenido: datos}}, nil)
	if err != nil {
		return "", err
	}
	return respuesta.Contenido, nil
}
