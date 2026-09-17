package agente

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"finanzas/internal/httpx"
)

type Handler struct {
	store     *Store
	proveedor Proveedor
	catalogo  *Catalogo

	// limiteDiario es cuantos mensajes puede mandar un usuario en 24 horas.
	// Cada mensaje le cuesta plata al dueno del servidor: sin techo, un
	// cliente con un script le deja la factura del mes en la mano.
	limiteDiario int

	// permiso dice si el plan del usuario incluye el asistente.
	permiso Permiso
}

// Permiso responde si un usuario puede usar el asistente. En la app es
// auth.Store.TieneIA: el plan del cliente lo incluye o no.
//
// Es una funcion y no el store entero para que este paquete no dependa de
// como se venden los planes, y para que las pruebas puedan pasar una propia.
type Permiso func(ctx context.Context, usuarioID int64) (bool, error)

// NewHandler arma el handler. Sin permiso (nil) nadie puede usar el chat: un
// olvido al conectarlo debe cerrar la puerta, no abrirsela a todos.
func NewHandler(store *Store, proveedor Proveedor, catalogo *Catalogo, limiteDiario int, permiso Permiso) *Handler {
	return &Handler{store: store, proveedor: proveedor, catalogo: catalogo,
		limiteDiario: limiteDiario, permiso: permiso}
}

// Rutas devuelve el sub-router de /api/agente.
// Como en el resto de paquetes, el middleware de auth no se pone aqui sino al
// montarlo en el router principal.
func (h *Handler) Rutas() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.Conversacion)
	r.Delete("/", h.Borrar)
	r.Post("/mensajes", h.Enviar)

	// Las escrituras del agente pasan SIEMPRE por aqui: un clic del usuario
	// sobre una tarjeta que ya vio. El modelo no tiene forma de llamar a
	// estas rutas.
	r.Post("/propuestas/{id}/confirmar", h.ConfirmarPropuesta)
	r.Delete("/propuestas/{id}", h.DescartarPropuesta)
	return r
}

type mensajeRequest struct {
	Texto string `json:"texto"`
}

type conversacionResponse struct {
	ID       int64     `json:"id"`
	Mensajes []Mensaje `json:"mensajes"`

	// Propuestas pendientes: las tarjetas sin confirmar sobreviven a recargar
	// la pagina. Si desaparecieran, el usuario creeria que se guardo algo.
	Propuestas []Propuesta `json:"propuestas"`

	// Restantes es lo que le queda al usuario en la ventana de 24 horas. Va
	// en la respuesta para que la app pueda avisarle ANTES de que se choque
	// con el 429.
	Restantes int `json:"restantes"`
}

type envioResponse struct {
	ConversacionID int64       `json:"conversacion_id"`
	Mensaje        Mensaje     `json:"mensaje"`
	Propuestas     []Propuesta `json:"propuestas"`
	Restantes      int         `json:"restantes"`
}

// Conversacion devuelve el hilo abierto del usuario.
func (h *Handler) Conversacion(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	restantes, err := h.restantes(r, usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: contando mensajes")
		return
	}

	propuestas, err := h.store.PropuestasPendientes(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: listando propuestas")
		return
	}

	conv, err := h.store.Activa(r.Context(), usuarioID)
	if errors.Is(err, ErrNoEncontrada) {
		// Todavia no ha escrito nunca: hilo vacio, no un 404. Para la app es
		// el estado normal de la primera visita, no un error.
		httpx.JSON(w, http.StatusOK, conversacionResponse{
			Mensajes:   []Mensaje{},
			Propuestas: propuestas,
			Restantes:  restantes,
		})
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: leyendo la conversacion")
		return
	}

	httpx.JSON(w, http.StatusOK, conversacionResponse{
		ID:         conv.ID,
		Mensajes:   conv.Mensajes,
		Propuestas: propuestas,
		Restantes:  restantes,
	})
}

