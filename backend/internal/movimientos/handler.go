package movimientos

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/dinero"
	"finanzas/internal/httpx"
)

type Handler struct {
	store   *Store
	almacen *AlmacenFacturas
}

func NewHandler(store *Store, almacen *AlmacenFacturas) *Handler {
	return &Handler{store: store, almacen: almacen}
}

func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.Listar)
	r.Post("/", h.Crear)
	// Antes de /{id}: chi prefiere la ruta fija, pero asi se lee claro.
	r.Get("/exportar", h.Exportar)
	r.Get("/{id}", h.Obtener)
	r.Put("/{id}", h.Actualizar)
	r.Delete("/{id}", h.Eliminar)

	// PATCH (no PUT) porque modifica UN campo, no reemplaza el recurso entero.
	//
	// Por dentro ya no escribe el campo: "pagado" registra el abono que falta
	// y "pendiente" borra los abonos. El endpoint se queda igual para que el
	// boton de la lista y el asistente no tengan que saber nada de eso.
	r.Patch("/{id}/estado", h.CambiarEstado)

	// Los pagos parciales y el acuerdo de cuotas (ver handler_abonos.go).
	r.Get("/{id}/abonos", h.ListarAbonos)
	r.Post("/{id}/abonos", h.Abonar)
	r.Delete("/{id}/abonos/{abonoID}", h.BorrarAbono)

	r.Get("/{id}/cuotas", h.ListarCuotas)
	r.Put("/{id}/cuotas", h.GuardarAcuerdo)
	r.Delete("/{id}/cuotas", h.BorrarAcuerdo)

	r.Post("/{id}/factura", h.SubirFactura)
	r.Get("/{id}/factura", h.DescargarFactura)
	r.Delete("/{id}/factura", h.EliminarFactura)

	return r
}

// Dashboard se monta aparte, en /api/dashboard.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	resumen, err := h.store.Resumen(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "dashboard: calculando resumen")
		return
	}
	httpx.JSON(w, http.StatusOK, resumen)
}

// ---------------------------------------------------------------- listado

type listaResponse struct {
	Movimientos []Movimiento `json:"movimientos"`
	Total       int          `json:"total"`
	Limite      int          `json:"limite"`
	Offset      int          `json:"offset"`
}

func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	q := r.URL.Query()
	f := Filtros{
		CategoriaID: enteroDeQuery(q.Get("categoria_id")),
		Tipo:        strings.TrimSpace(q.Get("tipo")),
		Estado:      strings.TrimSpace(q.Get("estado")),
		Desde:       strings.TrimSpace(q.Get("desde")),
		Hasta:       strings.TrimSpace(q.Get("hasta")),
		MedioPagoID: enteroDeQuery(q.Get("medio_pago_id")),
		Texto:       strings.TrimSpace(q.Get("q")),
		AQuien:      strings.TrimSpace(q.Get("a_quien")),
		Limite:      int(enteroDeQuery(q.Get("limite"))),
		Offset:      int(enteroDeQuery(q.Get("offset"))),
	}

	// Los filtros tambien se validan: un valor basura aqui produciria un error
	// de Postgres (500) en vez de un mensaje claro.
	v := httpx.NuevoValidador()
	if f.Tipo != "" && !EsTipoValido(f.Tipo) {
		v.Check(false, "tipo", TiposValidosMsg)
	}
	if f.Estado != "" && !EsEstadoValido(f.Estado) {
		v.Check(false, "estado", EstadosValidosMsg)
	}
	if f.Desde != "" && !fechaValida(f.Desde) {
		v.Check(false, "desde", "Fecha inválida, usa el formato AAAA-MM-DD")
	}
	if f.Hasta != "" && !fechaValida(f.Hasta) {
		v.Check(false, "hasta", "Fecha inválida, usa el formato AAAA-MM-DD")
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	lista, total, err := h.store.Listar(r.Context(), usuarioID, f)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "movimientos: listando")
		return
	}

	limite := f.Limite
	if limite <= 0 || limite > 200 {
		limite = 50
	}

	httpx.JSON(w, http.StatusOK, listaResponse{
		Movimientos: lista,
		Total:       total,
		Limite:      limite,
		Offset:      f.Offset,
	})
}

