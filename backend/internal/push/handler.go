package push

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

type Handler struct {
	store    *Store
	enviador *Enviador
}

func NewHandler(store *Store, enviador *Enviador) *Handler {
	return &Handler{store: store, enviador: enviador}
}

func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.Estado)
	r.Post("/", h.Suscribir)
	r.Delete("/", h.Desuscribir)

	return r
}

// Estado: GET /api/push
//
// Le dice al navegador la llave publica (que necesita para suscribirse) y si
// esta cuenta ya tiene algun dispositivo registrado.
func (h *Handler) Estado(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	cuantas, err := h.store.Cuantas(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "push: contando suscripciones")
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"clave_publica": h.enviador.ClavePublica(),
		"dispositivos":  cuantas,
	})
}

// Suscribir: POST /api/push
//
// El cuerpo es lo que devuelve pushManager.subscribe() en el navegador, tal
// cual. No hay nada que validar mas alla de que esten los tres campos: si las
// llaves no sirven, se descubre al primer envio y la suscripcion se borra sola.
func (h *Handler) Suscribir(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	var req struct {
		Endpoint string `json:"endpoint"`
		Llaves   struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	sus := Suscripcion{
		Endpoint: strings.TrimSpace(req.Endpoint),
		P256dh:   strings.TrimSpace(req.Llaves.P256dh),
		Auth:     strings.TrimSpace(req.Llaves.Auth),
	}

	v := httpx.NuevoValidador()
	v.Requerido("endpoint", sus.Endpoint)
	v.Requerido("p256dh", sus.P256dh)
	v.Requerido("auth", sus.Auth)
	// Solo https: un endpoint http no existe en ningun servicio de push real,
	// y aceptarlo seria mandar avisos en claro a saber donde.
	v.Check(strings.HasPrefix(sus.Endpoint, "https://"), "endpoint", "El endpoint debe ser https")
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	if err := h.store.Guardar(r.Context(), usuarioID, sus); err != nil {
		httpx.ErrorInterno(w, r, err, "push: guardando la suscripcion")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Desuscribir: DELETE /api/push?endpoint=...
//
// El endpoint va en la query y no en el cuerpo porque un DELETE con cuerpo es
// terreno resbaladizo (proxies que lo descartan, fetch que lo omite).
func (h *Handler) Desuscribir(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	if endpoint == "" {
		httpx.ErrorCampos(w, map[string]string{"endpoint": "Falta el endpoint"})
		return
	}

	if err := h.store.Borrar(r.Context(), usuarioID, endpoint); err != nil {
		httpx.ErrorInterno(w, r, err, "push: borrando la suscripcion")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
