package agente

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"finanzas/internal/categorias"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
	"finanzas/internal/recurrentes"
)

// Catalogo es lo que el agente puede consultar. Cada herramienta envuelve un
// Store que ya existe: aqui no se escribe una sola linea de SQL nueva.
//
// LA REGLA, otra vez, porque este es el archivo donde importa: el usuarioID
// llega como PARAMETRO de Ejecutar, sacado del JWT, y no existe en el esquema
// de ninguna herramienta. El modelo no puede pedir "los movimientos de otro"
// porque no hay forma de decirlo: ese campo no esta en el formulario que se le
// ofrece, y si lo inventa, se descarta al decodificar los argumentos.
//
// Las de lectura consultan y ya. Las dos que empiezan por "proponer" NO
// escriben: dejan preparada una propuesta que el usuario confirma desde una
// tarjeta, y la escritura de verdad ocurre en otra peticion, con un clic suyo
// de por medio.
type Catalogo struct {
	movimientos *movimientos.Store
	categorias  *categorias.Store
	medios      *medios.Store

	// Los recurrentes son opcionales: si no llegan, el agente sencillamente
	// no ofrece esas dos herramientas y sigue funcionando igual. El
	// confirmador es el MISMO que usa el botón del resumen — el chat no tiene
	// un camino propio para dar por pagado un recurrente.
	recurrentes  *recurrentes.Store
	confirmarRec *recurrentes.Confirmador
}

func NuevoCatalogo(m *movimientos.Store, c *categorias.Store, me *medios.Store) *Catalogo {
	return &Catalogo{movimientos: m, categorias: c, medios: me}
}

// ConRecurrentes le enseña al agente los gastos que se repiten.
//
// Va aparte del constructor a propósito: es lo único del catálogo que puede no
// estar, y meterlo en NuevoCatalogo obligaría a pasar dos nils en cada prueba
// que no los necesita.
func (c *Catalogo) ConRecurrentes(store *recurrentes.Store, confirmador *recurrentes.Confirmador) *Catalogo {
	c.recurrentes = store
	c.confirmarRec = confirmador
	return c
}

// Los nombres son los que ve el modelo; tambien son los que la app le muestra
// al usuario debajo de la respuesta.
const (
	HerramientaResumen      = "resumen"
	HerramientaMovimientos  = "listar_movimientos"
	HerramientaCategorias   = "listar_categorias"
	HerramientaMedios       = "listar_medios_pago"
	HerramientaContrapartes = "listar_contrapartes"

	// Las dos que preparan una escritura. Ojo con el nombre: "proponer", no
	// "crear". No escriben nada — dejan una tarjeta para que el usuario
	// confirme —, y el nombre es lo primero que lee el modelo.
	HerramientaProponerMovimiento = "proponer_movimiento"
	HerramientaProponerPagado     = "proponer_marcar_pagado"
	HerramientaProponerAbono      = "proponer_abono"
)

const (
	// Cuantos movimientos se le devuelven al modelo. El techo no es capricho:
	// cada fila son tokens que se pagan en esta llamada y en las siguientes.
	// Si la pregunta necesita mas, la respuesta correcta es un total, no una
	// lista de trescientas lineas.
	limiteMovimientosPorDefecto = 20
	limiteMovimientosMaximo     = 50
)