func (h *Handler) Obtener(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	m, err := h.store.PorID(r.Context(), usuarioID, id)
	if errors.Is(err, ErrNoEncontrado) {
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "movimientos: obteniendo")
		return
	}
	httpx.JSON(w, http.StatusOK, m)
}

// ---------------------------------------------------------------- crear / editar

// Entrada es un movimiento tal como llega de afuera, sin validar todavia.
//
// Esta exportada porque el formulario no es la unica puerta: el asistente
// prepara movimientos a partir de lo que el usuario le dicta, y tiene que
// pasar por las MISMAS reglas. Si la validacion viviera solo dentro del
// handler, la segunda puerta terminaria con su propia copia — y el dia que
// cambie una regla, con su propio bug.
type Entrada struct {
	CategoriaID int64 `json:"categoria_id"`
	// MedioPagoID es por donde se mueve la plata. En un traslado, el ORIGEN.
	MedioPagoID int64 `json:"medio_pago_id"`
	// MedioCobroID solo se usa en un traslado: el medio DESTINO.
	MedioCobroID int64  `json:"medio_cobro_id"`
	Tipo         string `json:"tipo"`
	Monto        string `json:"monto"`
	Fecha        string `json:"fecha"`
	Descripcion  string `json:"descripcion"`
	AQuien       string `json:"a_quien"`
	Estado       string `json:"estado"`
	CobrarEl     string `json:"cobrar_el"`

	// Cubrir llega cuando el medio no alcanzaba y el usuario ya dijo de donde
	// salio el resto. Ver fondos.go.
	Cubrir *EntradaCubrir `json:"cubrir"`
}

type EntradaCubrir struct {
	Tipo        string `json:"tipo"` // me_prestaron, recibi o traslado
	CategoriaID int64  `json:"categoria_id"`
	AQuien      string `json:"a_quien"`
	CobrarEl    string `json:"cobrar_el"`
	OrigenID    int64  `json:"origen_id"`
}

// respuestaFaltaPlata es el 409 de "en ese medio no alcanza": el mensaje de
// siempre mas las cifras, para que el formulario pregunte de donde sale el resto.
type respuestaFaltaPlata struct {
	Error       string       `json:"error"`
	FaltaPlata  *FaltaPlata  `json:"falta_plata,omitempty"`
	QuedaEnRojo *QuedaEnRojo `json:"queda_en_rojo,omitempty"`
}

// ResponderFaltaPlata responde el 409 si err es un FaltaPlata o un
// QuedaEnRojo (ver fondos.go). Devuelve false si no es ninguno.
func ResponderFaltaPlata(w http.ResponseWriter, err error) bool {
	var falta *FaltaPlata
	if errors.As(err, &falta) {
		httpx.JSON(w, http.StatusConflict, respuestaFaltaPlata{Error: falta.Mensaje(), FaltaPlata: falta})
		return true
	}
	var rojo *QuedaEnRojo
	if errors.As(err, &rojo) {
		httpx.JSON(w, http.StatusConflict, respuestaFaltaPlata{Error: rojo.Mensaje(), QuedaEnRojo: rojo})
		return true
	}
	return false
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

	m, err := h.store.Crear(r.Context(), usuarioID, datos)
	if ResponderFaltaPlata(w, err) {
		return
	}
	if errors.Is(err, ErrCategoriaInvalida) {
		httpx.ErrorCampos(w, map[string]string{"categoria_id": "La categoría no existe"})
		return
	}
	if errors.Is(err, ErrMedioInvalido) {
		httpx.ErrorCampos(w, map[string]string{"medio_pago_id": "El medio de pago no existe"})
		return
	}
	if errors.Is(err, ErrCobroAntes) {
		httpx.ErrorCampos(w, map[string]string{"cobrar_el": "No puede ser antes del día en que prestaste"})
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "movimientos: creando")
		return
	}
	httpx.JSON(w, http.StatusCreated, m)
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

	m, err := h.store.Actualizar(r.Context(), usuarioID, id, datos)
	if ResponderFaltaPlata(w, err) {
		return
	}
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
	case errors.Is(err, ErrCategoriaInvalida):
		httpx.ErrorCampos(w, map[string]string{"categoria_id": "La categoría no existe"})
	case errors.Is(err, ErrMedioInvalido):
		httpx.ErrorCampos(w, map[string]string{"medio_pago_id": "El medio de pago no existe"})
	case errors.Is(err, ErrCobroAntes):
		httpx.ErrorCampos(w, map[string]string{"cobrar_el": "No puede ser antes del día en que prestaste"})
	case err != nil:
		httpx.ErrorInterno(w, r, err, "movimientos: actualizando")
	default:
		httpx.JSON(w, http.StatusOK, m)
	}
}