// Borrar empieza de cero: se van las conversaciones y sus mensajes.
func (h *Handler) Borrar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	if err := h.store.BorrarTodo(r.Context(), usuarioID); err != nil {
		httpx.ErrorInterno(w, r, err, "agente: borrando la conversacion")
		return
	}

	// El limite no se toca: vive en agente_consumo, que es otra tabla justo
	// para que borrar el hilo no devuelva los mensajes del dia.
	w.WriteHeader(http.StatusNoContent)
}

// Enviar guarda el mensaje del usuario, se lo pasa al modelo con el hilo
// reciente y guarda la respuesta.
func (h *Handler) Enviar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	texto, listo := leerTexto(w, r)
	if !listo {
		return
	}

	usados, err := h.store.MensajesRecientes(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: contando mensajes")
		return
	}
	if usados >= h.limiteDiario {
		// 429: la peticion es valida, simplemente ya gasto su cuota.
		httpx.Error(w, http.StatusTooManyRequests,
			"Llegaste al límite de mensajes por hoy. Puedes seguir escribiéndole al agente mañana.")
		return
	}

	conversacionID, err := h.conversacionAbierta(r, usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: abriendo la conversacion")
		return
	}

	// El consumo se anota al aceptar el mensaje, no al responderlo. Si se
	// cobrara solo el exito, un proveedor fallando en bucle seria un reintento
	// infinito gratis contra la cuota.
	if err := h.store.RegistrarConsumo(r.Context(), usuarioID); err != nil {
		httpx.ErrorInterno(w, r, err, "agente: registrando consumo")
		return
	}

	// Se guarda ANTES de llamar al modelo: si el proveedor falla, el usuario
	// tiene que seguir viendo lo que escribio en vez de perderlo.
	if _, err := h.store.GuardarMensaje(r.Context(), usuarioID, conversacionID, MensajeNuevo{Rol: RolUsuario, Contenido: texto}); err != nil {
		httpx.ErrorInterno(w, r, err, "agente: guardando el mensaje del usuario")
		return
	}

	historial, err := h.store.Historial(r.Context(), usuarioID, conversacionID, MensajesDeContexto)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: leyendo el hilo")
		return
	}

	nombre, err := h.store.NombreUsuario(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: consultando el nombre")
		return
	}

	respuesta, err := h.responder(r.Context(), usuarioID, nombre, historial)
	if err != nil {
		h.responderFallo(w, r, err)
		return
	}

	mensaje, err := h.store.GuardarMensaje(r.Context(), usuarioID, conversacionID, MensajeNuevo{
		Rol:           RolAgente,
		Contenido:     respuesta.Contenido,
		TokensEntrada: respuesta.TokensEntrada,
		TokensSalida:  respuesta.TokensSalida,
		Herramientas:  respuesta.Herramientas,
	})
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: guardando la respuesta")
		return
	}

	// Las propuestas se guardan DESPUES del mensaje: si algo falla al
	// guardarlas, en la base del dinero no ha pasado nada igual — una
	// propuesta no es un movimiento.
	propuestas := []Propuesta{}
	for _, nueva := range respuesta.Propuestas {
		guardada, err := h.store.GuardarPropuesta(r.Context(), usuarioID, conversacionID, nueva)
		if err != nil {
			httpx.ErrorInterno(w, r, err, "agente: guardando la propuesta")
			return
		}
		propuestas = append(propuestas, *guardada)
	}

	httpx.JSON(w, http.StatusOK, envioResponse{
		ConversacionID: conversacionID,
		Mensaje:        *mensaje,
		Propuestas:     propuestas,
		// usados ya no cuenta el que se acaba de guardar, por eso el +1.
		Restantes: max(h.limiteDiario-(usados+1), 0),
	})
}

