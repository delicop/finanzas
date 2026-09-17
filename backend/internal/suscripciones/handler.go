package suscripciones

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/dinero"
	"finanzas/internal/httpx"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// Los sub-routers se montan bajo /api/admin, que ya va detras de RequireAdmin.
// No repiten el middleware aqui: un solo sitio donde mirar quien puede entrar.

func (h *Handler) RutasPlanes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListarPlanes)
	r.Post("/", h.CrearPlan)
	r.Put("/{id}", h.ActualizarPlan)
	r.Delete("/{id}", h.EliminarPlan)
	return r
}

func (h *Handler) RutasPagos() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListarPagos)
	r.Post("/", h.RegistrarPago)
	r.Delete("/{id}", h.EliminarPago)
	return r
}

/* ----------------------------- planes ----------------------------------- */

func (h *Handler) ListarPlanes(w http.ResponseWriter, r *http.Request) {
	lista, err := h.store.ListarPlanes(r.Context())
	if err != nil {
		httpx.ErrorInterno(w, r, err, "planes: listando")
		return
	}
	httpx.JSON(w, http.StatusOK, lista)
}

type planRequest struct {
	Nombre        string `json:"nombre"`
	PrecioMensual string `json:"precio_mensual"`
	// Vacio o ausente = el plan no se vende por año.
	PrecioAnual string `json:"precio_anual"`
	IncluyeIA   bool   `json:"incluye_ia"`
	Activo      *bool  `json:"activo"`
}

// valida deja los datos listos para guardar, o los errores por campo.
func (req *planRequest) valida() (DatosPlan, map[string]string) {
	v := httpx.NuevoValidador()

	d := DatosPlan{
		Nombre:    strings.TrimSpace(req.Nombre),
		IncluyeIA: req.IncluyeIA,
		// PUT: si no mandan "activo", el plan sigue activo. Desactivarlo es
		// explicito, nunca un efecto secundario de editarle el nombre.
		Activo: req.Activo == nil || *req.Activo,
	}
	v.Requerido("nombre", d.Nombre)
	v.MaxLargo("nombre", d.Nombre, 60)

	v.Requerido("precio_mensual", req.PrecioMensual)
	if strings.TrimSpace(req.PrecioMensual) != "" {
		// Mismo validador que los movimientos: el precio es plata y nunca
		// pasa por float, ni aqui ni en Postgres.
		normalizado, err := dinero.Normalizar(req.PrecioMensual)
		if err != nil {
			v.Check(false, "precio_mensual", err.Error())
		} else {
			d.PrecioMensual = normalizado
		}
	}

	if strings.TrimSpace(req.PrecioAnual) != "" {
		normalizado, err := dinero.Normalizar(req.PrecioAnual)
		if err != nil {
			v.Check(false, "precio_anual", err.Error())
		} else {
			d.PrecioAnual = normalizado
		}
	}

	if !v.Valido() {
		return DatosPlan{}, v.Campos
	}
	return d, nil
}

func (h *Handler) CrearPlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	datos, campos := req.valida()
	if campos != nil {
		httpx.ErrorCampos(w, campos)
		return
	}

	plan, err := h.store.CrearPlan(r.Context(), datos)
	if errors.Is(err, ErrNombreDuplicado) {
		httpx.ErrorCampos(w, map[string]string{"nombre": "Ya existe un plan con ese nombre"})
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "planes: creando")
		return
	}
	httpx.JSON(w, http.StatusCreated, plan)
}

func (h *Handler) ActualizarPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	var req planRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	datos, campos := req.valida()
	if campos != nil {
		httpx.ErrorCampos(w, campos)
		return
	}

	plan, err := h.store.ActualizarPlan(r.Context(), id, datos)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Plan no encontrado")
		return
	case errors.Is(err, ErrNombreDuplicado):
		httpx.ErrorCampos(w, map[string]string{"nombre": "Ya existe un plan con ese nombre"})
		return
	case errors.Is(err, ErrPlanConAnuales):
		httpx.ErrorCampos(w, map[string]string{
			"precio_anual": "Hay clientes pagando este plan por año. Pásalos a mensual " +
				"desde Clientes antes de quitarle el precio anual.",
		})
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "planes: actualizando")
		return
	}
	httpx.JSON(w, http.StatusOK, plan)
}

