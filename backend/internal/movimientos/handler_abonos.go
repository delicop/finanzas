package movimientos

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/dinero"
	"finanzas/internal/httpx"
)

// Los endpoints de los pagos parciales y del acuerdo de cuotas.
//
// Van en su propio archivo porque son otra conversacion: el resto del handler
// habla de "registrar plata que se movio", y esto habla de "cuanto falta de lo
// que ya se registro".

// ---------------------------------------------------------------- abonos

// ListarAbonos: GET /api/movimientos/{id}/abonos
func (h *Handler) ListarAbonos(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	lista, err := h.store.ListarAbonos(r.Context(), usuarioID, id)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "abonos: listando")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"abonos": lista})
}

// EntradaAbono es lo que manda el formulario de "registrar abono".
type EntradaAbono struct {
	Monto string `json:"monto"`
	Fecha string `json:"fecha"`
	// MedioID es por donde entro (o salio) el abono. Opcional.
	MedioID int64  `json:"medio_id"`
	Nota    string `json:"nota"`

	// Cubrir: al pagar una deuda propia sin fondos en el medio, de donde
	// salio la plata. Ver fondos.go.
	Cubrir *EntradaCubrir `json:"cubrir"`
}

// Abonar: POST /api/movimientos/{id}/abonos
func (h *Handler) Abonar(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	var req EntradaAbono
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	datos, campos := ValidarAbono(req)
	if len(campos) > 0 {
		httpx.ErrorCampos(w, campos)
		return
	}

	m, err := h.store.Abonar(r.Context(), usuarioID, id, datos)
	h.responderDeuda(w, r, m, err, "abonos: registrando")
}

// BorrarAbono: DELETE /api/movimientos/{id}/abonos/{abonoID}
//
// El id del movimiento va en la ruta aunque el store no lo necesite (el abono
// ya sabe de quien es): asi la URL se lee sola y el frontend no tiene que
// inventarse una ruta suelta /abonos/{id} que no cuelga de nada.
func (h *Handler) BorrarAbono(w http.ResponseWriter, r *http.Request) {
	usuarioID, _, ok := h.contexto(w, r)
	if !ok {
		return
	}

	abonoID, err := strconv.ParseInt(chi.URLParam(r, "abonoID"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id de abono inválido")
		return
	}

	m, err := h.store.BorrarAbono(r.Context(), usuarioID, abonoID)
	h.responderDeuda(w, r, m, err, "abonos: borrando")
}

// ValidarAbono aplica las reglas de un abono. Igual que Validar: no sabe de
// HTTP, para que el asistente use exactamente las mismas.
func ValidarAbono(req EntradaAbono) (AbonoDatos, map[string]string) {
	req.Fecha = strings.TrimSpace(req.Fecha)
	req.Nota = strings.TrimSpace(req.Nota)

	v := httpx.NuevoValidador()

	monto, err := dinero.Normalizar(req.Monto)
	if err != nil {
		v.Check(false, "monto", err.Error())
	}

	// Sin fecha se asume hoy: casi siempre el abono se anota el mismo dia que
	// pasa, y obligar a escribirlo solo agrega un paso.
	if req.Fecha == "" {
		req.Fecha = HoyEnColombia()
	} else if !fechaValida(req.Fecha) {
		v.Check(false, "fecha", "Fecha inválida, usa el formato AAAA-MM-DD")
	}

	v.MaxLargo("nota", req.Nota, 200)

	var cubrir *Cubrir
	if req.Cubrir != nil {
		cubrir = validarCubrir(v, Entrada{MedioPagoID: req.MedioID, Fecha: req.Fecha}, *req.Cubrir)
	}

	if !v.Valido() {
		return AbonoDatos{}, v.Campos
	}

	datos := AbonoDatos{Monto: monto, Fecha: req.Fecha, Nota: req.Nota, Cubrir: cubrir}
	if req.MedioID > 0 {
		id := req.MedioID
		datos.MedioID = &id
	}
	return datos, nil
}

// ---------------------------------------------------------------- cuotas

// ListarCuotas: GET /api/movimientos/{id}/cuotas
func (h *Handler) ListarCuotas(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	lista, err := h.store.ListarCuotas(r.Context(), usuarioID, id)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "cuotas: listando")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cuotas": lista})
}

