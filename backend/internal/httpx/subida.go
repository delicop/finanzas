package httpx

import (
	"context"
	"net/http"
	"time"
)

// tiempoSubida es lo que tiene una subida de archivo para llegar completa.
const tiempoSubida = 3 * time.Minute

// PermitirSubidaLenta le da a una ruta de subida más tiempo que el límite
// general (30 s del servidor y del middleware Timeout): una foto o un PDF por
// datos móviles tarda fácil más que eso, y el servidor cortaba la lectura a
// la mitad. Devuelve la request con un contexto que no muere a los 30 s;
// quien llama tiene que ejecutar cancel.
func PermitirSubidaLenta(w http.ResponseWriter, r *http.Request) (*http.Request, context.CancelFunc) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(tiempoSubida))
	_ = rc.SetWriteDeadline(time.Now().Add(tiempoSubida + 30*time.Second))

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), tiempoSubida+15*time.Second)
	return r.WithContext(ctx), cancel
}