func (h *Handler) EliminarPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	err := h.store.EliminarPlan(r.Context(), id)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Plan no encontrado")
		return
	case errors.Is(err, ErrPlanEnUso):
		httpx.Error(w, http.StatusConflict,
			"El plan tiene clientes asignados. Múevelos a otro plan antes de borrarlo, "+
				"o desactívalo para dejar de ofrecerlo sin tocar a los que ya lo tienen.")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "planes: eliminando")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ------------------------- negocio y pagos ------------------------------ */

func (h *Handler) Negocio(w http.ResponseWriter, r *http.Request) {
	periodo, err := periodoDeQuery(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	resumen, err := h.store.Resumen(r.Context(), periodo)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "negocio: calculando resumen")
		return
	}
	httpx.JSON(w, http.StatusOK, resumen)
}

func (h *Handler) ListarPagos(w http.ResponseWriter, r *http.Request) {
	periodo, err := periodoDeQuery(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	lista, err := h.store.ListarPagos(r.Context(), periodo)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "pagos: listando")
		return
	}
	httpx.JSON(w, http.StatusOK, lista)
}

type pagoRequest struct {
	UsuarioID int64  `json:"usuario_id"`
	Periodo   string `json:"periodo"`
	// Vacio = el precio de lista del plan. Solo se manda para cobrar algo
	// distinto (un descuento puntual, un mes a medias).
	Monto    string `json:"monto"`
	PagadoEn string `json:"pagado_en"`
	Nota     string `json:"nota"`
}

func (h *Handler) RegistrarPago(w http.ResponseWriter, r *http.Request) {
	var req pagoRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	v := httpx.NuevoValidador()
	v.Check(req.UsuarioID > 0, "usuario_id", "Falta el cliente")

	periodo, err := normalizarPeriodo(req.Periodo)
	if err != nil {
		v.Check(false, "periodo", err.Error())
	}

	pagadoEn := strings.TrimSpace(req.PagadoEn)
	if pagadoEn == "" {
		pagadoEn = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", pagadoEn); err != nil {
		v.Check(false, "pagado_en", "La fecha debe ser AAAA-MM-DD")
	}

	monto := ""
	if strings.TrimSpace(req.Monto) != "" {
		normalizado, err := dinero.Normalizar(req.Monto)
		if err != nil {
			v.Check(false, "monto", err.Error())
		} else {
			monto = normalizado
		}
	}

	v.MaxLargo("nota", req.Nota, 200)

	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	pago, err := h.store.RegistrarPago(r.Context(), req.UsuarioID, periodo, monto, pagadoEn, req.Nota)
	switch {
	case errors.Is(err, ErrSinPlan):
		httpx.Error(w, http.StatusConflict,
			"Ese cliente no tiene plan asignado: no hay nada que cobrarle todavía.")
		return
	case errors.Is(err, ErrPagoDuplicado):
		// Puede ser un pago de este mismo mes o uno anual que ya lo incluye.
		httpx.Error(w, http.StatusConflict,
			"Ese cliente ya tiene cubierto ese periodo: hay un pago de este mes, "+
				"o uno anual que lo incluye.")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "pagos: registrando")
		return
	}

	httpx.JSON(w, http.StatusCreated, pago)
}

func (h *Handler) EliminarPago(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	err := h.store.EliminarPago(r.Context(), id)
	if errors.Is(err, ErrNoEncontrado) {
		httpx.Error(w, http.StatusNotFound, "Pago no encontrado")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "pagos: eliminando")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ----------------------------- helpers ---------------------------------- */

var formatoPeriodo = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// normalizarPeriodo pasa "2026-03" a "2026-03-01".
//
// El dia siempre es 1 porque un periodo es un MES, no una fecha. Guardarlo
// normalizado es lo que permite que la restriccion pagos_sin_solapar compare
// meses enteros: sin eso, "2026-03-15" dejaria media quincena sin cubrir.
func normalizarPeriodo(crudo string) (string, error) {
	s := strings.TrimSpace(crudo)
	if s == "" {
		return time.Now().Format("2006-01") + "-01", nil
	}
	if !formatoPeriodo.MatchString(s) {
		return "", fmt.Errorf("El periodo debe ser AAAA-MM")
	}
	return s + "-01", nil
}

func periodoDeQuery(r *http.Request) (string, error) {
	return normalizarPeriodo(r.URL.Query().Get("periodo"))
}

func idDeRuta(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return 0, false
	}
	return id, true
}