// Esquemas es lo que se le ofrece al modelo en cada llamada.
//
// Las descripciones son parte del codigo, no adorno: son literalmente lo unico
// que el modelo lee para decidir cual usar y con que argumentos. Una
// descripcion floja se paga en llamadas equivocadas.
func (c *Catalogo) Esquemas() []Herramienta {
	lista := []Herramienta{
		{
			Nombre: HerramientaResumen,
			Descripcion: "Devuelve el resumen financiero del usuario: totales (recibido, pagado, por cobrar, por pagar, balance), " +
				"el saldo que tiene en cada medio de pago, el desglose por categoría, quién le debe plata y a quién le debe él. " +
				"Úsala para cualquier pregunta sobre cuánto tiene, cuánto lleva gastado, quién le debe o cuánto debe.",
			Parametros: objeto(nil),
		},
		{
			Nombre: HerramientaMovimientos,
			Descripcion: "Lista los movimientos del usuario, del más reciente al más viejo, con filtros opcionales. " +
				"Úsala cuando pregunten por movimientos concretos ('¿qué compré ayer?', '¿cuánto le presté a Juan?'). " +
				"Para totales usa mejor la herramienta resumen: ahí las sumas ya vienen hechas.",
			Parametros: objeto(map[string]any{
				"tipo": map[string]any{
					"type":        "string",
					"enum":        tiposDeMovimiento,
					"description": descripcionDeTipos,
				},
				"estado": map[string]any{
					"type": "string",
					"enum": []string{movimientos.EstadoPendiente, movimientos.EstadoParcial, movimientos.EstadoPagado},
					"description": "Solo aplica a las deudas (preste y me_prestaron): pendiente = no ha abonado nada, " +
						"parcial = abonó una parte, pagado = ya está saldada",
				},
				"categoria": map[string]any{
					"type":        "string",
					"description": "Nombre exacto de la categoría, tal como aparece en listar_categorias",
				},
				"medio_pago": map[string]any{
					"type":        "string",
					"description": "Nombre exacto del medio de pago, tal como aparece en listar_medios_pago",
				},
				"desde": map[string]any{
					"type":        "string",
					"description": "Fecha inicial inclusive, en formato AAAA-MM-DD",
				},
				"hasta": map[string]any{
					"type":        "string",
					"description": "Fecha final inclusive, en formato AAAA-MM-DD",
				},
				"texto": map[string]any{
					"type":        "string",
					"description": "Busca estas palabras en la descripción o en el nombre de la persona",
				},
				"limite": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Cuántos movimientos devolver (por omisión %d, máximo %d)", limiteMovimientosPorDefecto, limiteMovimientosMaximo),
				},
			}),
		},
		{
			Nombre: HerramientaCategorias,
			Descripcion: "Lista las categorías del usuario (de qué es la plata: Negocio, Personal...) con cuántos movimientos tiene cada una. " +
				"Llámala SIEMPRE antes de proponer un movimiento cuando el usuario no dijo la categoría: de esta lista sale la que vas a usar. " +
				"Las categorías son de él, no tuyas; las que te suenan obvias muchas veces no existen en su cuenta.",
			Parametros: objeto(nil),
		},
		{
			Nombre: HerramientaMedios,
			Descripcion: "Lista los medios de pago del usuario (por dónde entra o sale la plata: Efectivo, Transferencia, Nequi...). " +
				"Llámala SIEMPRE antes de proponer un movimiento cuando el usuario no dijo por dónde se movió la plata: " +
				"de esta lista sale el medio que vas a usar, nunca de tu memoria.",
			Parametros: objeto(nil),
		},
		{
			Nombre: HerramientaProponerMovimiento,
			Descripcion: "Prepara un movimiento para que el usuario lo confirme. NO lo registra: le aparece una tarjeta " +
				"con los datos, que puede corregir antes de guardar. Úsala cuando te cuente un gasto, un ingreso, un préstamo " +
				"en cualquiera de los dos sentidos, o un traslado entre sus medios de pago " +
				"('pagué 45 mil de almuerzo con Nequi', 'el negocio me prestó 500 mil', 'pasé 200 mil del efectivo al banco'). " +
				"Si el usuario no dijo la categoría o el medio de pago, no los adivines: llama primero a listar_categorias y " +
				"listar_medios_pago, elige de esas listas la que mejor encaje con lo que te contó, y al responder dile cuál " +
				"elegiste tú para que la corrija en la tarjeta si no es. Pregúntale solo si ninguna encaja o si dos encajan igual.",
			Parametros: objeto(map[string]any{
				"tipo": map[string]any{
					"type":        "string",
					"enum":        tiposDeMovimiento,
					"description": descripcionDeTipos,
				},
				"monto": map[string]any{
					"type":        "string",
					"description": "Solo el número, sin puntos ni signos: 45000 o 45000.50",
				},
				"fecha": map[string]any{
					"type":        "string",
					"description": "AAAA-MM-DD. Si el usuario no dijo cuándo, usa hoy",
				},
				"descripcion": map[string]any{
					"type":        "string",
					"description": "De qué fue, en pocas palabras y con las del usuario",
				},
				"categoria": map[string]any{
					"type": "string",
					"description": "Obligatorio. Nombre exacto, copiado de listar_categorias. Si el usuario no la dijo, " +
						"elige de esa lista la que mejor encaje; no escribas un nombre que no esté ahí",
				},
				"medio_pago": map[string]any{
					"type": "string",
					"description": "Obligatorio. Por dónde entró o salió la plata: nombre exacto, copiado de listar_medios_pago. " +
						"Si el usuario no lo dijo, elige de esa lista el que mejor encaje; no escribas uno que no esté ahí",
				},
				"medio_destino": map[string]any{
					"type": "string",
					"description": "SOLO para tipo traslado: el medio de pago al que ENTRA la plata. " +
						"En un traslado, medio_pago es de dónde sale y medio_destino a dónde entra. " +
						"Tienen que ser distintos",
				},
				"a_quien": map[string]any{
					"type": "string",
					"description": "Solo para preste y me_prestaron: con quién es la deuda (persona o negocio). " +
						"Antes de inventar un nombre nuevo, mira listar_contrapartes: si ya existe uno parecido, " +
						"usa ese mismo, escrito igual",
				},
				"estado": map[string]any{
					"type": "string",
					"enum": []string{movimientos.EstadoPendiente, movimientos.EstadoPagado},
					"description": "Solo para preste y me_prestaron: en qué va la deuda. " +
						"Si no te dijo que ya se saldó, no lo mandes: se toma como pendiente, que es lo normal en una deuda nueva",
				},
				"cobrar_el": map[string]any{
					"type": "string",
					"description": "Solo para preste y me_prestaron, opcional: AAAA-MM-DD del día acordado " +
						"('me paga el viernes', 'le pago el 30'). Ese día la app le avisa. Si no lo dijo, no lo pongas",
				},
			}),
		},
		{
			Nombre: HerramientaProponerPagado,
			Descripcion: "Prepara el SALDO COMPLETO de una deuda para que el usuario lo confirme " +
				"('ya me pagó Juan', 'ya le pagué todo al negocio'). NO lo marca: le aparece una tarjeta para confirmarlo. " +
				"Si solo le abonaron una parte, usa proponer_abono en vez de esta. " +
				"Antes busca la deuda con listar_movimientos (tipo=preste o me_prestaron) para saber su id.",
			Parametros: objeto(map[string]any{
				"movimiento_id": map[string]any{
					"type":        "integer",
					"description": "Id de la deuda, tal como salió en listar_movimientos",
				},
				"medio_cobro": map[string]any{
					"type":        "string",
					"description": "Nombre del medio por donde se movió la plata al saldar. Si no lo dijo, déjalo vacío y que lo elija él",
				},
			}),
		},
		{
			Nombre: HerramientaProponerAbono,
			Descripcion: "Prepara un ABONO PARCIAL a una deuda para que el usuario lo confirme " +
				"('Carlos me abonó 50 mil', 'le pagué 100 mil de lo que le debo'). NO lo registra: le aparece una tarjeta. " +
				"Úsala cuando el monto sea MENOR que la deuda; si le pagaron todo, usa proponer_marcar_pagado. " +
				"Antes busca la deuda con listar_movimientos para saber su id y cuánto falta.",
			Parametros: objeto(map[string]any{
				"movimiento_id": map[string]any{
					"type":        "integer",
					"description": "Id de la deuda, tal como salió en listar_movimientos",
				},
				"monto": map[string]any{
					"type":        "string",
					"description": "Solo el número, sin puntos ni signos: 50000",
				},
				"fecha": map[string]any{
					"type":        "string",
					"description": "AAAA-MM-DD. Si no dijo cuándo, usa hoy",
				},
				"medio": map[string]any{
					"type":        "string",
					"description": "Nombre del medio por donde se movió el abono. Si no lo dijo, déjalo vacío y que lo elija él",
				},
				"nota": map[string]any{
					"type":        "string",
					"description": "Opcional, en pocas palabras: para qué o por qué fue este abono",
				},
			}),
		},
		{
			Nombre: HerramientaContrapartes,
			Descripcion: "Lista las personas y negocios con los que el usuario tiene cuentas pendientes, " +
				"con cuánto le deben, cuánto les debe y el neto. Úsala ANTES de proponer un préstamo para " +
				"escribir el nombre igual que como ya está guardado, y para responder '¿cuánto me debe X?'. " +
				"Si un nombre además es una categoría del usuario, viene marcado con es_categoria: díselo, " +
				"porque son dos cosas distintas que se llaman igual.",
			Parametros: objeto(nil),
		},
	}

	// Las de los recurrentes solo se ofrecen si el servidor los tiene: una
	// herramienta que no puede funcionar es una invitación a que el modelo la
	// llame y reciba un error.
	if c.recurrentes != nil {
		lista = append(lista, esquemasDeRecurrentes()...)
	}
	return lista
}

