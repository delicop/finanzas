package recurrentes

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/dinero"
	"finanzas/internal/httpx"
	"finanzas/internal/movimientos"
)

type Handler struct {
	store *Store
	// El store de movimientos: confirmar una ocurrencia crea un movimiento de
	// verdad, por la MISMA puerta que el formulario. Aqui no hay un camino
	// propio a la tabla del dinero.
	movimientos *movimientos.Store
}

func NewHandler(store *Store, mov *movimientos.Store) *Handler {
	return &Handler{store: store, movimientos: mov}
}

func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()

	// Las rutas fijas van antes que /{id}: chi las prefiere igual, pero asi se
	// lee claro que "pendientes" no es un id.
	r.Get("/pendientes", h.ListarPendientes)
	r.Post("/pendientes/{id}/confirmar", h.Confirmar)
	r.Delete("/pendientes/{id}", h.Descartar)

	r.Get("/", h.Listar)
	r.Post("/", h.Crear)
	r.Put("/{id}", h.Actualizar)
	r.Delete("/{id}", h.Eliminar)

	return r
}

// --------------------------------------------------------------- plantillas

func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	lista, err := h.store.Listar(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "recurrentes: listando")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recurrentes": lista})
}

// Entrada es lo que manda el formulario.
type Entrada struct {
	CategoriaID int64  `json:"categoria_id"`
	MedioPagoID int64  `json:"medio_pago_id"`
	Tipo        string `json:"tipo"`
	Monto       string `json:"monto"`
	Descripcion string `json:"descripcion"`
	Frecuencia  string `json:"frecuencia"`
	Dia         int    `json:"dia"`
	Desde       string `json:"desde"`
	Hasta       string `json:"hasta"`
	Activo      *bool  `json:"activo"`
}

func (h *Handler) Crear(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	datos, listo := h.leerDatos(w, r)
	if !listo {
		return
	}

	rec, err := h.store.Crear(r.Context(), usuarioID, datos)
	h.responder(w, r, rec, err, http.StatusCreated, "recurrentes: creando")
}

func (h *Handler) Actualizar(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	datos, listo := h.leerDatos(w, r)
	if !listo {
		return
	}

	rec, err := h.store.Actualizar(r.Context(), usuarioID, id, datos)
	h.responder(w, r, rec, err, http.StatusOK, "recurrentes: actualizando")
}

func (h *Handler) Eliminar(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	err := h.store.Eliminar(r.Context(), usuarioID, id)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Ese gasto recurrente no existe")
	case err != nil:
		httpx.ErrorInterno(w, r, err, "recurrentes: eliminando")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) leerDatos(w http.ResponseWriter, r *http.Request) (Datos, bool) {
	var req Entrada
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return Datos{}, false
	}

	datos, campos := Validar(req)
	if len(campos) > 0 {
		httpx.ErrorCampos(w, campos)
		return Datos{}, false
	}
	return datos, true
}