// respuesta es lo que sale del ciclo completo: el texto final, lo que costo en
// tokens (sumando TODAS las rondas) y que herramientas se consultaron.
type respuestaFinal struct {
	Contenido     string
	Herramientas  []string
	TokensEntrada int
	TokensSalida  int

	// Propuestas son las escrituras que el agente dejo preparadas en este
	// turno. Todavia no ha pasado nada en la base del dinero: son tarjetas
	// esperando un clic del usuario.
	Propuestas []PropuestaNueva
}

// responder es el ciclo del agente: se le pregunta al modelo, y mientras pida
// datos se ejecutan sus herramientas y se le devuelven, hasta que redacte la
// respuesta.
//
// El corte por MaxRondas no es paranoia: cada vuelta es otra llamada al
// proveedor —otros segundos y otra fraccion de centavo—, y un modelo que se
// queda pidiendo datos en bucle costaria hasta que el router corte la
// peticion a los 30s.
func (h *Handler) responder(ctx context.Context, usuarioID int64, nombre string, historial []Mensaje) (respuestaFinal, error) {
	var final respuestaFinal

	sistema := instrucciones(nombre, time.Now())
	esquemas := h.catalogo.Esquemas()
	pasos := pasosDe(historial)

	for range MaxRondas {
		respuesta, err := h.proveedor.Completar(ctx, sistema, pasos, esquemas)
		if err != nil {
			return final, err
		}

		// Los tokens se acumulan: lo que se guarda en la base es el costo
		// completo de la pregunta, no el de la ultima vuelta.
		final.TokensEntrada += respuesta.TokensEntrada
		final.TokensSalida += respuesta.TokensSalida

		// Sin llamadas, el modelo ya redacto: termino el ciclo.
		if len(respuesta.Llamadas) == 0 {
			final.Contenido = respuesta.Contenido
			return final, nil
		}

		pasos = append(pasos, Paso{
			Rol:       RolAgente,
			Contenido: respuesta.Contenido,
			Llamadas:  respuesta.Llamadas,
		})

		for _, llamada := range respuesta.Llamadas {
			// AQUI esta la regla del paquete hecha codigo: el usuarioID lo
			// pone el servidor, y sale del JWT. Lo que el modelo escribio en
			// los argumentos no tiene voz sobre de quien son los datos.
			salida, err := h.catalogo.Ejecutar(ctx, usuarioID, llamada)
			if err != nil {
				// Solo llega aqui si fallo la base: los errores del modelo
				// (argumentos raros, herramienta inventada) vuelven como
				// resultado para que se corrija solo.
				return final, err
			}

			if salida.Propuesta != nil {
				final.Propuestas = append(final.Propuestas, *salida.Propuesta)
			}

			final.Herramientas = agregarUnaVez(final.Herramientas, llamada.Nombre)
			pasos = append(pasos, Paso{
				Rol:       RolHerramienta,
				LlamadaID: llamada.ID,
				Nombre:    llamada.Nombre,
				Contenido: salida.Texto,
			})
		}
	}

	return final, ErrDemasiadasRondas
}

// pasosDe convierte el hilo guardado en la conversacion que ve el modelo.
//
// Del ir y venir con las herramientas no queda nada en la base: al hilo llega
// el texto final. En el siguiente turno el modelo no recuerda lo que
// consulto, pero puede volver a consultarlo, y a cambio el historial no se
// llena de JSON que se paga en cada llamada.
func pasosDe(historial []Mensaje) []Paso {
	pasos := make([]Paso, 0, len(historial))
	for _, m := range historial {
		pasos = append(pasos, Paso{Rol: m.Rol, Contenido: m.Contenido})
	}
	return pasos
}

// agregarUnaVez mantiene la lista de herramientas sin repetidos y en el orden
// en que se usaron: es lo que la app le muestra al usuario.
func agregarUnaVez(lista []string, nombre string) []string {
	if slices.Contains(lista, nombre) {
		return lista
	}
	return append(lista, nombre)
}