// Los tipos que el modelo puede usar, y como se los explicamos.
//
// En variables y no escritos dos veces: el enum aparece en listar_movimientos y
// en proponer_movimiento, y el dia que se agregue un tipo tiene que aparecer en
// los dos. Con el texto duplicado, lo normal es que se actualice uno solo y el
// modelo termine sin poder registrar la mitad de las cosas.
var tiposDeMovimiento = []string{
	movimientos.TipoRecibi,
	movimientos.TipoPague,
	movimientos.TipoPreste,
	movimientos.TipoMePrestaron,
	movimientos.TipoTraslado,
}

const descripcionDeTipos = "recibi = entró plata; " +
	"pague = salió plata; " +
	"preste = se la llevó alguien y TE la debe; " +
	"me_prestaron = te la dieron y TÚ la debes; " +
	"traslado = pasó plata de un medio de pago suyo a otro (no es ingreso ni gasto)"

// Resultado es lo que deja una herramienta: el JSON que vuelve al modelo y,
// cuando preparo una escritura, la propuesta que el usuario tendra que
// confirmar. El catalogo NO la guarda: eso es del handler, que es quien sabe
// en que conversacion va.
type Resultado struct {
	Texto     string
	Propuesta *PropuestaNueva
}

// Ejecutar corre una herramienta y devuelve el JSON que vuelve al modelo.
//
// Dos clases de problema, tratadas distinto:
//
//   - El modelo se equivoco (herramienta que no existe, argumentos con basura,
//     una categoria inventada): se le devuelve el error COMO RESULTADO, en
//     JSON, y en la siguiente ronda se corrige solo. No es un fallo del
//     servidor y no tiene por que tumbar la respuesta.
//   - La base de datos fallo: eso si es un error de verdad y sube como error
//     de Go para que quede en la bitacora y el usuario vea un 500.
func (c *Catalogo) Ejecutar(ctx context.Context, usuarioID int64, llamada Llamada) (Resultado, error) {
	var (
		texto string
		err   error
	)

	switch llamada.Nombre {
	case HerramientaResumen:
		texto, err = c.resumen(ctx, usuarioID)
	case HerramientaMovimientos:
		texto, err = c.listarMovimientos(ctx, usuarioID, llamada.Argumentos)
	case HerramientaCategorias:
		texto, err = c.listarCategorias(ctx, usuarioID)
	case HerramientaMedios:
		texto, err = c.listarMedios(ctx, usuarioID)
	case HerramientaContrapartes:
		texto, err = c.listarContrapartes(ctx, usuarioID)

	case HerramientaProponerMovimiento:
		return c.proponerMovimiento(ctx, usuarioID, llamada.Argumentos)
	case HerramientaProponerPagado:
		return c.proponerMarcarPagado(ctx, usuarioID, llamada.Argumentos)
	case HerramientaProponerAbono:
		return c.proponerAbono(ctx, usuarioID, llamada.Argumentos)

	case HerramientaRecurrentesPendientes:
		texto, err = c.listarRecurrentesPendientes(ctx, usuarioID)
	case HerramientaProponerRecurrente:
		return c.proponerRecurrente(ctx, usuarioID, llamada.Argumentos)

	default:
		texto = errorParaElModelo("no existe una herramienta llamada %q", llamada.Nombre)
	}

	return Resultado{Texto: texto}, err
}

// --------------------------------------------------------------------------
// Las herramientas
// --------------------------------------------------------------------------

// Lo que se le manda al modelo es una version RECORTADA de cada dato: sin ids,
// sin marcas de tiempo, sin rutas de facturas. Nada de eso le sirve para
// responder, y todo eso son tokens que se pagan.

type resumenParaModelo struct {
	Totales      movimientos.Totales     `json:"totales"`
	Medios       []medioParaModelo       `json:"medios"`
	Categorias   []rubroParaModelo       `json:"categorias"`
	Contrapartes []contraparteParaModelo `json:"contrapartes"`
	Nota         string                  `json:"nota"`
}

type medioParaModelo struct {
	Nombre string `json:"nombre"`
	Saldo  string `json:"saldo"`
}

