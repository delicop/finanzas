package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/auth"
	"finanzas/internal/httpx"
	"finanzas/internal/medios"
)

// BorradorArchivos es lo unico que este paquete necesita saber del almacen de
// facturas. Es una interfaz y no el tipo concreto para que admin no tenga que
// importar movimientos entero: aqui no se sabe que es una factura, solo que
// hay archivos que se borran por su ruta.
type BorradorArchivos interface {
	Eliminar(rutaRelativa string) error
}

type Handler struct {
	store    *Store
	auth     *auth.Store
	medios   *medios.Store
	archivos BorradorArchivos
}

func NewHandler(store *Store, authStore *auth.Store, mediosStore *medios.Store, archivos BorradorArchivos) *Handler {
	return &Handler{store: store, auth: authStore, medios: mediosStore, archivos: archivos}
}

// Rutas devuelve el sub-router de /api/admin/usuarios.
//
// NO lleva el middleware de admin aqui dentro: lo pone el router principal al
// montarlo. Asi hay un solo sitio donde mirar para saber que esto esta cerrado,
// en vez de dos que pueden contradecirse.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.Listar)
	r.Post("/", h.Crear)
	r.Patch("/{id}", h.Actualizar)
	r.Post("/{id}/password", h.ResetearPassword)
	r.Delete("/{id}", h.Eliminar)
	r.Put("/{id}/plan", h.AsignarPlan)

	return r
}

// GET /api/admin/usuarios
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	lista, err := h.store.Listar(r.Context())
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: listando usuarios")
		return
	}
	httpx.JSON(w, http.StatusOK, lista)
}

type crearRequest struct {
	Email    string `json:"email"`
	Nombre   string `json:"nombre"`
	Password string `json:"password"`
	Rol      string `json:"rol"`
}

// POST /api/admin/usuarios
//
// Esto es lo que reemplaza al comando createuser para el dia a dia. El comando
// sigue existiendo porque es la unica forma de crear al PRIMER administrador:
// sin el no habria nadie que pudiera abrir este panel.
func (h *Handler) Crear(w http.ResponseWriter, r *http.Request) {
	var req crearRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(req.Rol) == "" {
		req.Rol = auth.RolUsuario
	}

	v := httpx.NuevoValidador()

	v.Requerido("email", req.Email)
	if strings.TrimSpace(req.Email) != "" {
		v.MaxLargo("email", req.Email, 255)
		v.Email("email", req.Email)
	}

	v.MaxLargo("nombre", req.Nombre, 120)

	v.Requerido("password", req.Password)
	if req.Password != "" {
		v.MinLargo("password", req.Password, 8)
		// bcrypt ignora todo lo que pase de 72 bytes.
		v.MaxLargo("password", req.Password, 72)
	}

	v.Check(auth.RolValido(req.Rol), "rol", "El rol debe ser admin o usuario")

	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: generando hash")
		return
	}

	usuario, err := h.auth.Crear(r.Context(), req.Email, req.Nombre, req.Rol, hash)
	if errors.Is(err, auth.ErrEmailDuplicado) {
		httpx.ErrorCampos(w, map[string]string{"email": "Ya existe una cuenta con ese correo"})
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: creando usuario")
		return
	}

	// Sin esto la cuenta nueva abre con la lista de medios de pago vacia y no
	// hay por donde registrar el primer movimiento.
	if err := h.medios.SembrarPorDefecto(r.Context(), usuario.ID); err != nil {
		// El usuario ya quedo creado y puede entrar: que falle el sembrado no
		// justifica responder un error. Queda en la bitacora y el usuario
		// puede crear sus medios a mano.
		httpx.ErrorInterno(w, r, err, "admin: sembrando medios del usuario nuevo")
		return
	}

	ficha, err := h.store.PorID(r.Context(), usuario.ID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: releyendo usuario creado")
		return
	}

	httpx.JSON(w, http.StatusCreated, ficha)
}

type actualizarRequest struct {
	Nombre *string `json:"nombre"`
	Rol    *string `json:"rol"`
	Activo *bool   `json:"activo"`
}

// PATCH /api/admin/usuarios/{id}
//
// PATCH y no PUT porque cambia solo los campos que llegan: el admin puede
// desactivar a alguien sin tener que reenviar su nombre y su rol, y por lo
// tanto sin riesgo de pisarlos con valores viejos que traia la pantalla.
func (h *Handler) Actualizar(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	var req actualizarRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	v := httpx.NuevoValidador()

	if req.Nombre != nil {
		v.MaxLargo("nombre", *req.Nombre, 120)
	}
	if req.Rol != nil {
		v.Check(auth.RolValido(*req.Rol), "rol", "El rol debe ser admin o usuario")
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	// Candados sobre uno mismo. La regla de "al menos un admin activo" que vive
	// en el store ya evita el desastre mayor, pero estos dos mensajes explican
	// el porque en vez de soltar un error generico, y frenan el caso mas
	// probable de todos: el dueno cerrandose la puerta con la llave por dentro.
	yo, _ := httpx.UsuarioID(r.Context())
	if id == yo {
		if req.Activo != nil && !*req.Activo {
			httpx.Error(w, http.StatusConflict, "No puedes desactivar tu propia cuenta")
			return
		}
		if req.Rol != nil && *req.Rol != auth.RolAdmin {
			httpx.Error(w, http.StatusConflict, "No puedes quitarte a ti mismo el rol de administrador")
			return
		}
	}

	ficha, err := h.store.Actualizar(r.Context(), id, Cambios{
		Nombre: req.Nombre,
		Rol:    req.Rol,
		Activo: req.Activo,
	})
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Usuario no encontrado")
		return
	case errors.Is(err, ErrUltimoAdmin):
		httpx.Error(w, http.StatusConflict,
			"Debe quedar al menos un administrador activo")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "admin: actualizando usuario")
		return
	}

	httpx.JSON(w, http.StatusOK, ficha)
}

