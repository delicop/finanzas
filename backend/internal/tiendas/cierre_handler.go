package tiendas

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/dinero"
	"finanzas/internal/httpx"
)

// Las rutas de los cierres cuelgan de una tienda (/api/tiendas/{id}/cierres):
// un cierre no existe por su cuenta, es la hoja de ESE local. Van dentro del
// router de tiendas, asi que ya pasaron por el mismo permiso del plan.
func (h *Handler) rutasCierres() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.CrearCierre)
	r.Get("/{cierreID}", h.VerCierre)
	r.Put("/{cierreID}", h.ActualizarCierre)
	r.Delete("/{cierreID}", h.EliminarCierre)

	// La foto de la hoja firmada (ver foto.go).
	r.Post("/{cierreID}/foto", h.SubirFoto)
	r.Get("/{cierreID}/foto", h.VerFoto)
	r.Delete("/{cierreID}/foto", h.QuitarFoto)

	return r
}

// RutasTodosLosCierres es /api/cierres: los de todas las tiendas juntos.
//
// Se monta aparte del router de tiendas, asi que repite el permiso del plan
// — es la unica forma de que no dependa de por donde se entre.
func (h *Handler) RutasTodosLosCierres() chi.Router {
	r := chi.NewRouter()
	r.Use(h.exigirPlan)
	r.Get("/", h.TodosLosCierres)
	r.Get("/exportar", h.ExportarCierres)
	return r
}

// limiteMaximoCierres topa cuanto se puede pedir de un tiron en /api/cierres,
// para que nadie pida el historico completo por accidente (o a proposito).
const limiteMaximoCierres = 100

func (h *Handler) TodosLosCierres(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	filtros, ok := filtrosDeQuery(w, r)
	if !ok {
		return
	}
	filtros.Limite, ok = limiteDeQuery(w, r)
	if !ok {
		return
	}

	lista, err := h.store.ListarTodosLosCierres(r.Context(), usuarioID, filtros)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: listando todos los cierres")
		return
	}
	httpx.JSON(w, http.StatusOK, lista)
}

// filtrosDeQuery lee y valida los filtros del listado. Un valor basura aqui
// produciria un error de Postgres (500) en vez de un mensaje claro.
func filtrosDeQuery(w http.ResponseWriter, r *http.Request) (FiltrosCierres, bool) {
	q := r.URL.Query()
	f := FiltrosCierres{
		Desde: strings.TrimSpace(q.Get("desde")),
		Hasta: strings.TrimSpace(q.Get("hasta")),
	}
	if id := strings.TrimSpace(q.Get("tienda_id")); id != "" {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "La tienda del filtro no es válida")
			return FiltrosCierres{}, false
		}
		f.TiendaID = n
	}

	v := httpx.NuevoValidador()
	for campo, valor := range map[string]string{"desde": f.Desde, "hasta": f.Hasta} {
		if valor == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", valor); err != nil {
			v.Check(false, campo, "La fecha no es válida")
		}
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return FiltrosCierres{}, false
	}
	return f, true
}

// limiteDeQuery lee ?limite=, con un tope para que nadie pida un millon de
// filas. Sin el parametro, cero: sin limite. Quien no la llame (Exportar,
// que necesita todo) deja el filtro en cero.
func limiteDeQuery(w http.ResponseWriter, r *http.Request) (int, bool) {
	valor := strings.TrimSpace(r.URL.Query().Get("limite"))
	if valor == "" {
		return 0, true
	}
	n, err := strconv.Atoi(valor)
	if err != nil || n < 0 {
		httpx.Error(w, http.StatusBadRequest, "El límite no es válido")
		return 0, false
	}
	if n > limiteMaximoCierres {
		n = limiteMaximoCierres
	}
	return n, true
}

