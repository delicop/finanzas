package registro

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"

	"finanzas/internal/httpx"
)

// ---------------------------------------------------------------- adaptador

// Adaptador conecta httpx con este Store.
//
// httpx no puede importar este paquete (este importa httpx, habría un ciclo),
// así que httpx define la interfaz y aquí la implementamos.
type Adaptador struct {
	store *Store
}

func NuevoAdaptador(store *Store) *Adaptador { return &Adaptador{store: store} }

func (a *Adaptador) GuardarError(r *http.Request, err error, contexto string, esPanico bool, traza string) {
	e := Error{
		Contexto:  contexto,
		Mensaje:   err.Error(),
		EsPanico:  esPanico,
		Traza:     traza,
		RequestID: middleware.GetReqID(r.Context()),
		Ruta:      r.URL.Path,
		Metodo:    r.Method,
	}
	if id, ok := httpx.UsuarioID(r.Context()); ok {
		e.UsuarioID = &id
	}

	a.store.Registrar(e)
}

// ---------------------------------------------------------------- pánicos

// Recuperador reemplaza al Recoverer de chi para, además de evitar que el
// servidor se caiga, dejar el pánico guardado con su traza.
//
// Un pánico casi siempre es un bug nuestro (un puntero nil, un índice fuera
// de rango). Sin la traza es muy difícil encontrarlo después.
func Recuperador(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			// http.ErrAbortHandler es la forma normal de cortar una respuesta
			// a media: no es una falla y no hay que registrarla.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}

			// Un panic puede llevar cualquier valor, no solo un error.
			err, ok := rec.(error)
			if !ok {
				err = fmt.Errorf("%v", rec)
			}

			httpx.RegistrarPanico(r, err, string(debug.Stack()))
			httpx.Error(w, http.StatusInternalServerError, "Error interno del servidor")
		}()

		next.ServeHTTP(w, r)
	})
}