type rubroParaModelo struct {
	Nombre    string `json:"nombre"`
	Recibido  string `json:"recibido"`
	Pagado    string `json:"pagado"`
	PorCobrar string `json:"por_cobrar"`
	PorPagar  string `json:"por_pagar"`
	Balance   string `json:"balance"`
}

// contraparteParaModelo es con quien hay cuentas pendientes, en los dos
// sentidos. Los nombres de los campos estan escritos para que el modelo no se
// confunda de lado: es el error mas caro que puede cometer aqui.
type contraparteParaModelo struct {
	Nombre string `json:"nombre"`
	// TeDebe: lo que esta persona le debe AL USUARIO.
	TeDebe string `json:"te_debe"`
	// LeDebes: lo que EL USUARIO le debe a esta persona.
	LeDebes string `json:"le_debes"`
	// Neto positivo = a favor del usuario; negativo = en contra.
	Neto string `json:"neto"`
	// EsCategoria: este nombre tambien es una categoria del usuario.
	EsCategoria bool `json:"es_categoria,omitempty"`
	// ProximaFecha es el dia acordado mas cercano de sus deudas vivas.
	ProximaFecha string `json:"proxima_fecha,omitempty"`
}

func (c *Catalogo) resumen(ctx context.Context, usuarioID int64) (string, error) {
	resumen, err := c.movimientos.Resumen(ctx, usuarioID)
	if err != nil {
		return "", fmt.Errorf("herramienta resumen: %w", err)
	}

	// Los slices arrancan vacios y no nil: un "deudores": null le da al modelo
	// una cosa mas que interpretar, y "[]" ya dice lo que hay que decir.
	salida := resumenParaModelo{
		Totales:      resumen.Totales,
		Medios:       []medioParaModelo{},
		Categorias:   []rubroParaModelo{},
		Contrapartes: []contraparteParaModelo{},
		Nota: "Todas las cifras ya vienen sumadas por la base de datos. " +
			"Cópialas tal cual: no sumes, no restes, no conviertas. " +
			"por_cobrar es lo que le deben al usuario y por_pagar lo que él debe; " +
			"las dos son saldos, ya descontados los abonos.",
	}

	for _, m := range resumen.Medios {
		salida.Medios = append(salida.Medios, medioParaModelo{Nombre: m.Nombre, Saldo: m.Saldo})
	}
	for _, cat := range resumen.Categorias {
		salida.Categorias = append(salida.Categorias, rubroParaModelo{
			Nombre:    cat.Nombre,
			Recibido:  cat.Recibido,
			Pagado:    cat.Pagado,
			PorCobrar: cat.PorCobrar,
			PorPagar:  cat.PorPagar,
			Balance:   cat.Balance,
		})
	}
	salida.Contrapartes = contrapartesParaModelo(resumen.Contrapartes)

	return aJSON(salida)
}

type argumentosMovimientos struct {
	Tipo      string `json:"tipo"`
	Estado    string `json:"estado"`
	Categoria string `json:"categoria"`
	MedioPago string `json:"medio_pago"`
	Desde     string `json:"desde"`
	Hasta     string `json:"hasta"`
	Texto     string `json:"texto"`
	Limite    int    `json:"limite"`
}

type movimientoParaModelo struct {
	// El id va porque proponer_marcar_pagado lo necesita para decir CUAL
	// prestamo se cobro. Es seguro: al ejecutarla, la consulta sigue
	// filtrando por el usuario del JWT, asi que un id de otra persona no
	// devuelve nada.
	ID          int64  `json:"id"`
	Fecha       string `json:"fecha"`
	Tipo        string `json:"tipo"`
	Monto       string `json:"monto"`
	Descripcion string `json:"descripcion"`
	Categoria   string `json:"categoria"`
	MedioPago   string `json:"medio_pago,omitempty"`
	// MedioDestino solo aparece en los traslados: a donde entro la plata.
	MedioDestino string `json:"medio_destino,omitempty"`
	AQuien       string `json:"a_quien,omitempty"`
	Estado       string `json:"estado,omitempty"`
	// Saldo es lo que FALTA de una deuda (monto menos abonos), y Abonado lo
	// que ya se movio. Sin estos dos el modelo diria "te deben 500.000" de un
	// prestamo del que ya devolvieron 400.
	Saldo   string `json:"saldo,omitempty"`
	Abonado string `json:"abonado,omitempty"`
	Cuotas  int    `json:"cuotas_del_acuerdo,omitempty"`
}

type listaParaModelo struct {
	Total       int                    `json:"total_que_cumple_los_filtros"`
	Movimientos []movimientoParaModelo `json:"movimientos"`
	Nota        string                 `json:"nota,omitempty"`
}