type planRequest struct {
	// Nulo o ausente = quitarle el plan (deja de cobrarsele).
	PlanID *int64 `json:"plan_id"`
}

// PUT /api/admin/usuarios/{id}/plan
//
// PUT y no PATCH: reemplaza la asignacion entera. Mandar {"plan_id": null}
// significa "quitaselo", y eso solo es inequivoco si el campo siempre viene.
func (h *Handler) AsignarPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	var req planRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// 0 y null son lo mismo aqui: un select vacio en el formulario manda 0.
	planID := req.PlanID
	if planID != nil && *planID <= 0 {
		planID = nil
	}

	ficha, err := h.store.AsignarPlan(r.Context(), id, planID)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Usuario no encontrado")
		return
	case errors.Is(err, ErrPlanNoExiste):
		httpx.ErrorCampos(w, map[string]string{"plan_id": "Ese plan no existe"})
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "admin: asignando plan")
		return
	}

	httpx.JSON(w, http.StatusOK, ficha)
}

type resetRequest struct {
	Nueva string `json:"nueva"`
}

// POST /api/admin/usuarios/{id}/password
//
// El admin no necesita saber la contrasena actual del otro: para eso es el
// admin. Lo que si conviene es que despues le pida a esa persona que la cambie
// desde la app, porque el admin la conoce.
func (h *Handler) ResetearPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	var req resetRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	v := httpx.NuevoValidador()
	v.Requerido("nueva", req.Nueva)
	if req.Nueva != "" {
		v.MinLargo("nueva", req.Nueva, 8)
		v.MaxLargo("nueva", req.Nueva, 72)
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	hash, err := auth.HashPassword(req.Nueva)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: generando hash")
		return
	}

	err = h.auth.ActualizarPassword(r.Context(), id, hash)
	if errors.Is(err, auth.ErrNoEncontrado) {
		httpx.Error(w, http.StatusNotFound, "Usuario no encontrado")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: reseteando contrasena")
		return
	}

	// Igual que en el cambio de contrasena normal: los tokens ya emitidos
	// siguen vivos hasta que expiren. Si hay que echar a alguien YA, lo que
	// corta el acceso de inmediato es desactivar la cuenta, porque eso si lo
	// revisa el middleware en cada peticion.
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/admin/usuarios/{id}?email=<correo>
//
// Borra la cuenta y con ella TODO: movimientos, categorias, medios de pago y
// las facturas del disco. No hay papelera y no se puede deshacer.
//
// Por eso exige el correo de la cuenta como parametro y lo compara con el de
// la fila: un id en una URL es un numero que se equivoca facil (se borra el 3
// creyendo que era el 2 y se va el historial de otro cliente), mientras que
// escribir el correo completo obliga a mirar a quien se esta borrando.
//
// Para cortarle el acceso a alguien sin destruir su historial esta PATCH con
// {"activo": false}, que es casi siempre lo que de verdad se quiere.
func (h *Handler) Eliminar(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	yo, _ := httpx.UsuarioID(r.Context())
	if id == yo {
		httpx.Error(w, http.StatusConflict, "No puedes eliminar tu propia cuenta")
		return
	}

	usuario, err := h.store.PorID(r.Context(), id)
	if errors.Is(err, ErrNoEncontrado) {
		httpx.Error(w, http.StatusNotFound, "Usuario no encontrado")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "admin: consultando usuario a eliminar")
		return
	}

	confirmacion := auth.NormalizarEmail(r.URL.Query().Get("email"))
	if confirmacion != usuario.Email {
		httpx.Error(w, http.StatusBadRequest,
			"Para eliminar la cuenta hay que confirmar su correo exacto")
		return
	}

	rutas, err := h.store.Eliminar(r.Context(), id)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Usuario no encontrado")
		return
	case errors.Is(err, ErrUltimoAdmin):
		httpx.Error(w, http.StatusConflict, "Debe quedar al menos un administrador activo")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "admin: eliminando usuario")
		return
	}

	// Las filas ya no existen. Que falle borrar un archivo no puede devolver
	// error: el cliente reintentaria un borrado que ya ocurrio y recibiria un
	// 404 confuso. Se deja en el log, que es donde se revisa el disco.
	for _, ruta := range rutas {
		if err := h.archivos.Eliminar(ruta); err != nil {
			slog.Error("no se pudo borrar la factura de un usuario eliminado",
				"ruta", ruta, "usuario_id", id, "error", err)
		}
	}

	slog.Warn("cuenta eliminada",
		"usuario_id", id, "email", usuario.Email,
		"movimientos", usuario.Movimientos, "facturas", len(rutas),
		"eliminada_por", yo)

	w.WriteHeader(http.StatusNoContent)
}

func idDeRuta(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return 0, false
	}
	return id, true
}
