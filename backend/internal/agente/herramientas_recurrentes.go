package agente

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"finanzas/internal/movimientos"
	"finanzas/internal/recurrentes"
)

// Los gastos que se repiten, vistos desde el chat.
//
// EL PROBLEMA QUE RESUELVE ESTE ARCHIVO: hasta aquí el agente estaba ciego a
// los recurrentes. Si le decías "ya pagué el internet", preparaba un gasto
// suelto — y la ocurrencia del recurrente seguía esperando en el resumen.
// Después confirmabas esa también y el internet quedaba registrado DOS VECES.
// Dos caminos escribiendo en la misma tabla, sin saber uno del otro.
//
// Ahora el chat no crea un gasto paralelo: confirma LA MISMA ocurrencia que ya
// estaba pendiente, por el mismo Confirmador que usa el botón del resumen.

const (
	HerramientaRecurrentesPendientes = "listar_recurrentes_pendientes"

	// Prepara el pago de uno. Ojo con el nombre: "proponer", no "pagar". Deja
	// una tarjeta, y quien confirma es la persona.
	HerramientaProponerRecurrente = "proponer_pago_recurrente"
)

// TipoPropuestaRecurrente es la tarjeta que resuelve una ocurrencia.
const TipoPropuestaRecurrente = "recurrente"

// esquemasDeRecurrentes son las dos herramientas, aparte para que el catálogo
// pueda dejarlas fuera cuando el servidor no tiene recurrentes conectados.
func esquemasDeRecurrentes() []Herramienta {
	return []Herramienta{
		{
			Nombre: HerramientaRecurrentesPendientes,
			Descripcion: "Lista los gastos e ingresos que se repiten y están esperando confirmación: los que ya vencieron " +
				"y los que vienen en los próximos días. Llámala SIEMPRE que la persona diga que pagó o recibió algo que " +
				"suene a repetirse (el arriendo, el internet, la luz, el sueldo, la cuota): si lo que te contó está en " +
				"esta lista, NO es un gasto nuevo y se confirma con proponer_pago_recurrente.",
			Parametros: objeto(nil),
		},
		{
			Nombre: HerramientaProponerRecurrente,
			Descripcion: "Prepara el pago de un gasto recurrente que estaba pendiente, para que la persona lo confirme. " +
				"NO lo registra: le aparece una tarjeta con el monto, que puede corregir antes de guardar. " +
				"Úsala en vez de proponer_movimiento cuando lo que te contó coincide con algo de " +
				"listar_recurrentes_pendientes: así queda UN solo movimiento y el recurrente deja de aparecer pendiente. " +
				"Si usaras proponer_movimiento, ese gasto quedaría registrado dos veces.",
			Parametros: objeto(map[string]any{
				"ocurrencia_id": map[string]any{
					"type":        "integer",
					"description": "Id del pendiente, tal como salió en listar_recurrentes_pendientes",
				},
				"monto": map[string]any{
					"type": "string",
					"description": "Solo si la persona dijo un valor distinto al de siempre (el recibo casi nunca llega igual): " +
						"solo el número, sin puntos ni signos. Si no dijo cuánto, NO lo pongas y NO se lo inventes: " +
						"se usa el de la plantilla y ella lo corrige en la tarjeta",
				},
			}),
		},
	}
}

// pendienteParaModelo es un recurrente esperando confirmación, recortado a lo
// que el modelo necesita para reconocerlo y nombrarlo.
type pendienteParaModelo struct {
	OcurrenciaID int64  `json:"ocurrencia_id"`
	Descripcion  string `json:"descripcion"`
	Tipo         string `json:"tipo"`
	// Monto es lo que dice la plantilla, que es lo que se cobra de costumbre.
	// No es necesariamente lo que llegó este mes.
	Monto     string `json:"monto_de_siempre"`
	Fecha     string `json:"fecha"`
	Categoria string `json:"categoria"`
	Medio     string `json:"medio_pago"`
	// YaVencio separa "esto ya te tocaba" de "esto se viene". Las dos se
	// pueden confirmar: el arriendo del 5 a veces se paga el 2.
	YaVencio bool `json:"ya_vencio"`
}

func (c *Catalogo) listarRecurrentesPendientes(ctx context.Context, usuarioID int64) (string, error) {
	if c.recurrentes == nil {
		return errorParaElModelo("este servidor no tiene gastos recurrentes"), nil
	}

	hoy := recurrentes.HoyEnColombia()

	vencidos, err := c.recurrentes.Pendientes(ctx, usuarioID, hoy)
	if err != nil {
		return "", fmt.Errorf("listando recurrentes pendientes: %w", err)
	}
	proximos, err := c.recurrentes.Proximas(ctx, usuarioID, hoy)
	if err != nil {
		return "", fmt.Errorf("listando recurrentes proximos: %w", err)
	}

	lista := make([]pendienteParaModelo, 0, len(vencidos)+len(proximos))
	for _, o := range vencidos {
		lista = append(lista, recortarPendiente(o, true))
	}
	for _, o := range proximos {
		lista = append(lista, recortarPendiente(o, false))
	}

	if len(lista) == 0 {
		return aJSON(map[string]any{
			"pendientes": lista,
			"nota": "No hay ningún recurrente esperando confirmación. Si te contó un gasto, " +
				"entonces es uno nuevo: prepáralo con proponer_movimiento.",
		})
	}

	return aJSON(map[string]any{
		"pendientes": lista,
		"nota": "El monto que ves es el de la plantilla, lo que se cobra de costumbre. " +
			"Si la persona te dijo otro valor, manda ESE en proponer_pago_recurrente.",
	})
}

