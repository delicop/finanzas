package tiendas

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

type Handler struct {
	store *Store
	// permiso dice si el plan del usuario incluye esta seccion.
	permiso Permiso
	// archivos es el almacen de las fotos de los cierres (ver foto.go).
	// nil = este servidor no guarda archivos y esas rutas lo dicen.
	archivos Archivos
}

// Permiso responde si un usuario puede usar las tiendas. En la app es
// auth.Store.TieneTiendas: el plan del cliente las incluye o no.
//
// Es una funcion y no el store entero, igual que en el agente, para que este
// paquete no dependa de como se venden los planes y para que las pruebas
// puedan pasar una propia.
type Permiso func(ctx context.Context, usuarioID int64) (bool, error)

// NewHandler arma el handler. Sin permiso (nil) nadie entra: un olvido al
// conectarlo tiene que cerrar la puerta, no abrirsela a todos.
func NewHandler(store *Store, permiso Permiso, archivos Archivos) *Handler {
	return &Handler{store: store, permiso: permiso, archivos: archivos}
}

// Rutas devuelve el sub-router de /api/tiendas. Como en el resto de paquetes,
// el middleware de auth no se pone aqui sino al montarlo en el router.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()

	// El plan se revisa en CADA peticion, no al iniciar sesion: si el dueño le
	// quita las tiendas al plan, el corte es inmediato y no espera a que venza
	// el token. Va como middleware para que no exista forma de agregar una
	// ruta aqui y olvidarse de revisarlo.
	r.Use(h.exigirPlan)

	r.Get("/", h.Listar)
	r.Post("/", h.Crear)
	r.Put("/{id}", h.Actualizar)
	r.Delete("/{id}", h.Eliminar)

	// Los cierres de caja de cada tienda (ver cierre_handler.go). Cuelgan de
	// aqui y no de /api/cierres: una hoja siempre es de un local.
	r.Mount("/{id}/cierres", h.rutasCierres())

	return r
}

func (h *Handler) exigirPlan(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usuarioID, ok := httpx.UsuarioID(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "No autenticado")
			return
		}

		// Mientras un admin observa una cuenta ajena, el id del context ya es
		// el del cliente observado: lo que se revisa aqui es el plan de esa
		// persona, que es de quien son las tiendas que se van a mostrar.
		permitido := false
		if h.permiso != nil {
			var err error
			permitido, err = h.permiso(r.Context(), usuarioID)
			if err != nil {
				httpx.ErrorInterno(w, r, err, "tiendas: revisando el plan")
				return
			}
		}
		if !permitido {
			httpx.Error(w, http.StatusForbidden,
				"Tu plan no incluye las tiendas. Habla con el administrador para cambiarte a uno que las tenga.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

type tiendaRequest struct {
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
		httpx.ErrorInterno(w, r, err, "tiendas: listando")
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

	nombre, listo := leerNombre(w, r)
	if !listo {
		return
	}

	tienda, err := h.store.Crear(r.Context(), usuarioID, nombre)
	if errors.Is(err, ErrNombreDuplicado) {
		httpx.ErrorCampos(w, map[string]string{"nombre": "Ya existe una tienda con ese nombre"})
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: creando")
		return
	}

	httpx.JSON(w, http.StatusCreated, tienda)
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

	nombre, listo := leerNombre(w, r)
	if !listo {
		return
	}

	tienda, err := h.store.Actualizar(r.Context(), usuarioID, id, nombre)
	switch {
	case errors.Is(err, ErrNoEncontrada):
		httpx.Error(w, http.StatusNotFound, "Tienda no encontrada")
	case errors.Is(err, ErrNombreDuplicado):
		httpx.ErrorCampos(w, map[string]string{"nombre": "Ya existe una tienda con ese nombre"})
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: actualizando")
	default:
		httpx.JSON(w, http.StatusOK, tienda)
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
		httpx.Error(w, http.StatusNotFound, "Tienda no encontrada")
	case errors.Is(err, ErrTieneCierres):
		// 409 Conflict, igual que con los medios de pago: la peticion es
		// valida pero choca con lo que ya hay registrado.
		httpx.Error(w, http.StatusConflict,
			"No se puede eliminar: esta tienda tiene cierres de caja registrados.")
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: eliminando")
	default:
		// 204 No Content: borrado exitoso, no hay cuerpo que devolver.
		w.WriteHeader(http.StatusNoContent)
	}
}

// leerNombre decodifica y valida el body. Devuelve false si ya respondio error.
func leerNombre(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req tiendaRequest
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