// Validar aplica las reglas de un recurrente. Sin HTTP, igual que en
// movimientos: asi las reglas viven en un solo sitio.
func Validar(req Entrada) (Datos, map[string]string) {
	req.Tipo = strings.TrimSpace(req.Tipo)
	req.Descripcion = strings.TrimSpace(req.Descripcion)
	req.Frecuencia = strings.TrimSpace(req.Frecuencia)
	req.Desde = strings.TrimSpace(req.Desde)
	req.Hasta = strings.TrimSpace(req.Hasta)

	v := httpx.NuevoValidador()

	v.Check(req.CategoriaID > 0, "categoria_id", "Selecciona una categoría")
	v.Check(req.MedioPagoID > 0, "medio_pago_id", "Indica cómo se paga")

	// Solo gastos e ingresos. Un prestamo recurrente no existe: cada prestamo
	// es un acuerdo distinto, con su persona y su fecha.
	v.Check(req.Tipo == movimientos.TipoRecibi || req.Tipo == movimientos.TipoPague,
		"tipo", "Un gasto recurrente solo puede ser Recibí o Pagué")

	monto, err := dinero.Normalizar(req.Monto)
	if err != nil {
		v.Check(false, "monto", err.Error())
	}

	// La descripcion SI es obligatoria aqui, al reves que en un movimiento
	// suelto: es lo unico que distingue "Arriendo" de "Internet" en una lista
	// de plantillas que se ven todos los meses.
	v.Requerido("descripcion", req.Descripcion)
	v.MaxLargo("descripcion", req.Descripcion, 200)

	if !EsFrecuenciaValida(req.Frecuencia) {
		v.Check(false, "frecuencia", "Indica cada cuánto: mensual, quincenal o semanal")
	} else if !DiaValido(req.Frecuencia, req.Dia) {
		if req.Frecuencia == Semanal {
			v.Check(false, "dia", "Elige un día de la semana")
		} else {
			v.Check(false, "dia", "El día del mes va entre 1 y 31")
		}
	}

	if req.Desde == "" {
		v.Check(false, "desde", "Indica desde cuándo aplica")
	} else if !fechaValida(req.Desde) {
		v.Check(false, "desde", "Fecha inválida, usa el formato AAAA-MM-DD")
	}

	if req.Hasta != "" {
		if !fechaValida(req.Hasta) {
			v.Check(false, "hasta", "Fecha inválida, usa el formato AAAA-MM-DD")
		} else if req.Hasta < req.Desde {
			v.Check(false, "hasta", "No puede ser antes de la fecha de inicio")
		}
	}

	if !v.Valido() {
		return Datos{}, v.Campos
	}

	datos := Datos{
		CategoriaID: req.CategoriaID,
		MedioPagoID: req.MedioPagoID,
		Tipo:        req.Tipo,
		Monto:       monto,
		Descripcion: req.Descripcion,
		Frecuencia:  req.Frecuencia,
		Dia:         req.Dia,
		Desde:       req.Desde,
		// Activo por omision: quien crea un recurrente lo quiere andando.
		Activo: req.Activo == nil || *req.Activo,
	}
	if req.Hasta != "" {
		hasta := req.Hasta
		datos.Hasta = &hasta
	}
	return datos, nil
}

// -------------------------------------------------------------- ocurrencias

func (h *Handler) ListarPendientes(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	lista, err := h.store.Pendientes(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "recurrentes: listando pendientes")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"pendientes": lista})
}

// Confirmar crea el movimiento de esta ocurrencia.
//
// El cuerpo puede traer los datos FINALES, que pueden no ser los de la
// plantilla: el recibo de la luz cambia todos los meses, y corregir el monto
// antes de confirmar es media gracia del asunto. Si no viene nada, se usa la
// plantilla tal cual.
func (h *Handler) Confirmar(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	o, err := h.store.Ocurrencia(r.Context(), usuarioID, id)
	if errors.Is(err, ErrOcurrenciaNoExiste) {
		httpx.Error(w, http.StatusNotFound, "Ese pendiente no existe")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "recurrentes: consultando el pendiente")
		return
	}
	if o.Estado != Pendiente {
		// 409: la peticion es valida pero choca con el estado actual.
		httpx.Error(w, http.StatusConflict, "Ese pendiente ya se había resuelto")
		return
	}

	// Lo que llegue del cliente pisa a la plantilla, campo por campo.
	entrada := movimientos.Entrada{
		CategoriaID: o.CategoriaID,
		MedioPagoID: o.MedioPagoID,
		Tipo:        o.Tipo,
		Monto:       o.Monto,
		Fecha:       o.Fecha,
		Descripcion: o.Descripcion,
	}
	// Sin cuerpo se confirma la plantilla tal cual, que es el caso de un solo
	// clic en "Sí, lo pagué". Con cuerpo, lo que venga pisa a la plantilla.
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &entrada); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	datos, campos := movimientos.Validar(entrada)
	if len(campos) > 0 {
		httpx.ErrorCampos(w, campos)
		return
	}

	// Primero se reserva y despues se escribe. Al reves, dos clics seguidos
	// (o dos pestañas) crearian dos arriendos: el UPDATE condicional de
	// Resolver es lo unico que puede decidir quien gana esa carrera.
	if err := h.store.Resolver(r.Context(), usuarioID, id, Confirmada, nil); err != nil {
		h.responderReserva(w, r, err)
		return
	}

	m, err := h.movimientos.Crear(r.Context(), usuarioID, datos)
	if err != nil {
		// No se creo nada: el pendiente vuelve a estar disponible para que el
		// usuario corrija y reintente.
		h.devolverAPendiente(r, usuarioID, id)

		switch {
		case errors.Is(err, movimientos.ErrCategoriaInvalida):
			httpx.ErrorCampos(w, map[string]string{"categoria_id": "La categoría no existe"})
		case errors.Is(err, movimientos.ErrMedioInvalido):
			httpx.ErrorCampos(w, map[string]string{"medio_pago_id": "El medio de pago no existe"})
		default:
			httpx.ErrorInterno(w, r, err, "recurrentes: creando el movimiento")
		}
		return
	}

	// Rastro: de que pendiente salio este movimiento. Si falla, el movimiento
	// ya existe y no vamos a tumbar la respuesta por una anotacion.
	h.anotarMovimiento(r, usuarioID, id, m.ID)

	httpx.JSON(w, http.StatusCreated, m)
}

