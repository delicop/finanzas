package registro

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

// Handler expone la bitácora para consultarla desde afuera con curl.
//
// Va montada bajo /api/mantenimiento y protegida con un token PROPIO, no con
// el login del cliente. Dos razones:
//
//  1. El cliente no debe poder llegar a esto ni escribiendo la URL a mano.
//  2. Quien mantiene la app puede revisar el servidor sin pedirle la
//     contraseña al cliente ni iniciar sesión como él.
type Handler struct {
	store *Store
	token string
}

func NewHandler(store *Store, token string) *Handler {
	return &Handler{store: store, token: token}
}

// NewHandlerSesion es la misma bitacora pero SIN el token propio, para colgarla
// del panel de administracion. Ahi la puerta ya la cuida RequireAdmin: pedir
// ademas el token de mantenimiento obligaria al dueno a copiarlo del .env cada
// vez que quiere ver los errores desde su propia app.
func NewHandlerSesion(store *Store) *Handler {
	return &Handler{store: store}
}

// Rutas monta la bitacora. Cuando el handler se construyo con un token propio
// (NewHandler) exige ese token; cuando se construyo para el panel
// (NewHandlerSesion) confia en el middleware que lo envuelve.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()
	if h.token != "" {
		r.Use(h.exigirToken)
	}

	r.Get("/", h.Listar)
	r.Get("/conteo", h.Conteo)
	r.Patch("/{id}", h.MarcarResuelto)
	r.Delete("/resueltos", h.BorrarResueltos)

	return r
}

// exigirToken compara el token con el configurado.
func (h *Handler) exigirToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enviado := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))

		// subtle.ConstantTimeCompare y no "==": una comparación normal corta
		// apenas encuentra el primer carácter distinto, y midiendo cuánto
		// tarda en responder se puede adivinar el token letra por letra.
		iguales := subtle.ConstantTimeCompare([]byte(enviado), []byte(h.token)) == 1

		if !iguales {
			// 404 y no 401: para quien no tenga el token, estas rutas
			// simplemente no existen.
			httpx.Error(w, http.StatusNotFound, "No encontrado")
			return
		}

		next.ServeHTTP(w, r)
	})
}

type listaResponse struct {
	Errores []Error `json:"errores"`
	Total   int     `json:"total"`
	Limite  int     `json:"limite"`
	Offset  int     `json:"offset"`
}

// GET /api/mantenimiento/errores?pendientes=1&limite=20
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limite, _ := strconv.Atoi(q.Get("limite"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	f := Filtros{
		SoloPendientes: q.Get("pendientes") == "1",
		Limite:         limite,
		Offset:         offset,
	}

	lista, total, err := h.store.Listar(r.Context(), f)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "mantenimiento: listando errores")
		return
	}

	if f.Limite <= 0 || f.Limite > 200 {
		f.Limite = 50
	}

	httpx.JSON(w, http.StatusOK, listaResponse{
		Errores: lista,
		Total:   total,
		Limite:  f.Limite,
		Offset:  f.Offset,
	})
}

// GET /api/mantenimiento/errores/conteo
func (h *Handler) Conteo(w http.ResponseWriter, r *http.Request) {
	pendientes, total, err := h.store.Conteo(r.Context())
	if err != nil {
		httpx.ErrorInterno(w, r, err, "mantenimiento: contando errores")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{
		"pendientes": pendientes,
		"total":      total,
	})
}

// PATCH /api/mantenimiento/errores/{id}   {"resuelto":true}
//
// Para marcar lo que ya se corrigió y no volver a revisarlo.
func (h *Handler) MarcarResuelto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	var req struct {
		Resuelto bool `json:"resuelto"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	e, err := h.store.MarcarResuelto(r.Context(), id, req.Resuelto)
	if errors.Is(err, ErrNoEncontrado) {
		httpx.Error(w, http.StatusNotFound, "Error no encontrado")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "mantenimiento: marcando error")
		return
	}

	httpx.JSON(w, http.StatusOK, e)
}

// DELETE /api/mantenimiento/errores/resueltos
func (h *Handler) BorrarResueltos(w http.ResponseWriter, r *http.Request) {
	n, err := h.store.BorrarResueltos(r.Context())
	if err != nil {
		httpx.ErrorInterno(w, r, err, "mantenimiento: borrando resueltos")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int64{"borrados": n})
}
