package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

type Handler struct {
	store    *Store
	tokens   *TokenManager
	limitado *limitador
}

func NewHandler(store *Store, tokens *TokenManager) *Handler {
	return &Handler{
		store:  store,
		tokens: tokens,
		// 10 intentos fallidos por IP cada 15 minutos. Suficiente margen para
		// quien se equivoca tecleando, y letal para un script de fuerza bruta
		// (960 intentos por dia contra un bcrypt de costo 12).
		limitado: nuevoLimitador(10, 15*time.Minute),
	}
}

// Rutas devuelve el sub-router de /api/auth.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()

	r.Post("/login", h.Login)

	// Todo lo que este dentro de este Group exige token valido.
	r.Group(func(priv chi.Router) {
		priv.Use(RequireAuth(h.tokens, h.store))
		priv.Get("/me", h.Me)
		priv.Post("/password", h.CambiarPassword)
	})

	return r
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token    string      `json:"token"`
	ExpiraEn time.Time   `json:"expira_en"`
	Usuario  usuarioJSON `json:"usuario"`
}

// usuarioJSON es lo que ve el frontend de si mismo. Incluye el rol porque de
// ahi sale si se pinta o no el menu de administracion. Ojo: eso es cosmetico.
// Quien de verdad decide es RequireAdmin en el servidor; un usuario que edite
// su propio localStorage vera el menu y recibira 403 en cuanto lo toque.
type usuarioJSON struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Nombre string `json:"nombre"`
	Rol    string `json:"rol"`
	// Si su plan incluye el asistente. Es para que la app no muestre un botón
	// que no sirve; quien de verdad lo impide es el backend, en cada mensaje.
	IA bool `json:"ia"`
}

// fichaDe arma lo que el frontend sabe de su usuario.
func (h *Handler) fichaDe(ctx context.Context, u *Usuario) (usuarioJSON, error) {
	ia, err := h.store.TieneIA(ctx, u.ID)
	if err != nil {
		return usuarioJSON{}, err
	}
	return usuarioJSON{ID: u.ID, Email: u.Email, Nombre: u.Nombre, Rol: u.Rol, IA: ia}, nil
}

// POST /api/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ip := ipDelRequest(r)
	if !h.limitado.permitir(ip) {
		w.Header().Set("Retry-After", "900")
		httpx.Error(w, http.StatusTooManyRequests,
			"Demasiados intentos fallidos. Intenta de nuevo en 15 minutos.")
		return
	}

	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	v := httpx.NuevoValidador()

	// Cada campo se valida por separado: si el password viene vacio, el usuario
	// igual debe enterarse de que el correo tambien esta mal, y no descubrirlo
	// en un segundo intento.
	v.Requerido("email", req.Email)
	if strings.TrimSpace(req.Email) != "" {
		v.MaxLargo("email", req.Email, 255)
		v.Email("email", req.Email)
	}

	v.Requerido("password", req.Password)
	if req.Password != "" {
		// bcrypt ignora todo lo que pase de 72 bytes: lo cortamos aqui
		// para que nadie crea que una clave de 200 caracteres es mas segura.
		v.MaxLargo("password", req.Password, 72)
	}

	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	usuario, err := h.store.PorEmail(r.Context(), req.Email)
	if err != nil && !errors.Is(err, ErrNoEncontrado) {
		httpx.ErrorInterno(w, r, err, "login: consultando usuario")
		return
	}

	// Respuesta identica si el email no existe o si la clave esta mala.
	// Si dijeramos "ese correo no existe" le estariamos confirmando a un
	// atacante cual es el correo valido del unico usuario del sistema.
	if err != nil || !VerificarPassword(usuario.PasswordHash, req.Password) {
		httpx.Error(w, http.StatusUnauthorized, "Correo o contraseña incorrectos")
		return
	}

	// La clave es correcta pero el admin cerro la cuenta. Aqui SI se dice el
	// motivo real: ya demostro ser el dueno del correo, asi que no le estamos
	// revelando nada que no sepa, y en cambio se ahorra llamar a soporte
	// creyendo que olvido la contrasena.
	if !usuario.Activo {
		httpx.Error(w, http.StatusForbidden,
			"Tu cuenta está desactivada. Habla con el administrador.")
		return
	}

	token, expira, err := h.tokens.Generar(usuario)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "login: generando token")
		return
	}

	ficha, err := h.fichaDe(r.Context(), usuario)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "login: armando la ficha")
		return
	}

	h.limitado.exito(ip)

	// Para el panel: desde cuando no aparece este cliente.
	h.store.RegistrarAcceso(r.Context(), usuario.ID)

	httpx.JSON(w, http.StatusOK, loginResponse{
		Token:    token,
		ExpiraEn: expira,
		Usuario:  ficha,
	})
}

// GET /api/auth/me
// Sirve para que el frontend valide al arrancar si el token guardado
// todavia es valido, sin obligar al usuario a escribir la clave otra vez.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	usuario, err := h.store.PorID(r.Context(), usuarioID)
	if errors.Is(err, ErrNoEncontrado) {
		// Token con firma valida pero de un usuario borrado.
		httpx.Error(w, http.StatusUnauthorized, "El usuario ya no existe")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "me: consultando usuario")
		return
	}

	ficha, err := h.fichaDe(r.Context(), usuario)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "me: armando la ficha")
		return
	}

	// Aqui pasa cada vez que alguien abre la app con la sesion ya iniciada,
	// que es la mayoria de las veces: es la señal buena de "sigue usandolo".
	// Un admin mirando la cuenta de un cliente NO cuenta como visita suya.
	if _, observando := httpx.Observador(r.Context()); !observando {
		h.store.RegistrarAcceso(r.Context(), usuarioID)
	}

	httpx.JSON(w, http.StatusOK, ficha)
}

type cambiarPasswordRequest struct {
	Actual string `json:"actual"`
	Nueva  string `json:"nueva"`
}

// POST /api/auth/password
//
// Pide la contrasena ACTUAL aunque el usuario ya este autenticado. Es la
// defensa estandar: si alguien se sienta en el computador con la sesion
// abierta, no puede cambiar la clave y dejar al dueno por fuera.
func (h *Handler) CambiarPassword(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	var req cambiarPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	v := httpx.NuevoValidador()
	v.Requerido("actual", req.Actual)
	v.Requerido("nueva", req.Nueva)
	if req.Nueva != "" {
		v.MinLargo("nueva", req.Nueva, 8)
		// bcrypt ignora lo que pase de 72 bytes.
		v.MaxLargo("nueva", req.Nueva, 72)
		v.Check(req.Nueva != req.Actual, "nueva", "La nueva contraseña debe ser distinta")
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	usuario, err := h.store.PorID(r.Context(), usuarioID)
	if errors.Is(err, ErrNoEncontrado) {
		httpx.Error(w, http.StatusUnauthorized, "El usuario ya no existe")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "password: consultando usuario")
		return
	}

	if !VerificarPassword(usuario.PasswordHash, req.Actual) {
		httpx.ErrorCampos(w, map[string]string{"actual": "La contraseña actual no es correcta"})
		return
	}

	hash, err := HashPassword(req.Nueva)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "password: generando hash")
		return
	}

	if err := h.store.ActualizarPassword(r.Context(), usuarioID, hash); err != nil {
		httpx.ErrorInterno(w, r, err, "password: guardando")
		return
	}

	// Nota: los tokens ya emitidos siguen siendo validos hasta que expiren.
	// Invalidarlos exigiria llevar una lista negra o un contador de version
	// por usuario; con un solo usuario y tokens de 24h no compensa.
	w.WriteHeader(http.StatusNoContent)
}