func (h *Handler) Eliminar(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	rutaFactura, err := h.store.Eliminar(r.Context(), usuarioID, id)
	if ResponderFaltaPlata(w, err) {
		return
	}
	if errors.Is(err, ErrNoEncontrado) {
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "movimientos: eliminando")
		return
	}

	// El archivo se borra DESPUES de que la fila ya no existe. Si esto falla,
	// queda un archivo huerfano en disco: molesto, pero inofensivo. Al reves
	// (borrar el archivo y que falle la fila) dejaria un movimiento apuntando
	// a una factura inexistente, que si es un error visible para el usuario.
	if rutaFactura != "" {
		if err := h.almacen.Eliminar(rutaFactura); err != nil {
			slog.Error("no se pudo borrar la factura del disco", "ruta", rutaFactura, "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// leerDatos decodifica y valida el body. Devuelve false si ya respondio error.
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

// Validar aplica las reglas del movimiento y devuelve los datos listos para el
// store, o el mapa de errores por campo (vacio si todo esta bien).
//
// No sabe nada de HTTP a proposito: asi la usan tanto el handler como el
// asistente, y las reglas del dinero viven en un solo sitio.
func Validar(req Entrada) (Datos, map[string]string) {
	req.Tipo = strings.TrimSpace(req.Tipo)
	req.Fecha = strings.TrimSpace(req.Fecha)
	req.Descripcion = strings.TrimSpace(req.Descripcion)
	req.AQuien = strings.TrimSpace(req.AQuien)
	req.Estado = strings.TrimSpace(req.Estado)
	req.CobrarEl = strings.TrimSpace(req.CobrarEl)

	v := httpx.NuevoValidador()

	v.Check(req.CategoriaID > 0, "categoria_id", "Selecciona una categoría")
	// El medio de pago es obligatorio al registrar: sin el, el resumen no
	// puede decir donde esta la plata y todo termina en "Sin registrar".
	v.Check(req.MedioPagoID > 0, "medio_pago_id", "Indica cómo fue el pago")
	v.Check(EsTipoValido(req.Tipo), "tipo", "Selecciona un tipo")

	monto, err := dinero.Normalizar(req.Monto)
	if err != nil {
		v.Check(false, "monto", err.Error())
	}

	if req.Fecha == "" {
		v.Check(false, "fecha", "Este campo es obligatorio")
	} else if !fechaValida(req.Fecha) {
		v.Check(false, "fecha", "Fecha inválida, usa el formato AAAA-MM-DD")
	}

	v.MaxLargo("descripcion", req.Descripcion, 500)

	datos := Datos{
		CategoriaID: req.CategoriaID,
		Tipo:        req.Tipo,
		Monto:       monto,
		Fecha:       req.Fecha,
		Descripcion: req.Descripcion,
	}

	if req.MedioPagoID > 0 {
		id := req.MedioPagoID
		datos.MedioPagoID = &id
	}

	switch {
	// ------------------------------------------------------------------
	// Traslado: de un medio a otro. Ni a_quien ni estado ni fecha acordada.
	// ------------------------------------------------------------------
	case req.Tipo == TipoTraslado:
		// El texto habla de "destino" y no de "medio de cobro" porque eso es
		// lo que el usuario ve en el formulario; el nombre de la columna es
		// cosa nuestra.
		if req.MedioCobroID <= 0 {
			v.Check(false, "medio_cobro_id", "Indica a qué medio pasó la plata")
		} else if req.MedioCobroID == req.MedioPagoID {
			// Un traslado de un medio a si mismo no mueve nada: es siempre un
			// error de digitacion, y dejarlo pasar llena la lista de ruido.
			v.Check(false, "medio_cobro_id", "El origen y el destino no pueden ser el mismo medio")
		} else {
			id := req.MedioCobroID
			datos.MedioCobroID = &id
		}

	// ------------------------------------------------------------------
	// Las dos deudas: piden a quien y en que va.
	// ------------------------------------------------------------------
	case EsDeuda(req.Tipo):
		v.Requerido("a_quien", req.AQuien)
		if req.AQuien != "" {
			v.MaxLargo("a_quien", req.AQuien, 100)
		}

		if req.Estado == "" {
			v.Check(false, "estado", "Indica si ya está saldada o sigue pendiente")
		} else if !EsEstadoValido(req.Estado) {
			v.Check(false, "estado", EstadosValidosMsg)
		}

		// La fecha acordada: cuando te pagan (preste) o cuando te toca pagar
		// (me_prestaron). Opcional. Las fechas AAAA-MM-DD se pueden comparar
		// como texto, y asi no hace falta convertirlas.
		if req.CobrarEl != "" {
			if !fechaValida(req.CobrarEl) {
				v.Check(false, "cobrar_el", "Fecha inválida, usa el formato AAAA-MM-DD")
			} else if fechaValida(req.Fecha) && req.CobrarEl < req.Fecha {
				v.Check(false, "cobrar_el", "No puede ser antes de la fecha del movimiento")
			}
		}

		if v.Valido() {
			aQuien, estado := req.AQuien, req.Estado
			datos.AQuien = &aQuien
			datos.Estado = &estado
			if req.CobrarEl != "" {
				cobrarEl := req.CobrarEl
				datos.CobrarEl = &cobrarEl
			}
		}
	}

	// Para 'recibi' y 'pague' no hay nada que agregar: a_quien, estado,
	// cobrar_el y el destino quedan en nil. No se da error si el cliente los
	// manda llenos — se ignoran, para que el formulario pueda cambiar de tipo
	// sin tener que limpiar campos. El CHECK de la base garantiza que asi
	// queden guardados.

	if req.Cubrir != nil && sacaPlata(req.Tipo) {
		datos.Cubrir = validarCubrir(v, req, *req.Cubrir)
	}

	if !v.Valido() {
		return Datos{}, v.Campos
	}

	return datos, nil
}

// validarCubrir revisa de donde dice el usuario que salio lo que faltaba. Los
// errores van con el prefijo "cubrir." para que el formulario los ponga junto
// a la pregunta y no junto a los campos del gasto.
func validarCubrir(v *httpx.Validador, req Entrada, c EntradaCubrir) *Cubrir {
	c.Tipo = strings.TrimSpace(c.Tipo)
	c.AQuien = strings.TrimSpace(c.AQuien)
	c.CobrarEl = strings.TrimSpace(c.CobrarEl)

	cubrir := &Cubrir{Tipo: c.Tipo, CategoriaID: c.CategoriaID}

	switch c.Tipo {
	case CubrirPrestamo:
		v.Check(c.AQuien != "", "cubrir.a_quien", "Indica quién te prestó")
		v.MaxLargo("cubrir.a_quien", c.AQuien, 100)
		cubrir.AQuien = c.AQuien
		if c.CobrarEl != "" {
			if !fechaValida(c.CobrarEl) {
				v.Check(false, "cubrir.cobrar_el", "Fecha inválida, usa el formato AAAA-MM-DD")
			} else if fechaValida(req.Fecha) && c.CobrarEl < req.Fecha {
				v.Check(false, "cubrir.cobrar_el", "No puede ser antes de la fecha del movimiento")
			} else {
				cobrarEl := c.CobrarEl
				cubrir.CobrarEl = &cobrarEl
			}
		}
	case CubrirIngreso:
		v.Check(c.CategoriaID > 0, "cubrir.categoria_id", "Indica por qué categoría entró la plata")
	case CubrirTraslado:
		if c.OrigenID <= 0 {
			v.Check(false, "cubrir.origen_id", "Indica de qué medio pasaste la plata")
		} else {
			v.Check(c.OrigenID != req.MedioPagoID, "cubrir.origen_id", "Tiene que ser otro medio, no el mismo del pago")
		}
		cubrir.OrigenID = c.OrigenID
	default:
		v.Check(false, "cubrir.tipo", "Indica si te prestaron, fue un ingreso o la pasaste de otro medio")
	}
	return cubrir
}

// CambiarEstado marca un prestamo como pagado o pendiente de un solo clic
// desde la lista, sin abrir el formulario de edicion.
//
// PATCH /api/movimientos/{id}/estado  {"estado":"pagado"}
func (h *Handler) CambiarEstado(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	var req struct {
		Estado string `json:"estado"`
		// Por donde se movio la plata al saldar. Opcional: si no se sabe,
		// el abono queda sin medio registrado.
		//
		// Se sigue aceptando el nombre viejo (medio_cobro_id) porque es el que
		// manda el asistente al confirmar una tarjeta que ya estaba preparada
		// antes de esta version.
		MedioID      int64 `json:"medio_id"`
		MedioCobroID int64 `json:"medio_cobro_id"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Estado = strings.TrimSpace(req.Estado)
	// 'parcial' no se puede pedir por aqui: sale de registrar un abono, no de
	// declararlo. Por eso la lista de este endpoint es mas corta que la de
	// EsEstadoValido.
	if req.Estado != EstadoPagado && req.Estado != EstadoPendiente {
		httpx.ErrorCampos(w, map[string]string{
			"estado": "Estado inválido: usa pagado o pendiente",
		})
		return
	}

	medioID := req.MedioID
	if medioID <= 0 {
		medioID = req.MedioCobroID
	}
	var medio *int64
	if medioID > 0 {
		medio = &medioID
	}

	m, err := h.store.CambiarEstado(r.Context(), usuarioID, id, req.Estado, medio)
	h.responderDeuda(w, r, m, err, "movimientos: cambiando estado")
}

// ---------------------------------------------------------------- facturas

func (h *Handler) SubirFactura(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	// Doble freno de tamano: MaxBytesReader corta la conexion si el cuerpo
	// entero se pasa, y ParseMultipartForm limita cuanto se guarda en RAM
	// (el resto va a archivos temporales). Sin el primero, un cliente podria
	// mandar un stream infinito y llenar el disco de la Raspberry.
	r.Body = http.MaxBytesReader(w, r.Body, MaxFacturaBytes+(1<<20))

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, ErrFacturaMuyGrande.Error())
			return
		}
		httpx.Error(w, http.StatusBadRequest, "No se pudo leer el archivo enviado")
		return
	}
	defer func() {
		// Libera los archivos temporales que creo ParseMultipartForm.
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	archivo, encabezado, err := r.FormFile("factura")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Falta el archivo en el campo \"factura\"")
		return
	}
	defer archivo.Close()

	// Verificamos que el movimiento exista ANTES de escribir en disco,
	// para no dejar basura por un id equivocado.
	if _, err := h.store.PorID(r.Context(), usuarioID, id); err != nil {
		if errors.Is(err, ErrNoEncontrado) {
			httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
			return
		}
		httpx.ErrorInterno(w, r, err, "factura: verificando movimiento")
		return
	}

	guardado, err := h.almacen.Guardar(archivo, encabezado)
	switch {
	case errors.Is(err, ErrFacturaMuyGrande):
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	case errors.Is(err, ErrFacturaTipo), errors.Is(err, ErrFacturaVacia):
		httpx.ErrorCampos(w, map[string]string{"factura": err.Error()})
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "factura: guardando archivo")
		return
	}

	anterior, err := h.store.GuardarFactura(r.Context(), usuarioID, id, guardado)
	if err != nil {
		// La fila no se actualizo: borramos el archivo recien escrito para no
		// dejarlo huerfano en disco.
		_ = h.almacen.Eliminar(guardado.Ruta)

		if errors.Is(err, ErrNoEncontrado) {
			httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
			return
		}
		httpx.ErrorInterno(w, r, err, "factura: asociando al movimiento")
		return
	}

	// Si el movimiento ya tenia factura, la reemplazamos y borramos la vieja.
	if anterior != "" {
		if err := h.almacen.Eliminar(anterior); err != nil {
			slog.Error("no se pudo borrar la factura anterior", "ruta", anterior, "error", err)
		}
	}

	m, err := h.store.PorID(r.Context(), usuarioID, id)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "factura: releyendo movimiento")
		return
	}
	httpx.JSON(w, http.StatusOK, m)
}

func (h *Handler) DescargarFactura(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	// La descarga pasa por este endpoint (y por tanto por el JWT) a proposito.
	// Servir /uploads con un FileServer dejaria las facturas accesibles a
	// cualquiera que adivinara la URL, sin autenticacion.
	ruta, nombre, tipo, err := h.store.RutaFactura(r.Context(), usuarioID, id)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
		return
	case errors.Is(err, ErrSinFactura):
		httpx.Error(w, http.StatusNotFound, "El movimiento no tiene factura")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "factura: consultando")
		return
	}

	archivo, err := h.almacen.Abrir(ruta)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "factura: abriendo archivo")
		return
	}
	defer archivo.Close()

	info, err := archivo.Stat()
	if err != nil {
		httpx.ErrorInterno(w, r, err, "factura: leyendo archivo")
		return
	}

	if tipo != "" {
		w.Header().Set("Content-Type", tipo)
	}
	// inline: el navegador muestra la imagen o el PDF en vez de descargarlo.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, nombre))
	// Las facturas no cambian nunca, pero son privadas: cache solo del lado
	// del usuario, nunca en un proxy compartido.
	w.Header().Set("Cache-Control", "private, max-age=3600")

	// ServeContent maneja solo los Range requests (que un PDF grande usa
	// para cargar por partes) y el header Last-Modified.
	http.ServeContent(w, r, nombre, info.ModTime(), archivo)
}

func (h *Handler) EliminarFactura(w http.ResponseWriter, r *http.Request) {
	usuarioID, id, ok := h.contexto(w, r)
	if !ok {
		return
	}

	ruta, err := h.store.QuitarFactura(r.Context(), usuarioID, id)
	switch {
	case errors.Is(err, ErrNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Movimiento no encontrado")
		return
	case errors.Is(err, ErrSinFactura):
		httpx.Error(w, http.StatusNotFound, "El movimiento no tiene factura")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "factura: quitando")
		return
	}

	if err := h.almacen.Eliminar(ruta); err != nil {
		slog.Error("no se pudo borrar la factura del disco", "ruta", ruta, "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- helpers

// contexto saca el usuario del token y el id de la URL, respondiendo el error
// si algo falta. Se repite en casi todos los handlers, asi que vive aparte.
func (h *Handler) contexto(w http.ResponseWriter, r *http.Request) (usuarioID, id int64, ok bool) {
	usuarioID, autenticado := httpx.UsuarioID(r.Context())
	if !autenticado {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return 0, 0, false
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return 0, 0, false
	}

	return usuarioID, id, true
}

func enteroDeQuery(valor string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(valor), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// fechaValida exige el formato ISO y descarta anios absurdos, que casi siempre
// son un dedazo (escribir 202 o 20266 en vez de 2026).
func fechaValida(valor string) bool {
	fecha, err := time.Parse(FormatoFecha, valor)
	if err != nil {
		return false
	}
	anio := fecha.Year()
	return anio >= 2000 && anio <= 2100
}