// EntradaAcuerdo llega de dos formas, y las dos valen:
//
//   - con `cuotas` explicitas, cuando el acuerdo es a la medida
//     ("100 el viernes y 300 a fin de mes");
//   - con `cantidad` + `cada` + `primera`, cuando es parejo, que es lo normal
//     ("seis cuotas mensuales empezando el 5").
//
// La segunda forma la reparte el servidor y no el navegador a proposito: es
// una division de plata, y ahi es donde se cuelan los pesos perdidos.
type EntradaAcuerdo struct {
	Cuotas []EntradaCuota `json:"cuotas"`

	Cantidad int    `json:"cantidad"`
	Cada     string `json:"cada"`    // mensual | quincenal | semanal
	Primera  string `json:"primera"` // AAAA-MM-DD
	Total    string `json:"total"`   // lo que se reparte; vacio = el saldo
}

type EntradaCuota struct {
	VenceEl string `json:"vence_el"`
	Monto   string `json:"monto"`
}

// GuardarAcuerdo: PUT /api/movimientos/{id}/cuotas
//
// Reemplaza el acuerdo entero. Una lista vacia lo borra.
func (h *Handler) GuardarAcuerdo(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	var req EntradaAcuerdo
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// La forma "parejo" necesita saber cuanto repartir. Si no lo mandan, es el
	// saldo de la deuda, que es lo que casi siempre se quiere: un acuerdo se
	// hace sobre lo que falta, no sobre lo que se prestó hace tres meses.
	if len(req.Cuotas) == 0 && req.Cantidad > 0 && strings.TrimSpace(req.Total) == "" {
		m, err := h.store.PorID(r.Context(), usuarioID, id)
		if errors.Is(err, ErrNoEncontrado) {
			httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
			return
		}
		if err != nil {
			httpx.ErrorInterno(w, r, err, "cuotas: consultando el saldo")
			return
		}
		req.Total = m.Saldo
	}

	cuotas, campos := ValidarAcuerdo(req)
	if len(campos) > 0 {
		httpx.ErrorCampos(w, campos)
		return
	}

	lista, err := h.store.GuardarAcuerdo(r.Context(), usuarioID, id, cuotas)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
	case errors.Is(err, ErrNoEsDeuda):
		httpx.Error(w, http.StatusConflict, "Solo las deudas tienen acuerdo de pago")
	case errors.Is(err, ErrAcuerdoDeMas):
		httpx.ErrorCampos(w, map[string]string{"cuotas": "Las cuotas suman más que la deuda"})
	case errors.Is(err, ErrCuotaAntes):
		httpx.ErrorCampos(w, map[string]string{"primera": "Una cuota no puede vencer antes del movimiento"})
	case errors.Is(err, ErrMaximoDeCuotas):
		httpx.ErrorCampos(w, map[string]string{"cantidad": "Demasiadas cuotas"})
	case err != nil:
		httpx.ErrorInterno(w, r, err, "cuotas: guardando el acuerdo")
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"cuotas": lista})
	}
}

