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
}

func NuevoCatalogo(m *movimientos.Store, c *categorias.Store, me *medios.Store) *Catalogo {
	return &Catalogo{movimientos: m, categorias: c, medios: me}
}

// Los nombres son los que ve el modelo; tambien son los que la app le muestra
// al usuario debajo de la respuesta.
const (
	HerramientaResumen     = "resumen"
	HerramientaMovimientos = "listar_movimientos"
	HerramientaCategorias  = "listar_categorias"
	HerramientaMedios      = "listar_medios_pago"

	// Las dos que preparan una escritura. Ojo con el nombre: "proponer", no
	// "crear". No escriben nada — dejan una tarjeta para que el usuario
	// confirme —, y el nombre es lo primero que lee el modelo.
	HerramientaProponerMovimiento = "proponer_movimiento"
	HerramientaProponerPagado     = "proponer_marcar_pagado"
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
	return []Herramienta{
		{
			Nombre: HerramientaResumen,
			Descripcion: "Devuelve el resumen financiero del usuario: totales (recibido, pagado, por cobrar, balance), " +
				"el saldo que tiene en cada medio de pago, el desglose por categoría y quién le debe plata. " +
				"Úsala para cualquier pregunta sobre cuánto tiene, cuánto lleva gastado o quién le debe.",
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
					"enum":        []string{movimientos.TipoRecibi, movimientos.TipoPague, movimientos.TipoPreste},
					"description": "recibi = entró plata, pague = salió plata, preste = se la llevó alguien y la debe",
				},
				"estado": map[string]any{
					"type":        "string",
					"enum":        []string{movimientos.EstadoPendiente, movimientos.EstadoPagado},
					"description": "Solo aplica a los préstamos: si ya se lo devolvieron o no",
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
			Nombre:      HerramientaCategorias,
			Descripcion: "Lista las categorías del usuario (de qué es la plata: Negocio, Personal...) con cuántos movimientos tiene cada una.",
			Parametros:  objeto(nil),
		},
		{
			Nombre:      HerramientaMedios,
			Descripcion: "Lista los medios de pago del usuario (por dónde entra o sale la plata: Efectivo, Transferencia, Nequi...).",
			Parametros:  objeto(nil),
		},
		{
			Nombre: HerramientaProponerMovimiento,
			Descripcion: "Prepara un movimiento para que el usuario lo confirme. NO lo registra: le aparece una tarjeta " +
				"con los datos, que puede corregir antes de guardar. Úsala cuando te cuente un gasto, un ingreso o un préstamo " +
				"('pagué 45 mil de almuerzo con Nequi'). Si te falta la categoría o el medio de pago, pregúntale: no los inventes.",
			Parametros: objeto(map[string]any{
				"tipo": map[string]any{
					"type":        "string",
					"enum":        []string{movimientos.TipoRecibi, movimientos.TipoPague, movimientos.TipoPreste},
					"description": "recibi = entró plata, pague = salió plata, preste = se la llevó alguien y la debe",
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
					"type":        "string",
					"description": "Nombre exacto de una categoría existente (obligatorio)",
				},
				"medio_pago": map[string]any{
					"type":        "string",
					"description": "Nombre exacto de un medio de pago existente: por dónde entró o salió la plata (obligatorio)",
				},
				"a_quien": map[string]any{
					"type":        "string",
					"description": "Solo para tipo preste: a quién se le prestó",
				},
				"estado": map[string]any{
					"type":        "string",
					"enum":        []string{movimientos.EstadoPendiente, movimientos.EstadoPagado},
					"description": "Solo para tipo preste: si ya se lo devolvieron o sigue pendiente",
				},
			}),
		},
		{
			Nombre: HerramientaProponerPagado,
			Descripcion: "Prepara el cobro de un préstamo para que el usuario lo confirme ('ya me pagó Juan'). NO lo marca: " +
				"le aparece una tarjeta para confirmarlo. Antes busca el préstamo con listar_movimientos " +
				"(tipo=preste, estado=pendiente) para saber su id.",
			Parametros: objeto(map[string]any{
				"movimiento_id": map[string]any{
					"type":        "integer",
					"description": "Id del préstamo, tal como salió en listar_movimientos",
				},
				"medio_cobro": map[string]any{
					"type":        "string",
					"description": "Nombre del medio por donde le devolvieron la plata. Si no lo dijo, déjalo vacío y que lo elija él",
				},
			}),
		},
	}
}

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

	case HerramientaProponerMovimiento:
		return c.proponerMovimiento(ctx, usuarioID, llamada.Argumentos)
	case HerramientaProponerPagado:
		return c.proponerMarcarPagado(ctx, usuarioID, llamada.Argumentos)

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
	Totales    movimientos.Totales `json:"totales"`
	Medios     []medioParaModelo   `json:"medios"`
	Categorias []rubroParaModelo   `json:"categorias"`
	Deudores   []deudorParaModelo  `json:"deudores"`
	Nota       string              `json:"nota"`
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
	Balance   string `json:"balance"`
}

type deudorParaModelo struct {
	AQuien string `json:"a_quien"`
	Debe   string `json:"debe"`
}