// Descartar cierra el pendiente sin crear nada ("este mes no lo pagué").
func (h *Handler) Descartar(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	err := h.store.Resolver(r.Context(), usuarioID, id, Descartada, nil)
	switch {
	case errors.Is(err, ErrOcurrenciaNoExiste):
		httpx.Error(w, http.StatusNotFound, "Ese pendiente no existe")
	case errors.Is(err, ErrYaResuelta):
		// Para el usuario el resultado es el mismo: la tarjeta ya no está.
		w.WriteHeader(http.StatusNoContent)
	case err != nil:
		httpx.ErrorInterno(w, r, err, "recurrentes: descartando")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ------------------------------------------------------------------ comunes

func (h *Handler) responder(w http.ResponseWriter, r *http.Request, rec *Recurrente, err error, ok int, contexto string) {
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Ese gasto recurrente no existe")
	case errors.Is(err, ErrCategoriaInvalida):
		httpx.ErrorCampos(w, map[string]string{"categoria_id": "La categoría no existe"})
	case errors.Is(err, ErrMedioInvalido):
		httpx.ErrorCampos(w, map[string]string{"medio_pago_id": "El medio de pago no existe"})
	case err != nil:
		httpx.ErrorInterno(w, r, err, contexto)
	default:
		httpx.JSON(w, ok, rec)
	}
}

func (h *Handler) responderReserva(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrOcurrenciaNoExiste):
		httpx.Error(w, http.StatusNotFound, "Ese pendiente no existe")
	case errors.Is(err, ErrYaResuelta):
		httpx.Error(w, http.StatusConflict, "Ese pendiente ya se había resuelto")
	default:
		httpx.ErrorInterno(w, r, err, "recurrentes: reservando el pendiente")
	}
}

// devolverAPendiente y anotarMovimiento son tareas de CIERRE de una escritura,
// y por eso no usan r.Context() a secas: si el usuario cierra la pestaña justo
// cuando falla la escritura, ese context ya esta cancelado y el "deshacer"
// fallaria tambien, dejando el pendiente marcado como confirmado sin que
// exista el movimiento. WithoutCancel conserva los valores del request (el
// usuario, el id de la peticion) pero no hereda la cancelacion.
func (h *Handler) devolverAPendiente(r *http.Request, usuarioID, id int64) {
	ctx, cancelar := contextoDeCierre(r)
	defer cancelar()

	if err := h.store.DevolverAPendiente(ctx, usuarioID, id); err != nil {
		httpx.RegistrarFalloSiempre(r, err, "recurrentes: devolviendo el pendiente")
	}
}

func (h *Handler) anotarMovimiento(r *http.Request, usuarioID, id, movimientoID int64) {
	ctx, cancelar := contextoDeCierre(r)
	defer cancelar()

	if err := h.store.AnotarMovimiento(ctx, usuarioID, id, movimientoID); err != nil {
		httpx.RegistrarFalloSiempre(r, err, "recurrentes: anotando el movimiento")
	}
}

func contextoDeCierre(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
}

func (h *Handler) contexto(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return 0, 0, false
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return 0, 0, false
	}
	return usuarioID, id, true
}

func fechaValida(valor string) bool {
	_, err := time.Parse(formatoFecha, valor)
	return err == nil
}
