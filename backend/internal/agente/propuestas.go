package agente

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
	"finanzas/internal/movimientos"
)

// Aqui vive la unica escritura del agente, y la hace el usuario.
//
// El modelo prepara; la persona revisa la tarjeta, corrige lo que quiera y da
// clic. Recien ahi se toca la base del dinero, por el mismo Store y con las
// mismas validaciones que el formulario de Movimientos.

// ConfirmarPropuesta ejecuta lo que el agente dejo preparado.
//
// El cuerpo trae los datos FINALES, que pueden no ser los que propuso el
// modelo: la tarjeta es editable y esa es media gracia del asunto. Por eso se
// validan otra vez, igual que si vinieran del formulario.
func (h *Handler) ConfirmarPropuesta(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	id, err := idDeRuta(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	propuesta, estado, err := h.store.Propuesta(r.Context(), usuarioID, id)
	if !h.propuestaUsable(w, r, propuesta, estado, err) {
		return
	}

	switch propuesta.Tipo {
	case TipoPropuestaMovimiento:
		h.confirmarMovimiento(w, r, usuarioID, propuesta.ID)
	case TipoPropuestaMarcarPagado:
		h.confirmarMarcarPagado(w, r, usuarioID, propuesta)
	default:
		// No deberia pasar: el tipo lo limita un CHECK de la base.
		httpx.ErrorInterno(w, r, errors.New("tipo de propuesta desconocido: "+propuesta.Tipo),
			"agente: confirmando propuesta")
	}
}

// DescartarPropuesta cierra la tarjeta sin escribir nada.
func (h *Handler) DescartarPropuesta(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	id, err := idDeRuta(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	err = h.store.ReservarPropuesta(r.Context(), usuarioID, id, EstadoPropuestaDescartada)
	switch {
	case errors.Is(err, ErrPropuestaResuelta):
		// Ya estaba resuelta (o no es suya): para el usuario el resultado es
		// el mismo, la tarjeta ya no esta.
		w.WriteHeader(http.StatusNoContent)
	case err != nil:
		httpx.ErrorInterno(w, r, err, "agente: descartando propuesta")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) confirmarMovimiento(w http.ResponseWriter, r *http.Request, usuarioID, propuestaID int64) {
	var entrada movimientos.Entrada
	if err := httpx.DecodeJSON(w, r, &entrada); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Primero se reserva y despues se escribe. Al reves, dos clics seguidos
	// (o dos pestanas) crearian dos movimientos identicos: el UPDATE
	// condicional es lo unico que puede decidir quien gana esa carrera.
	if err := h.store.ReservarPropuesta(r.Context(), usuarioID, propuestaID, EstadoPropuestaConfirmada); err != nil {
		h.responderReserva(w, r, err)
		return
	}

	m, campos, err := h.catalogo.CrearMovimiento(r.Context(), usuarioID, entrada)
	if campos != nil || err != nil {
		// No se creo nada: la tarjeta vuelve a estar disponible para que el
		// usuario corrija y reintente.
		h.devolverAPendiente(r, usuarioID, propuestaID)

		if campos != nil {
			httpx.ErrorCampos(w, campos)
			return
		}
		httpx.ErrorInterno(w, r, err, "agente: creando el movimiento propuesto")
		return
	}

	// Rastro: de que propuesta salio este movimiento. Si falla, el movimiento
	// ya existe y no vamos a tumbar la respuesta por una anotacion.
	if err := h.store.AnotarMovimiento(r.Context(), usuarioID, propuestaID, m.ID); err != nil {
		httpx.RegistrarFallo(r, err, "agente: anotando el movimiento de la propuesta")
	}

	httpx.JSON(w, http.StatusCreated, m)
}

func (h *Handler) confirmarMarcarPagado(w http.ResponseWriter, r *http.Request, usuarioID int64, propuesta *Propuesta) {
	var cuerpo struct {
		// Por donde le devolvieron la plata. 0 = sin registrar.
		MedioCobroID int64 `json:"medio_cobro_id"`
	}
	if err := httpx.DecodeJSON(w, r, &cuerpo); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Cual prestamo es lo dice la propuesta, no el cuerpo de la peticion: eso
	// ya quedo fijado cuando el usuario vio la tarjeta.
	var datos datosMarcarPagado
	if err := json.Unmarshal(propuesta.Datos, &datos); err != nil {
		httpx.ErrorInterno(w, r, err, "agente: leyendo la propuesta")
		return
	}

	if err := h.store.ReservarPropuesta(r.Context(), usuarioID, propuesta.ID, EstadoPropuestaConfirmada); err != nil {
		h.responderReserva(w, r, err)
		return
	}

	m, err := h.catalogo.MarcarPagado(r.Context(), usuarioID, datos.MovimientoID, cuerpo.MedioCobroID)
	if err != nil {
		h.devolverAPendiente(r, usuarioID, propuesta.ID)

		switch {
		case errors.Is(err, movimientos.ErrNoEncontrado):
			// El prestamo se borro entre la propuesta y el clic.
			httpx.Error(w, http.StatusNotFound, "Ese movimiento ya no existe")
		case errors.Is(err, movimientos.ErrNoEsPrestamo):
			httpx.Error(w, http.StatusConflict, "Ese movimiento no es un préstamo")
		case errors.Is(err, movimientos.ErrMedioInvalido):
			httpx.ErrorCampos(w, map[string]string{"medio_cobro_id": "El medio de pago no existe"})
		default:
			httpx.ErrorInterno(w, r, err, "agente: marcando el préstamo como pagado")
		}
		return
	}

	if err := h.store.AnotarMovimiento(r.Context(), usuarioID, propuesta.ID, m.ID); err != nil {
		httpx.RegistrarFallo(r, err, "agente: anotando el movimiento de la propuesta")
	}

	httpx.JSON(w, http.StatusOK, m)
}

// propuestaUsable revisa que la tarjeta siga viva antes de tocar nada.
func (h *Handler) propuestaUsable(w http.ResponseWriter, r *http.Request, propuesta *Propuesta, estado string, err error) bool {
	switch {
	case errors.Is(err, ErrPropuestaNoEncontrada):
		httpx.Error(w, http.StatusNotFound, "Esa propuesta no existe")
		return false
	case err != nil:
		httpx.ErrorInterno(w, r, err, "agente: consultando la propuesta")
		return false
	case estado != EstadoPropuestaPendiente:
		// 409: la peticion es valida pero choca con el estado actual.
		httpx.Error(w, http.StatusConflict, "Esa propuesta ya se había resuelto")
		return false
	case time.Since(propuesta.CreadaEn) > VigenciaPropuesta:
		// Confirmar hoy un "pagué 45 mil" de anteayer descuadra el mes sin que
		// nadie se entere. Mejor que lo vuelva a dictar.
		httpx.Error(w, http.StatusConflict,
			"Esa propuesta ya caducó. Pídesela otra vez al asistente.")
		return false
	}
	return true
}

func (h *Handler) responderReserva(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrPropuestaResuelta) {
		httpx.Error(w, http.StatusConflict, "Esa propuesta ya se había resuelto")
		return
	}
	httpx.ErrorInterno(w, r, err, "agente: reservando la propuesta")
}

// devolverAPendiente deshace la reserva cuando la escritura fallo. Si esto
// fallara tambien, lo unico que pasa es que la tarjeta queda resuelta sin
// haber creado nada: queda en la bitacora y el usuario puede volver a
// dictarlo.
func (h *Handler) devolverAPendiente(r *http.Request, usuarioID, propuestaID int64) {
	if err := h.store.DevolverPropuestaAPendiente(r.Context(), usuarioID, propuestaID); err != nil {
		httpx.RegistrarFallo(r, err, "agente: devolviendo la propuesta a pendiente")
	}
}

func idDeRuta(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