func (c *Catalogo) resumen(ctx context.Context, usuarioID int64) (string, error) {
	resumen, err := c.movimientos.Resumen(ctx, usuarioID)
	if err != nil {
		return "", fmt.Errorf("herramienta resumen: %w", err)
	}

	// Los slices arrancan vacios y no nil: un "deudores": null le da al modelo
	// una cosa mas que interpretar, y "[]" ya dice lo que hay que decir.
	salida := resumenParaModelo{
		Totales:    resumen.Totales,
		Medios:     []medioParaModelo{},
		Categorias: []rubroParaModelo{},
		Deudores:   []deudorParaModelo{},
		Nota: "Todas las cifras ya vienen sumadas por la base de datos. " +
			"Cópialas tal cual: no sumes, no restes, no conviertas.",
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
			Balance:   cat.Balance,
		})
	}
	for _, d := range resumen.Deudores {
		salida.Deudores = append(salida.Deudores, deudorParaModelo{AQuien: d.AQuien, Debe: d.Total})
	}

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
	AQuien      string `json:"a_quien,omitempty"`
	Estado      string `json:"estado,omitempty"`
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
		salida.Movimientos = append(salida.Movimientos, movimientoParaModelo{
			ID:          m.ID,
			Fecha:       m.Fecha,
			Tipo:        m.Tipo,
			Monto:       m.Monto,
			Descripcion: m.Descripcion,
			Categoria:   m.CategoriaNombre,
			MedioPago:   valor(m.MedioPagoNombre),
			AQuien:      valor(m.AQuien),
			Estado:      valor(m.Estado),
		})
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
	Tipo        string `json:"tipo"`
	Monto       string `json:"monto"`
	Fecha       string `json:"fecha"`
	Descripcion string `json:"descripcion"`
	Categoria   string `json:"categoria"`
	MedioPago   string `json:"medio_pago"`
	AQuien      string `json:"a_quien"`
	Estado      string `json:"estado"`
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
	AQuien      string `json:"a_quien,omitempty"`
	Estado      string `json:"estado,omitempty"`
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

	// Las MISMAS reglas del formulario: monto, fecha, y que "presté" traiga a
	// quién y en qué estado. Si el agente tuviera su propia validación, el día
	// que cambie una regla tendría también su propio bug.
	entrada := movimientos.Entrada{
		CategoriaID: categoriaID,
		MedioPagoID: medioID,
		Tipo:        strings.TrimSpace(args.Tipo),
		Monto:       strings.TrimSpace(args.Monto),
		Fecha:       strings.TrimSpace(args.Fecha),
		Descripcion: strings.TrimSpace(args.Descripcion),
		AQuien:      strings.TrimSpace(args.AQuien),
		Estado:      strings.TrimSpace(args.Estado),
	}

	datos, campos := movimientos.Validar(entrada)
	if len(campos) > 0 {
		// Los errores por campo vuelven tal cual: son instrucciones bastante
		// claras para que el modelo lo intente de nuevo bien.
		return texto(errorConCampos("el movimiento no quedó bien", campos)), nil
	}

	propuesta := datosMovimiento{
		Tipo:        datos.Tipo,
		Monto:       datos.Monto, // ya normalizado por dinero
		Fecha:       datos.Fecha,
		Descripcion: datos.Descripcion,
		CategoriaID: categoriaID,
		Categoria:   categoria,
		MedioPagoID: medioID,
		MedioPago:   medio,
		AQuien:      valor(datos.AQuien),
		Estado:      valor(datos.Estado),
	}

	confirmacion, err := aJSON(map[string]any{
		"estado":    "pendiente de confirmación",
		"preparado": propuesta,
		"instruccion": "NO está registrado todavía. Al usuario le apareció una tarjeta con estos datos " +
			"para que los revise y confirme. Dile en una frase qué preparaste (tipo, monto y de qué es) " +
			"y pídele que confirme ahí. No digas que ya quedó guardado.",
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
	MovimientoID int64  `json:"movimiento_id"`
	AQuien       string `json:"a_quien"`
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

	if m.Tipo != movimientos.TipoPreste {
		return texto(errorParaElModelo("ese movimiento no es un préstamo, así que no se puede marcar como pagado")), nil
	}
	if valor(m.Estado) == movimientos.EstadoPagado {
		return texto(errorParaElModelo("ese préstamo ya está marcado como pagado")), nil
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
		AQuien:       valor(m.AQuien),
		Monto:        m.Monto,
		Fecha:        m.Fecha,
		Descripcion:  m.Descripcion,
		MedioCobroID: medioID,
		MedioCobro:   medio,
	}

	confirmacion, err := aJSON(map[string]any{
		"estado":    "pendiente de confirmación",
		"preparado": propuesta,
		"instruccion": "Todavía NO está marcado como pagado. Al usuario le apareció una tarjeta para confirmarlo. " +
			"Dile en una frase de qué préstamo se trata y pídele que confirme ahí, eligiendo por dónde le pagaron.",
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
				"Si no está claro de cuál es, pregúntale antes de proponer nada.",
			strings.Join(nombres, ", ")), nil
	}
	if id == 0 {
		return 0, "", errorParaElModelo(
			"no existe la categoría %q. Las del usuario son: %s. "+
				"Usa una de esas o pregúntale cuál quiere; no inventes categorías nuevas.",
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
			"no existe el medio de pago %q para %s. Los del usuario son: %s",
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
