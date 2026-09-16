package avisos

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// Rutas devuelve el sub-router de /api/notificaciones. Como en el resto de
// paquetes, el middleware de auth se pone al montarlo.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.Listar)
	r.Post("/leidas", h.MarcarLeidos)
	return r
}

type listaResponse struct {
	Avisos  []Aviso `json:"avisos"`
	SinLeer int     `json:"sin_leer"`
}

func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := usuarioPropio(w, r)
	if !ok {
		return
	}

	lista, sinLeer, err := h.store.Listar(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "avisos: listando")
		return
	}

	httpx.JSON(w, http.StatusOK, listaResponse{Avisos: lista, SinLeer: sinLeer})
}

// MarcarLeidos apaga la campana: el usuario abrio el panel y los vio.
func (h *Handler) MarcarLeidos(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := usuarioPropio(w, r)
	if !ok {
		return
	}

	if err := h.store.MarcarLeidos(r.Context(), usuarioID); err != nil {
		httpx.ErrorInterno(w, r, err, "avisos: marcando leidos")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// usuarioPropio devuelve el dueno de los avisos, o corta la peticion.
//
// Mismo criterio que el chat del asistente: un admin revisando la cuenta de un
// cliente ve sus movimientos —eso es parte de llevar el negocio— pero no los
// avisos que le llegaron a esa persona. Sin este corte, el middleware VerComo
// cambiaria el id del context y el GET devolveria los ajenos.
func usuarioPropio(w http.ResponseWriter, r *http.Request) (int64, bool) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return 0, false
	}

	if _, observando := httpx.Observador(r.Context()); observando {
		httpx.Error(w, http.StatusForbidden, "Los avisos de otra cuenta son privados.")
		return 0, false
	}

	return usuarioID, true
}