func (c *Catalogo) listarMovimientos(ctx context.Context, usuarioID int64, crudos json.RawMessage) (string, error) {
	var args argumentosMovimientos
	if err := json.Unmarshal(crudos, &args); err != nil {
		return errorParaElModelo("no entendí los argumentos: %v", err), nil
	}

	filtros := movimientos.Filtros{
		Tipo:   strings.TrimSpace(args.Tipo),
		Estado: strings.TrimSpace(args.Estado),
		Texto:  strings.TrimSpace(args.Texto),
		Limite: limiteMovimientosPorDefecto,
	}

	if filtros.Tipo != "" && !movimientos.EsTipoValido(filtros.Tipo) {
		return errorParaElModelo("tipo inválido: %q. Los válidos son recibi, pague y preste", args.Tipo), nil
	}
	if filtros.Estado != "" && !movimientos.EsEstadoValido(filtros.Estado) {
		return errorParaElModelo("estado inválido: %q. Los válidos son pendiente y pagado", args.Estado), nil
	}

	var err error
	if filtros.Desde, err = fechaValida(args.Desde); err != nil {
		return errorParaElModelo("la fecha 'desde' %v", err), nil
	}
	if filtros.Hasta, err = fechaValida(args.Hasta); err != nil {
		return errorParaElModelo("la fecha 'hasta' %v", err), nil
	}

	if args.Limite > 0 {
		filtros.Limite = min(args.Limite, limiteMovimientosMaximo)
	}

	// Los nombres se resuelven a ids AQUI, contra las categorias del usuario.
	// Asi el modelo nunca maneja ids —que es donde se cuelan los errores y las
	// invenciones— y una categoria que no existe se responde con la lista de
	// las que si, para que se corrija en la siguiente ronda.
	if nombre := strings.TrimSpace(args.Categoria); nombre != "" {
		lista, err := c.categorias.Listar(ctx, usuarioID)
		if err != nil {
			return "", fmt.Errorf("herramienta listar_movimientos (categorias): %w", err)
		}
		id, nombres := buscarPorNombre(nombre, len(lista), func(i int) (int64, string) {
			return lista[i].ID, lista[i].Nombre
		})
		if id == 0 {
			return errorParaElModelo("no existe la categoría %q. Las del usuario son: %s", nombre, strings.Join(nombres, ", ")), nil
		}
		filtros.CategoriaID = id
	}

	if nombre := strings.TrimSpace(args.MedioPago); nombre != "" {
		lista, err := c.medios.Listar(ctx, usuarioID)
		if err != nil {
			return "", fmt.Errorf("herramienta listar_movimientos (medios): %w", err)
		}
		id, nombres := buscarPorNombre(nombre, len(lista), func(i int) (int64, string) {
			return lista[i].ID, lista[i].Nombre
		})
		if id == 0 {
			return errorParaElModelo("no existe el medio de pago %q. Los del usuario son: %s", nombre, strings.Join(nombres, ", ")), nil
		}
		filtros.MedioPagoID = id
	}

	lista, total, err := c.movimientos.Listar(ctx, usuarioID, filtros)
	if err != nil {
		return "", fmt.Errorf("herramienta listar_movimientos: %w", err)
	}

	salida := listaParaModelo{Total: total, Movimientos: []movimientoParaModelo{}}
	for _, m := range lista {
		fila := movimientoParaModelo{
			ID:          m.ID,
			Fecha:       m.Fecha,
			Tipo:        m.Tipo,
			Monto:       m.Monto,
			Descripcion: m.Descripcion,
			Categoria:   m.CategoriaNombre,
			MedioPago:   valor(m.MedioPagoNombre),
			AQuien:      valor(m.AQuien),
			Estado:      valor(m.Estado),
		}
		if m.Tipo == movimientos.TipoTraslado {
			fila.MedioDestino = valor(m.MedioCobroNombre)
		}
		if movimientos.EsDeuda(m.Tipo) {
			fila.Saldo = m.Saldo
			fila.Abonado = m.Abonado
			fila.Cuotas = m.Cuotas
		}
		salida.Movimientos = append(salida.Movimientos, fila)
	}

	if total > len(lista) {
		salida.Nota = fmt.Sprintf(
			"Hay %d movimientos que cumplen los filtros y aquí van los %d más recientes. "+
				"Si necesitas un total, pídelo con la herramienta resumen o afina los filtros: no sumes tú.",
			total, len(lista))
	}

	return aJSON(salida)
}

type nombrado struct {
	Nombre      string `json:"nombre"`
	Movimientos int    `json:"movimientos"`
}

func (c *Catalogo) listarCategorias(ctx context.Context, usuarioID int64) (string, error) {
	lista, err := c.categorias.Listar(ctx, usuarioID)
	if err != nil {
		return "", fmt.Errorf("herramienta listar_categorias: %w", err)
	}

	salida := []nombrado{}
	for _, c := range lista {
		salida = append(salida, nombrado{Nombre: c.Nombre, Movimientos: c.Movimientos})
	}
	return aJSON(salida)
}

func (c *Catalogo) listarMedios(ctx context.Context, usuarioID int64) (string, error) {
	lista, err := c.medios.Listar(ctx, usuarioID)
	if err != nil {
		return "", fmt.Errorf("herramienta listar_medios_pago: %w", err)
	}

	salida := []nombrado{}
	for _, m := range lista {
		salida = append(salida, nombrado{Nombre: m.Nombre, Movimientos: m.Movimientos})
	}
	return aJSON(salida)
}

// --------------------------------------------------------------------------
// Ayudas
// --------------------------------------------------------------------------

// objeto arma el JSON Schema de los argumentos. Sin propiedades queda un
// objeto vacio, que es como se declara una herramienta sin argumentos.
func objeto(propiedades map[string]any) map[string]any {
	if propiedades == nil {
		propiedades = map[string]any{}
	}
	return map[string]any{
		"type":       "object",
		"properties": propiedades,
	}
}

// buscarPorNombre compara sin distinguir mayusculas ni tildes de mas: el
// modelo escribe "efectivo" y en la app dice "Efectivo". Devuelve 0 si no
// hubo coincidencia, junto con los nombres que si existen.
func buscarPorNombre(buscado string, n int, dato func(int) (int64, string)) (int64, []string) {
	nombres := make([]string, 0, n)
	var encontrado int64

	for i := range n {
		id, nombre := dato(i)
		nombres = append(nombres, nombre)
		if encontrado == 0 && strings.EqualFold(strings.TrimSpace(nombre), buscado) {
			encontrado = id
		}
	}
	return encontrado, nombres
}

func fechaValida(valor string) (string, error) {
	valor = strings.TrimSpace(valor)
	if valor == "" {
		return "", nil
	}
	if _, err := time.Parse(movimientos.FormatoFecha, valor); err != nil {
		return "", fmt.Errorf("tiene que ir en formato AAAA-MM-DD (llegó %q)", valor)
	}
	return valor, nil
}