// ValidarAcuerdo arma la lista final de cuotas a partir de cualquiera de las
// dos formas de pedirlo.
func ValidarAcuerdo(req EntradaAcuerdo) ([]CuotaDatos, map[string]string) {
	v := httpx.NuevoValidador()

	// Forma 1: las cuotas vienen escritas una por una.
	if len(req.Cuotas) > 0 {
		if len(req.Cuotas) > MaxCuotas {
			v.Check(false, "cuotas", "Demasiadas cuotas")
			return nil, v.Campos
		}

		cuotas := make([]CuotaDatos, 0, len(req.Cuotas))
		for i, c := range req.Cuotas {
			monto, err := dinero.Normalizar(c.Monto)
			if err != nil {
				v.Check(false, "cuotas", "La cuota "+strconv.Itoa(i+1)+": "+err.Error())
				continue
			}
			if !fechaValida(strings.TrimSpace(c.VenceEl)) {
				v.Check(false, "cuotas", "La cuota "+strconv.Itoa(i+1)+" no tiene una fecha válida")
				continue
			}
			cuotas = append(cuotas, CuotaDatos{VenceEl: strings.TrimSpace(c.VenceEl), Monto: monto})
		}
		if !v.Valido() {
			return nil, v.Campos
		}
		return cuotas, nil
	}

	// Un acuerdo vacio no es un error: es "borra el acuerdo".
	if req.Cantidad <= 0 {
		return []CuotaDatos{}, nil
	}

	// Forma 2: parejo.
	if req.Cantidad > MaxCuotas {
		v.Check(false, "cantidad", "Demasiadas cuotas")
	}
	primera := strings.TrimSpace(req.Primera)
	if !fechaValida(primera) {
		v.Check(false, "primera", "Indica cuándo vence la primera cuota (AAAA-MM-DD)")
	}
	if !esFrecuenciaDeCuotas(req.Cada) {
		v.Check(false, "cada", "Indica cada cuánto: mensual, quincenal o semanal")
	}
	total, err := dinero.Normalizar(req.Total)
	if err != nil {
		v.Check(false, "total", err.Error())
	}
	if !v.Valido() {
		return nil, v.Campos
	}

	montos, err := dinero.Repartir(total, req.Cantidad)
	if err != nil {
		v.Check(false, "total", err.Error())
		return nil, v.Campos
	}

	inicio, err := time.Parse(FormatoFecha, primera)
	if err != nil {
		v.Check(false, "primera", "Fecha inválida, usa el formato AAAA-MM-DD")
		return nil, v.Campos
	}

	cuotas := make([]CuotaDatos, 0, req.Cantidad)
	for i := range req.Cantidad {
		cuotas = append(cuotas, CuotaDatos{
			VenceEl: vencimiento(inicio, req.Cada, i).Format(FormatoFecha),
			Monto:   montos[i],
		})
	}
	return cuotas, nil
}

func esFrecuenciaDeCuotas(cada string) bool {
	switch strings.TrimSpace(cada) {
	case "mensual", "quincenal", "semanal":
		return true
	}
	return false
}

// vencimiento corre la fecha de la primera cuota n veces.
//
// AddDate(0, n, 0) para lo mensual y no "n por 30 dias": si la primera cuota
// vence el 31 de enero, la siguiente vence el 28 de febrero (Go ajusta el
// desborde), no "30 dias despues". Es como se cuentan las cuotas de verdad.
func vencimiento(primera time.Time, cada string, n int) time.Time {
	switch cada {
	case "mensual":
		return primera.AddDate(0, n, 0)
	case "quincenal":
		return primera.AddDate(0, 0, 15*n)
	default: // semanal
		return primera.AddDate(0, 0, 7*n)
	}
}

// BorrarAcuerdo: DELETE /api/movimientos/{id}/cuotas
func (h *Handler) BorrarAcuerdo(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	if _, err := h.store.GuardarAcuerdo(r.Context(), usuarioID, id, nil); err != nil {
		if errors.Is(err, ErrNoEncontrado) {
			httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
			return
		}
		httpx.ErrorInterno(w, r, err, "cuotas: borrando el acuerdo")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- comun

// responderDeuda traduce los errores de abonar/desabonar, que son los mismos
// en los tres endpoints.
func (h *Handler) responderDeuda(w http.ResponseWriter, r *http.Request, m *Movimiento, err error, contexto string) {
	switch {
	case ResponderFaltaPlata(w, err):
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
	case errors.Is(err, ErrAbonoNoExiste):
		httpx.Error(w, http.StatusNotFound, "Ese abono no existe")
	case errors.Is(err, ErrNoEsDeuda), errors.Is(err, ErrNoEsPrestamo):
		// 409: la peticion es valida pero no aplica a este movimiento.
		httpx.Error(w, http.StatusConflict, "Solo los préstamos y las deudas propias llevan abonos y estado de pago")
	case errors.Is(err, ErrSinSaldo):
		httpx.Error(w, http.StatusConflict, "Esa deuda ya está saldada")
	case errors.Is(err, ErrAbonoDeMas):
		httpx.ErrorCampos(w, map[string]string{"monto": "El abono es mayor que lo que falta"})
	case errors.Is(err, ErrMedioInvalido):
		httpx.ErrorCampos(w, map[string]string{"medio_id": "El medio de pago no existe"})
	case err != nil:
		httpx.ErrorInterno(w, r, err, contexto)
	default:
		httpx.JSON(w, http.StatusOK, m)
	}
}