// responderFallo traduce el error del proveedor a algo que el usuario pueda
// leer, y lo deja en la bitacora para el dueno del servidor.
//
// La distincion importa: que el modelo se caiga no es un error de la app (503,
// "intenta de nuevo"), pero una llave vencida si es algo que hay que arreglar
// y por eso baja al caso general de 500.
func (h *Handler) responderFallo(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrProveedorNoDisponible):
		httpx.RegistrarFallo(r, err, "agente: el proveedor no respondio")
		httpx.Error(w, http.StatusServiceUnavailable,
			"El asistente no está disponible en este momento. Intenta de nuevo en un minuto.")
	case errors.Is(err, ErrRespuestaVacia):
		httpx.RegistrarFallo(r, err, "agente: respuesta vacia del modelo")
		httpx.Error(w, http.StatusServiceUnavailable,
			"El asistente se quedó sin respuesta. Intenta preguntarlo de otra forma.")
	case errors.Is(err, ErrDemasiadasRondas):
		httpx.RegistrarFallo(r, err, "agente: se agotaron las rondas de herramientas")
		httpx.Error(w, http.StatusServiceUnavailable,
			"El asistente se enredó consultando tus datos. Intenta preguntarlo de otra forma.")
	default:
		httpx.ErrorInterno(w, r, err, "agente: llamando al modelo")
	}
}

// conversacionAbierta devuelve la conversacion activa, o crea una si es la
// primera vez.
func (h *Handler) conversacionAbierta(r *http.Request, usuarioID int64) (int64, error) {
	conv, err := h.store.Activa(r.Context(), usuarioID)
	if err == nil {
		return conv.ID, nil
	}
	if !errors.Is(err, ErrNoEncontrada) {
		return 0, err
	}
	return h.store.Crear(r.Context(), usuarioID)
}

func (h *Handler) restantes(r *http.Request, usuarioID int64) (int, error) {
	usados, err := h.store.MensajesRecientes(r.Context(), usuarioID)
	if err != nil {
		return 0, err
	}
	return max(h.limiteDiario-usados, 0), nil
}

// usuarioPropio devuelve el id del dueno del chat, o corta la peticion.
//
// Aqui esta la segunda mitad de la regla del paquete: el chat es SIEMPRE el de
// quien tiene la sesion. Un admin en modo "ver como" puede leer los
// movimientos de un cliente — eso es parte de su trabajo —, pero no su
// conversacion con el asistente: no es un registro de dinero, es lo que esa
// persona escribio creyendo que era privado. Sin este corte, el middleware
// VerComo cambiaria el id del context y el GET de arriba devolveria el hilo
// ajeno sin que ningun handler se entere.
func (h *Handler) usuarioPropio(w http.ResponseWriter, r *http.Request) (int64, bool) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return 0, false
	}

	if _, observando := httpx.Observador(r.Context()); observando {
		httpx.Error(w, http.StatusForbidden,
			"El chat con el asistente es privado: no se puede ver desde otra cuenta.")
		return 0, false
	}

	// El plan manda. Se revisa en CADA peticion y no solo al iniciar sesion:
	// si el dueño le quita la IA al plan, el corte es inmediato y no espera
	// a que el token venza. Es lo que protege el costo, no el menu de la app.
	permitido := false
	if h.permiso != nil {
		var err error
		permitido, err = h.permiso(r.Context(), usuarioID)
		if err != nil {
			httpx.ErrorInterno(w, r, err, "agente: revisando el plan")
			return 0, false
		}
	}
	if !permitido {
		httpx.Error(w, http.StatusForbidden,
			"Tu plan no incluye el asistente. Habla con el administrador para cambiarte a uno que lo tenga.")
		return 0, false
	}

	return usuarioID, true
}

// leerTexto decodifica y valida el body. Devuelve false si ya respondio error.
func leerTexto(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req mensajeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return "", false
	}

	texto := strings.TrimSpace(req.Texto)

	v := httpx.NuevoValidador()
	v.Requerido("texto", texto)
	if texto != "" {
		v.MaxLargo("texto", texto, MaxCaracteresMensaje)
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return "", false
	}

	return texto, true
}