func valor(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func aJSON(v any) (string, error) {
	datos, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("armando el resultado de la herramienta: %w", err)
	}
	return string(datos), nil
}

// errorParaElModelo es un error que el modelo puede leer y corregir. No es un
// fallo del servidor: es el equivalente a que la herramienta conteste "así no".
func errorParaElModelo(formato string, args ...any) string {
	datos, err := json.Marshal(map[string]string{"error": fmt.Sprintf(formato, args...)})
	if err != nil {
		return `{"error":"no se pudo ejecutar la herramienta"}`
	}
	return string(datos)
}

// --------------------------------------------------------------------------
// Las herramientas que PREPARAN una escritura
// --------------------------------------------------------------------------
//
// Ninguna de estas dos toca la base del dinero. Arman una propuesta, la app la
// pinta en una tarjeta con los campos editables y la escritura ocurre cuando
// el usuario le da a guardar — por el endpoint de confirmar, que valida otra
// vez y usa el mismo Store que el formulario.
//
// Que el modelo no escriba directo no es desconfianza abstracta: oye "cuarenta
// y cinco mil" y casi siempre escribe 45000, pero el dia que escriba 450000 el
// usuario tiene que poder verlo ANTES, no descubrirlo cuadrando el mes.

type argumentosMovimientoNuevo struct {
	Tipo         string `json:"tipo"`
	Monto        string `json:"monto"`
	Fecha        string `json:"fecha"`
	Descripcion  string `json:"descripcion"`
	Categoria    string `json:"categoria"`
	MedioPago    string `json:"medio_pago"`
	MedioDestino string `json:"medio_destino"`
	AQuien       string `json:"a_quien"`
	Estado       string `json:"estado"`
	CobrarEl     string `json:"cobrar_el"`
}

// datosMovimiento es lo que pinta la tarjeta: los ids para que los selectores
// lleguen elegidos, y los nombres para que se lea sin consultar nada.
type datosMovimiento struct {
	Tipo        string `json:"tipo"`
	Monto       string `json:"monto"`
	Fecha       string `json:"fecha"`
	Descripcion string `json:"descripcion"`
	CategoriaID int64  `json:"categoria_id"`
	Categoria   string `json:"categoria"`
	MedioPagoID int64  `json:"medio_pago_id"`
	MedioPago   string `json:"medio_pago"`
	// Solo en los traslados: a donde entra la plata.
	MedioDestinoID int64  `json:"medio_destino_id,omitempty"`
	MedioDestino   string `json:"medio_destino,omitempty"`
	AQuien         string `json:"a_quien,omitempty"`
	Estado         string `json:"estado,omitempty"`
	CobrarEl       string `json:"cobrar_el,omitempty"`
}

func (c *Catalogo) proponerMovimiento(ctx context.Context, usuarioID int64, crudos json.RawMessage) (Resultado, error) {
	var args argumentosMovimientoNuevo
	if err := json.Unmarshal(crudos, &args); err != nil {
		return texto(errorParaElModelo("no entendí los argumentos: %v", err)), nil
	}

	categoriaID, categoria, aviso, err := c.resolverCategoria(ctx, usuarioID, args.Categoria)
	if err != nil {
		return Resultado{}, err
	}
	if aviso != "" {
		return texto(aviso), nil
	}

	medioID, medio, aviso, err := c.resolverMedio(ctx, usuarioID, args.MedioPago, "medio_pago")
	if err != nil {
		return Resultado{}, err
	}
	if aviso != "" {
		return texto(aviso), nil
	}

	destinoID, destino, aviso, err := c.resolverMedio(ctx, usuarioID, args.MedioDestino, "medio_destino")
	if err != nil {
		return Resultado{}, err
	}
	if aviso != "" {
		return texto(aviso), nil
	}

	tipo := strings.TrimSpace(args.Tipo)

	// Una deuda nueva nace pendiente, y decirlo aquí ahorra una ronda entera.
	//
	// El formulario SÍ obliga a elegir estado —ahí la persona está viendo los
	// dos botones—, pero el modelo no tiene forma de saber que es obligatorio
	// hasta que la validación se lo rechaza, y esa ronda perdida es justo la
	// que hacía que un préstamo (que además necesita listar_contrapartes) se
	// pasara del tope y terminara en un 503 sin tarjeta.
	//
	// No es adivinar: "le presté 200 mil a Carlos" es una deuda viva, y si ya
	// se la pagaron el usuario lo cambia en la tarjeta, que trae el selector.
	// Cuando el modelo sí manda estado, manda el suyo.
	estado := strings.TrimSpace(args.Estado)
	if estado == "" && movimientos.EsDeuda(tipo) {
		estado = movimientos.EstadoPendiente
	}

	// Las MISMAS reglas del formulario: monto, fecha, que una deuda traiga a
	// quién y en qué estado, y que un traslado traiga dos medios distintos. Si
	// el agente tuviera su propia validación, el día que cambie una regla
	// tendría también su propio bug.
	entrada := movimientos.Entrada{
		CategoriaID:  categoriaID,
		MedioPagoID:  medioID,
		MedioCobroID: destinoID,
		Tipo:         tipo,
		Monto:        strings.TrimSpace(args.Monto),
		Fecha:        strings.TrimSpace(args.Fecha),
		Descripcion:  strings.TrimSpace(args.Descripcion),
		AQuien:       strings.TrimSpace(args.AQuien),
		Estado:       estado,
		CobrarEl:     strings.TrimSpace(args.CobrarEl),
	}

	datos, campos := movimientos.Validar(entrada)
	if len(campos) > 0 {
		// Los errores por campo vuelven tal cual: son instrucciones bastante
		// claras para que el modelo lo intente de nuevo bien.
		return texto(errorConCampos("el movimiento no quedó bien", campos)), nil
	}

	propuesta := datosMovimiento{
		Tipo:           datos.Tipo,
		Monto:          datos.Monto, // ya normalizado por dinero
		Fecha:          datos.Fecha,
		Descripcion:    datos.Descripcion,
		CategoriaID:    categoriaID,
		Categoria:      categoria,
		MedioPagoID:    medioID,
		MedioPago:      medio,
		MedioDestinoID: destinoID,
		MedioDestino:   destino,
		AQuien:         valor(datos.AQuien),
		Estado:         valor(datos.Estado),
		CobrarEl:       valor(datos.CobrarEl),
	}

	instruccion := "NO está registrado todavía. Al usuario le apareció una tarjeta con estos datos " +
		"para que los revise y confirme. Dile en una frase qué preparaste (tipo, monto y de qué es) " +
		"y pídele que confirme ahí. No digas que ya quedó guardado."
	if datos.Tipo == movimientos.TipoPague {
		// Un gasto con su soporte es un gasto que el contador acepta. La foto
		// no la ve el modelo: la adjunta el usuario en la tarjeta.
		instruccion += " Como es un gasto, pregúntale también si tiene la foto o el PDF de la factura: " +
			"puede adjuntarla en la misma tarjeta con el botón 📎 Adjuntar factura antes de guardar. " +
			"Si ya te dijo que la adjuntó, no se lo vuelvas a pedir."
	}
	if datos.Tipo == movimientos.TipoTraslado {
		// El traslado es el único tipo que no cambia el balance, y eso
		// sorprende si nadie lo dice.
		instruccion += " Aclárale que un traslado no cambia cuánta plata tiene en total: " +
			"solo la mueve de " + medio + " a " + destino + "."
	}

	confirmacion, err := aJSON(map[string]any{
		"estado":      "pendiente de confirmación",
		"preparado":   propuesta,
		"instruccion": instruccion,
	})
	if err != nil {
		return Resultado{}, err
	}

	return Resultado{
		Texto:     confirmacion,
		Propuesta: &PropuestaNueva{Tipo: TipoPropuestaMovimiento, Datos: propuesta},
	}, nil
}