// cierreRequest es la hoja como la manda el formulario. Todo el dinero llega
// como texto y se normaliza aqui, igual que en el resto de la app.
type cierreRequest struct {
	Fecha       string `json:"fecha"`
	Responsable string `json:"responsable"`

	QRBanco  string `json:"qr_banco"`
	QRTienda string `json:"qr_tienda"`

	DatafonoReporte string `json:"datafono_reporte"`
	DatafonoTienda  string `json:"datafono_tienda"`

	EfectivoBillete string `json:"efectivo_billete"`
	EfectivoMoneda  string `json:"efectivo_moneda"`
	EfectivoTienda  string `json:"efectivo_tienda"`

	VentaTienda string `json:"venta_tienda"`
	Novedades   string `json:"novedades"`

	Lineas []lineaRequest `json:"lineas"`
}

type lineaRequest struct {
	Grupo       string `json:"grupo"`
	Descripcion string `json:"descripcion"`
	Monto       string `json:"monto"`
}

// valida deja la hoja lista para guardar, o los errores por campo.
func (req *cierreRequest) valida() (DatosCierre, map[string]string) {
	v := httpx.NuevoValidador()

	d := DatosCierre{
		Fecha:       strings.TrimSpace(req.Fecha),
		Responsable: strings.TrimSpace(req.Responsable),
		Novedades:   strings.TrimSpace(req.Novedades),
		Lineas:      []Linea{},
	}

	v.Requerido("fecha", d.Fecha)
	if d.Fecha != "" {
		hoja, err := time.ParseInLocation("2006-01-02", d.Fecha, zonaColombia)
		switch {
		case err != nil:
			v.Check(false, "fecha", "La fecha no es válida")
		// Un cierre es el arqueo de un dia que ya termino: una fecha por delante
		// es siempre un dedazo, casi siempre en el año — que es justo lo que el
		// <input type="date"> deja escribir a mano. Hoy si pasa: la hoja se llena
		// al cerrar la caja, el mismo dia.
		case hoja.After(hoyEnColombia()):
			v.Check(false, "fecha", "Un cierre no puede ser de un día que todavía no llega")
		}
	}
	v.MaxLargo("responsable", d.Responsable, 120)
	v.MaxLargo("novedades", d.Novedades, 2000)

	// Las casillas vacias valen cero: la hoja de papel tambien se entrega con
	// renglones en blanco, y eso no es un error.
	casillas := []struct {
		campo   string
		origen  string
		destino *string
	}{
		{"qr_banco", req.QRBanco, &d.QRBanco},
		{"qr_tienda", req.QRTienda, &d.QRTienda},
		{"datafono_reporte", req.DatafonoReporte, &d.DatafonoReporte},
		{"datafono_tienda", req.DatafonoTienda, &d.DatafonoTienda},
		{"efectivo_billete", req.EfectivoBillete, &d.EfectivoBillete},
		{"efectivo_moneda", req.EfectivoMoneda, &d.EfectivoMoneda},
		{"efectivo_tienda", req.EfectivoTienda, &d.EfectivoTienda},
		{"venta_tienda", req.VentaTienda, &d.VentaTienda},
	}
	for _, c := range casillas {
		*c.destino = normalizarMonto(v, c.campo, c.origen)
	}

	// El campo del error se nombra por grupo y posicion dentro del grupo, no por
	// el indice del arreglo: asi la pantalla lo encuentra en su lista sin tener
	// que reconstruir en que orden se aplanaron las cinco.
	posicion := map[string]int{}
	for _, l := range req.Lineas {
		n := posicion[l.Grupo]
		posicion[l.Grupo]++

		descripcion := strings.TrimSpace(l.Descripcion)
		// Un renglon en blanco no es un error: es un renglon que no se uso.
		if descripcion == "" && strings.TrimSpace(l.Monto) == "" {
			continue
		}

		campo := fmt.Sprintf("lineas.%s.%d", l.Grupo, n)
		if !GrupoValido(l.Grupo) {
			v.Check(false, campo, "Ese grupo no existe")
			continue
		}
		v.Requerido(campo, descripcion)
		v.MaxLargo(campo, descripcion, 120)

		monto := normalizarMonto(v, campo, l.Monto)
		if descripcion != "" {
			d.Lineas = append(d.Lineas, Linea{
				Grupo: l.Grupo, Descripcion: descripcion, Monto: monto,
			})
		}
	}

	if !v.Valido() {
		return DatosCierre{}, v.Campos
	}
	return d, nil
}

