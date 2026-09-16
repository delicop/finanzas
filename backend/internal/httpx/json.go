// Package httpx concentra los helpers de request/response para no repetir
// el mismo manejo de JSON y de errores en cada handler.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// maxBodyBytes limita el tamano del JSON que aceptamos. Sin esto, cualquiera
// puede mandar un body de 2 GB y tumbar la Raspberry por memoria.
// (Las facturas NO pasan por aqui: esas van como multipart, con su propio limite.)
const maxBodyBytes = 1 << 20 // 1 MiB

// ErrorResponse es el formato unico de error que consume el frontend.
// Campos: mensaje general + detalle por campo cuando es error de validacion.
type ErrorResponse struct {
	Error  string            `json:"error"`
	Campos map[string]string `json:"campos,omitempty"`
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// El status ya se envio, no podemos cambiarlo: solo dejamos rastro.
		slog.Error("no se pudo escribir la respuesta JSON", "error", err)
	}
}

func Error(w http.ResponseWriter, status int, mensaje string) {
	JSON(w, status, ErrorResponse{Error: mensaje})
}

// ErrorCampos responde 422 con el detalle de que campo fallo y por que.
func ErrorCampos(w http.ResponseWriter, campos map[string]string) {
	JSON(w, http.StatusUnprocessableEntity, ErrorResponse{
		Error:  "Datos inválidos",
		Campos: campos,
	})
}

// Registrador guarda los errores para poder revisarlos despues.
// Es una interfaz para que httpx no dependa del paquete que habla con la
// base de datos (si no, tendriamos un ciclo de imports).
type Registrador interface {
	GuardarError(r *http.Request, err error, contexto string, esPanico bool, traza string)
}

// registrador se configura UNA vez al arrancar, desde main.
// Es una variable de paquete porque ErrorInterno se llama como funcion suelta
// desde decenas de sitios; pasarla por parametro a cada handler seria ruido.
var registrador Registrador

func UsarRegistrador(r Registrador) { registrador = r }

// ErrorInterno registra el error real y devuelve un mensaje generico.
// Nunca le exponemos al cliente el detalle de un error de base de datos:
// eso filtra nombres de tablas y ayuda a un atacante.
func ErrorInterno(w http.ResponseWriter, r *http.Request, err error, contexto string) {
	RegistrarFallo(r, err, contexto)
	Error(w, http.StatusInternalServerError, "Error interno del servidor")
}

// RegistrarFallo deja el error en la bitacora SIN responder nada.
//
// Existe para los fallos que no son un 500: cuando un servicio ajeno se cae,
// al usuario le corresponde un 503 con un mensaje suyo ("intenta en un
// momento"), pero el dueno del servidor igual tiene que poder verlo en la
// bitacora para saber que fue lo que pasó.
func RegistrarFallo(r *http.Request, err error, contexto string) {
	slog.Error(contexto, "error", err)

	// Ademas del log, queda en la base para poder leerlo desde la app.
	if registrador != nil {
		registrador.GuardarError(r, err, contexto, false, "")
	}
}

// RegistrarPanico deja constancia de un panico recuperado por el middleware.
func RegistrarPanico(r *http.Request, err error, traza string) {
	slog.Error("panico recuperado", "error", err, "ruta", r.URL.Path)
	if registrador != nil {
		registrador.GuardarError(r, err, "panico", true, traza)
	}
}

// DecodeJSON lee el body en dst y devuelve un error ya legible para el usuario.
//
// Dos detalles que no trae encoding/json por defecto:
//   - DisallowUnknownFields: si el cliente manda un campo que no existe,
//     falla en vez de ignorarlo en silencio (atrapa typos temprano).
//   - La segunda llamada a Decode: garantiza que el body tenia UN solo objeto
//     JSON y no basura pegada despues.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		return errors.New("El Content-Type debe ser application/json")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxErr *http.MaxBytesError

		switch {
		case errors.As(err, &syntaxErr):
			return fmt.Errorf("JSON mal formado (posición %d)", syntaxErr.Offset)
		case errors.As(err, &typeErr):
			return fmt.Errorf("El campo %q tiene un tipo incorrecto", typeErr.Field)
		case errors.Is(err, io.EOF):
			return errors.New("El body no puede estar vacío")
		case errors.Is(err, io.ErrUnexpectedEOF):
			// JSON cortado a la mitad: pasa cuando se cae la conexión
			// o cuando el cliente arma mal el cuerpo.
			return errors.New("JSON mal formado: se cortó antes de terminar")
		case errors.As(err, &maxErr):
			return errors.New("El body es demasiado grande")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			campo := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("Campo desconocido: %s", campo)
		default:
			return errors.New("No se pudo leer el JSON")
		}
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("El body debe contener un solo objeto JSON")
	}

	return nil
}