type argumentosMarcarPagado struct {
	MovimientoID int64  `json:"movimiento_id"`
	MedioCobro   string `json:"medio_cobro"`
}

type datosMarcarPagado struct {
	MovimientoID int64 `json:"movimiento_id"`
	// Tipo dice de qué lado está la plata: 'preste' (te pagan) o
	// 'me_prestaron' (pagas tú). La tarjeta cambia el texto según esto.
	Tipo   string `json:"tipo"`
	AQuien string `json:"a_quien"`
	// Monto es el SALDO que falta, no el monto original: si ya abonaron una
	// parte, saldar es poner el resto.
	Monto        string `json:"monto"`
	Fecha        string `json:"fecha"`
	Descripcion  string `json:"descripcion"`
	MedioCobroID int64  `json:"medio_cobro_id"`
	MedioCobro   string `json:"medio_cobro"`
}

func (c *Catalogo) proponerMarcarPagado(ctx context.Context, usuarioID int64, crudos json.RawMessage) (Resultado, error) {
	var args argumentosMarcarPagado
	if err := json.Unmarshal(crudos, &args); err != nil {
		return texto(errorParaElModelo("no entendí los argumentos: %v", err)), nil
	}
	if args.MovimientoID <= 0 {
		return texto(errorParaElModelo("falta movimiento_id. Búscalo con listar_movimientos usando tipo=preste y estado=pendiente")), nil
	}

	// PorID filtra por usuario: un id de otra persona simplemente "no existe".
	m, err := c.movimientos.PorID(ctx, usuarioID, args.MovimientoID)
	if errors.Is(err, movimientos.ErrNoEncontrado) {
		return texto(errorParaElModelo("el usuario no tiene ningún movimiento con id %d", args.MovimientoID)), nil
	}
	if err != nil {
		return Resultado{}, fmt.Errorf("herramienta proponer_marcar_pagado: %w", err)
	}

	if !movimientos.EsDeuda(m.Tipo) {
		return texto(errorParaElModelo("ese movimiento no es una deuda, así que no se puede marcar como saldado")), nil
	}
	if valor(m.Estado) == movimientos.EstadoPagado {
		return texto(errorParaElModelo("esa deuda ya está saldada")), nil
	}

	// El medio de cobro es opcional: si no se sabe por dónde devolvieron la
	// plata, queda sin registrar y el usuario lo elige en la tarjeta.
	medioID, medio, aviso, err := c.resolverMedio(ctx, usuarioID, args.MedioCobro, "medio_cobro")
	if err != nil {
		return Resultado{}, err
	}
	if aviso != "" {
		return texto(aviso), nil
	}

	propuesta := datosMarcarPagado{
		MovimientoID: m.ID,
		Tipo:         m.Tipo,
		AQuien:       valor(m.AQuien),
		// El SALDO, no el monto: de un préstamo de 500.000 con 400.000 ya
		// abonados, saldarlo es registrar los 100.000 que faltan.
		Monto:        m.Saldo,
		Fecha:        m.Fecha,
		Descripcion:  m.Descripcion,
		MedioCobroID: medioID,
		MedioCobro:   medio,
	}

	instruccion := "Todavía NO está saldada. Al usuario le apareció una tarjeta para confirmarlo. " +
		"Dile en una frase de qué deuda se trata y pídele que confirme ahí, eligiendo por dónde se movió la plata."
	if m.Abonos > 0 {
		instruccion += " Ojo: esa deuda ya tenía abonos, así que lo que se va a registrar es solo lo que faltaba (" +
			m.Saldo + "), no el monto original. Díselo."
	}

	confirmacion, err := aJSON(map[string]any{
		"estado":      "pendiente de confirmación",
		"preparado":   propuesta,
		"instruccion": instruccion,
	})
	if err != nil {
		return Resultado{}, err
	}

	return Resultado{
		Texto:     confirmacion,
		Propuesta: &PropuestaNueva{Tipo: TipoPropuestaMarcarPagado, Datos: propuesta},
	}, nil
}

