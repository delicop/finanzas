package categorias

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// Rutas devuelve el sub-router de /api/categorias.
// Ojo: el middleware de auth NO se pone aqui, se pone al montarlo en el router
// principal. Asi este paquete no depende del paquete auth.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.Listar)
	r.Post("/", h.Crear)
	r.Put("/{id}", h.Actualizar)
	r.Delete("/{id}", h.Eliminar)
	return r
}

type categoriaRequest struct {
	Nombre string `json:"nombre"`
}

func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	lista, err := h.store.Listar(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "categorias: listando")
		return
	}
	httpx.JSON(w, http.StatusOK, lista)
}

func (h *Handler) Crear(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	nombre, listo := h.leerNombre(w, r)
	if !listo {
		return
	}

	categoria, err := h.store.Crear(r.Context(), usuarioID, nombre)
	if errors.Is(err, ErrNombreDuplicado) {
		httpx.ErrorCampos(w, map[string]string{"nombre": "Ya existe una categoría con ese nombre"})
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "categorias: creando")
		return
	}

	httpx.JSON(w, http.StatusCreated, categoria)
}

func (h *Handler) Actualizar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	id, err := idDeRuta(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	nombre, listo := h.leerNombre(w, r)
	if !listo {
		return
	}

	categoria, err := h.store.Actualizar(r.Context(), usuarioID, id, nombre)
	switch {
	case errors.Is(err, ErrNoEncontrada):
		httpx.Error(w, http.StatusNotFound, "Categoría no encontrada")
	case errors.Is(err, ErrNombreDuplicado):
		httpx.ErrorCampos(w, map[string]string{"nombre": "Ya existe una categoría con ese nombre"})
	case err != nil:
		httpx.ErrorInterno(w, r, err, "categorias: actualizando")
	default:
		httpx.JSON(w, http.StatusOK, categoria)
	}
}

func (h *Handler) Eliminar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	id, err := idDeRuta(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	err = h.store.Eliminar(r.Context(), usuarioID, id)
	switch {
	case errors.Is(err, ErrNoEncontrada):
		httpx.Error(w, http.StatusNotFound, "Categoría no encontrada")
	case errors.Is(err, ErrTieneMovimientos):
		// 409 Conflict: la peticion es valida pero choca con el estado actual.
		// No borramos en cascada a proposito: se perderian registros de dinero.
		httpx.Error(w, http.StatusConflict,
			"No se puede eliminar: la categoría tiene movimientos. Muévelos o elimínalos primero.")
	case err != nil:
		httpx.ErrorInterno(w, r, err, "categorias: eliminando")
	default:
		// 204 No Content: borrado exitoso, no hay cuerpo que devolver.
		w.WriteHeader(http.StatusNoContent)
	}
}

// leerNombre decodifica y valida el body. Devuelve false si ya respondio error.
func (h *Handler) leerNombre(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req categoriaRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return "", false
	}

	nombre := strings.TrimSpace(req.Nombre)

	v := httpx.NuevoValidador()
	v.Requerido("nombre", nombre)
	if nombre != "" {
		v.MaxLargo("nombre", nombre, 60)
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return "", false
	}

	return nombre, true
}

func idDeRuta(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