// normalizarMonto convierte "1.441.000" en "1441000". Vacio = cero: es
// distinto de un valor malo, que si es un error.
func normalizarMonto(v *httpx.Validador, campo, valor string) string {
	if strings.TrimSpace(valor) == "" {
		return "0"
	}
	normalizado, err := dinero.Normalizar(valor)
	// El cero aqui es un dato, no un error: "hoy no hubo monedas" es algo que
	// la hoja dice. dinero.Normalizar lo rechaza porque un MOVIMIENTO de cero
	// no existe, que es otra pregunta.
	if errors.Is(err, dinero.ErrCero) {
		return "0"
	}
	if err != nil {
		v.Check(false, campo, err.Error())
		return "0"
	}
	return normalizado
}

func (h *Handler) VerCierre(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	id, ok := idDeParametro(w, r, "cierreID")
	if !ok {
		return
	}

	cierre, err := h.store.CierrePorID(r.Context(), usuarioID, tiendaID, id)
	switch {
	case errors.Is(err, ErrCierreNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: consultando cierre")
	default:
		httpx.JSON(w, http.StatusOK, cierre)
	}
}

func (h *Handler) CrearCierre(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	datos, ok := leerCierre(w, r)
	if !ok {
		return
	}

	cierre, err := h.store.CrearCierre(r.Context(), usuarioID, tiendaID, datos)
	switch {
	case errors.Is(err, ErrNoEncontrada):
		httpx.Error(w, http.StatusNotFound, "Tienda no encontrada")
	case errors.Is(err, ErrCierreDuplicado):
		httpx.ErrorCampos(w, map[string]string{
			"fecha": "Esta tienda ya tiene un cierre de ese día. Ábrelo y edítalo.",
		})
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: creando cierre")
	default:
		httpx.JSON(w, http.StatusCreated, cierre)
	}
}

func (h *Handler) ActualizarCierre(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	id, ok := idDeParametro(w, r, "cierreID")
	if !ok {
		return
	}
	datos, ok := leerCierre(w, r)
	if !ok {
		return
	}

	cierre, err := h.store.ActualizarCierre(r.Context(), usuarioID, tiendaID, id, datos)
	switch {
	case errors.Is(err, ErrCierreNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
	case errors.Is(err, ErrCierreDuplicado):
		httpx.ErrorCampos(w, map[string]string{
			"fecha": "Esta tienda ya tiene un cierre de ese día.",
		})
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: actualizando cierre")
	default:
		httpx.JSON(w, http.StatusOK, cierre)
	}
}

func (h *Handler) EliminarCierre(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	id, ok := idDeParametro(w, r, "cierreID")
	if !ok {
		return
	}

	ruta, err := h.store.EliminarCierre(r.Context(), usuarioID, tiendaID, id)
	switch {
	case errors.Is(err, ErrCierreNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: eliminando cierre")
		return
	}

	// Primero la fila y despues el archivo, igual que con las facturas: la
	// hoja ya no existe, y que falle borrar la foto no puede devolver error
	// porque el reintento del cliente encontraria un 404. Queda un archivo
	// huerfano, molesto pero inofensivo.
	if ruta != "" && h.archivos != nil {
		_ = h.archivos.Eliminar(ruta)
	}
	w.WriteHeader(http.StatusNoContent)
}

func leerCierre(w http.ResponseWriter, r *http.Request) (DatosCierre, bool) {
	var req cierreRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return DatosCierre{}, false
	}

	datos, campos := req.valida()
	if campos != nil {
		httpx.ErrorCampos(w, campos)
		return DatosCierre{}, false
	}
	return datos, true
}

// contextoTienda saca el usuario del token y la tienda de la URL.
func (h *Handler) contextoTienda(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return 0, 0, false
	}
	tiendaID, ok := idDeParametro(w, r, "id")
	if !ok {
		return 0, 0, false
	}
	return usuarioID, tiendaID, true
}

func idDeParametro(w http.ResponseWriter, r *http.Request, nombre string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, nombre), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return 0, false
	}
	return id, true
}