// --------------------------------------------------------------------------
// La escritura de verdad: solo desde la confirmacion del usuario
// --------------------------------------------------------------------------
//
// Estos dos metodos NO aparecen en Esquemas(): el modelo no puede llamarlos,
// no existen para el. Los usa el endpoint de confirmar, o sea que detras de
// cada uno hay un clic de una persona que vio los datos.

// CrearMovimiento valida y crea. Devuelve los errores por campo cuando lo que
// llego de la tarjeta no pasa las reglas — el usuario pudo haber editado
// cualquier cosa antes de confirmar.
func (c *Catalogo) CrearMovimiento(ctx context.Context, usuarioID int64, entrada movimientos.Entrada) (*movimientos.Movimiento, map[string]string, error) {
	datos, campos := movimientos.Validar(entrada)
	if len(campos) > 0 {
		return nil, campos, nil
	}

	m, err := c.movimientos.Crear(ctx, usuarioID, datos)
	switch {
	case errors.Is(err, movimientos.ErrCategoriaInvalida):
		return nil, map[string]string{"categoria_id": "La categoría no existe"}, nil
	case errors.Is(err, movimientos.ErrMedioInvalido):
		return nil, map[string]string{"medio_pago_id": "El medio de pago no existe"}, nil
	case errors.Is(err, movimientos.ErrCobroAntes):
		return nil, map[string]string{"cobrar_el": "No puede ser antes del día en que prestaste"}, nil
	case err != nil:
		return nil, nil, fmt.Errorf("creando el movimiento propuesto: %w", err)
	}
	return m, nil, nil
}

// MarcarPagado cobra un prestamo. Reusa el mismo metodo que el boton de la
// lista, con sus reglas: solo prestamos, y el medio de cobro solo existe
// cuando el prestamo queda pagado.
func (c *Catalogo) MarcarPagado(ctx context.Context, usuarioID, movimientoID, medioCobroID int64) (*movimientos.Movimiento, error) {
	var medio *int64
	if medioCobroID > 0 {
		medio = &medioCobroID
	}
	return c.movimientos.CambiarEstado(ctx, usuarioID, movimientoID, movimientos.EstadoPagado, medio)
}

// --------------------------------------------------------------------------

// resolverCategoria traduce el nombre que escribio el modelo al id real. El
// tercer valor es un aviso ya listo para devolverle: vacio si todo bien.
func (c *Catalogo) resolverCategoria(ctx context.Context, usuarioID int64, nombre string) (int64, string, string, error) {
	nombre = strings.TrimSpace(nombre)
	lista, err := c.categorias.Listar(ctx, usuarioID)
	if err != nil {
		return 0, "", "", fmt.Errorf("resolviendo la categoria: %w", err)
	}

	id, nombres := buscarPorNombre(nombre, len(lista), func(i int) (int64, string) {
		return lista[i].ID, lista[i].Nombre
	})

	if nombre == "" {
		return 0, "", errorParaElModelo(
			"falta la categoría: es obligatoria. Las del usuario son: %s. "+
				"Elige de esas la que mejor encaje con lo que te contó y vuelve a proponer, "+
				"diciéndole cuál elegiste; pregúntale solo si ninguna sirve.",
			strings.Join(nombres, ", ")), nil
	}
	if id == 0 {
		return 0, "", errorParaElModelo(
			"no existe la categoría %q. Las del usuario son: %s. "+
				"Elige de esas la que mejor encaje y dile cuál elegiste. No inventes categorías nuevas "+
				"ni lo intentes otra vez con un nombre que no esté en la lista.",
			nombre, strings.Join(nombres, ", ")), nil
	}

	return id, nombreExacto(lista, id), "", nil
}

// resolverMedio hace lo mismo con los medios de pago. Aqui el vacio SI es
// valido: "sin registrar" es una opcion legitima al cobrar un prestamo.
func (c *Catalogo) resolverMedio(ctx context.Context, usuarioID int64, nombre, campo string) (int64, string, string, error) {
	nombre = strings.TrimSpace(nombre)
	if nombre == "" {
		return 0, "", "", nil
	}

	lista, err := c.medios.Listar(ctx, usuarioID)
	if err != nil {
		return 0, "", "", fmt.Errorf("resolviendo el medio de pago: %w", err)
	}

	id, nombres := buscarPorNombre(nombre, len(lista), func(i int) (int64, string) {
		return lista[i].ID, lista[i].Nombre
	})
	if id == 0 {
		return 0, "", errorParaElModelo(
			"no existe el medio de pago %q para %s. Los del usuario son: %s. "+
				"Elige de esos el que mejor encaje y dile cuál elegiste; no inventes medios nuevos.",
			nombre, campo, strings.Join(nombres, ", ")), nil
	}

	for _, m := range lista {
		if m.ID == id {
			return id, m.Nombre, "", nil
		}
	}
	return id, nombre, "", nil
}

func nombreExacto(lista []categorias.Categoria, id int64) string {
	for _, c := range lista {
		if c.ID == id {
			return c.Nombre
		}
	}
	return ""
}

func texto(s string) Resultado { return Resultado{Texto: s} }

// errorConCampos le devuelve al modelo el detalle campo por campo, que es lo
// que necesita para corregir sin adivinar.
func errorConCampos(mensaje string, campos map[string]string) string {
	datos, err := json.Marshal(map[string]any{"error": mensaje, "campos": campos})
	if err != nil {
		return errorParaElModelo("%s", mensaje)
	}
	return string(datos)
}