func recortarPendiente(o recurrentes.Ocurrencia, vencio bool) pendienteParaModelo {
	return pendienteParaModelo{
		OcurrenciaID: o.ID,
		Descripcion:  o.Descripcion,
		Tipo:         o.Tipo,
		Monto:        o.Monto,
		Fecha:        o.Fecha,
		Categoria:    o.CategoriaNombre,
		Medio:        o.MedioPagoNombre,
		YaVencio:     vencio,
	}
}

type argumentosRecurrente struct {
	OcurrenciaID int64  `json:"ocurrencia_id"`
	Monto        string `json:"monto"`
}

// datosRecurrente es lo que pinta la tarjeta del chat.
type datosRecurrente struct {
	OcurrenciaID int64  `json:"ocurrencia_id"`
	Tipo         string `json:"tipo"`
	Descripcion  string `json:"descripcion"`
	Fecha        string `json:"fecha"`
	// Monto es el que se va a guardar: el que dijo la persona si dijo alguno,
	// y si no el de la plantilla.
	Monto string `json:"monto"`
	// MontoDeSiempre es el de la plantilla. Va en la tarjeta para que se vea
	// la diferencia cuando este mes llegó distinto.
	MontoDeSiempre string `json:"monto_de_siempre"`
	CategoriaID    int64  `json:"categoria_id"`
	Categoria      string `json:"categoria"`
	MedioPagoID    int64  `json:"medio_pago_id"`
	MedioPago      string `json:"medio_pago"`
}

func (c *Catalogo) proponerRecurrente(ctx context.Context, usuarioID int64, crudos json.RawMessage) (Resultado, error) {
	if c.recurrentes == nil {
		return texto(errorParaElModelo("este servidor no tiene gastos recurrentes")), nil
	}

	var args argumentosRecurrente
	if err := json.Unmarshal(crudos, &args); err != nil {
		return texto(errorParaElModelo("no entendí los argumentos: %v", err)), nil
	}
	if args.OcurrenciaID <= 0 {
		return texto(errorParaElModelo(
			"falta ocurrencia_id. Búscalo con listar_recurrentes_pendientes")), nil
	}

	// PorID filtra por usuario: un id de otra persona simplemente "no existe".
	o, err := c.recurrentes.Ocurrencia(ctx, usuarioID, args.OcurrenciaID)
	if err != nil {
		if err == recurrentes.ErrOcurrenciaNoExiste {
			return texto(errorParaElModelo(
				"no existe un recurrente pendiente con id %d. Vuelve a mirar listar_recurrentes_pendientes",
				args.OcurrenciaID)), nil
		}
		return Resultado{}, err
	}
	if o.Estado != recurrentes.Pendiente {
		return texto(errorParaElModelo(
			"ese recurrente ya se había resuelto: %q del %s. Si de verdad lo pagó otra vez, "+
				"es un gasto nuevo y va por proponer_movimiento", o.Descripcion, o.Fecha)), nil
	}

	// El monto que dijo la persona manda sobre el de la plantilla. Se
	// normaliza con las mismas reglas del formulario: "118.000" y "118000"
	// son lo mismo, y "como cien mil" no es un monto.
	monto := o.Monto
	if dicho := strings.TrimSpace(args.Monto); dicho != "" {
		entrada := movimientos.Entrada{
			CategoriaID: o.CategoriaID,
			MedioPagoID: o.MedioPagoID,
			Tipo:        o.Tipo,
			Monto:       dicho,
			Fecha:       o.Fecha,
			Descripcion: o.Descripcion,
		}
		datos, campos := movimientos.Validar(entrada)
		if len(campos) > 0 {
			return texto(errorConCampos("el pago no quedó bien", campos)), nil
		}
		monto = datos.Monto
	}

	propuesta := datosRecurrente{
		OcurrenciaID:   o.ID,
		Tipo:           o.Tipo,
		Descripcion:    o.Descripcion,
		Fecha:          o.Fecha,
		Monto:          monto,
		MontoDeSiempre: o.Monto,
		CategoriaID:    o.CategoriaID,
		Categoria:      o.CategoriaNombre,
		MedioPagoID:    o.MedioPagoID,
		MedioPago:      o.MedioPagoNombre,
	}

	instruccion := "NO está registrado todavía. Al usuario le apareció una tarjeta para que revise el monto " +
		"y confirme. Dile en una frase qué preparaste y pídele que confirme ahí, diciéndole con qué monto quedó. " +
		"No digas que ya quedó guardado."
	if monto != o.Monto {
		// Que se note el cambio: confirmar sin mirar un monto distinto al de
		// siempre es justo lo que esta tarjeta viene a evitar.
		instruccion += fmt.Sprintf(" Aclárale que lo dejaste en %s y no en los %s de siempre, "+
			"para que lo revise antes de confirmar.", monto, o.Monto)
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
		Propuesta: &PropuestaNueva{Tipo: TipoPropuestaRecurrente, Datos: propuesta},
	}, nil
}
