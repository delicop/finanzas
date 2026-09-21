package agente

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"finanzas/internal/dinero"
	"finanzas/internal/movimientos"
)

// Lo que el asistente sabe hacer con las deudas: mirar con quien hay cuentas
// pendientes y preparar un abono parcial.
//
// Estas dos cosas van juntas porque resuelven el mismo malentendido. El modelo
// oye "Carlos me abonó 50" y, sin contrapartes, no sabe si "Carlos" es alguien
// que ya existe o un nombre nuevo; y sin abonos, lo unico que podia hacer con
// un pago parcial era dar el prestamo entero por saldado.

// --------------------------------------------------------------------------
// listar_contrapartes
// --------------------------------------------------------------------------

func contrapartesParaModelo(lista []movimientos.Contraparte) []contraparteParaModelo {
	salida := []contraparteParaModelo{}
	for _, c := range lista {
		fila := contraparteParaModelo{
			Nombre:      c.Nombre,
			TeDebe:      c.TeDeben,
			LeDebes:     c.LeDebes,
			Neto:        c.Neto,
			EsCategoria: c.EsCategoria,
		}
		if c.ProximaFecha != nil {
			fila.ProximaFecha = *c.ProximaFecha
		}
		salida = append(salida, fila)
	}
	return salida
}

type contrapartesConNota struct {
	Contrapartes []contraparteParaModelo `json:"contrapartes"`
	Nota         string                  `json:"nota"`
}

func (c *Catalogo) listarContrapartes(ctx context.Context, usuarioID int64) (string, error) {
	resumen, err := c.movimientos.Resumen(ctx, usuarioID)
	if err != nil {
		return "", fmt.Errorf("herramienta listar_contrapartes: %w", err)
	}

	return aJSON(contrapartesConNota{
		Contrapartes: contrapartesParaModelo(resumen.Contrapartes),
		Nota: "Solo salen las que tienen saldo vivo. Al registrar una deuda nueva, escribe el nombre " +
			"IGUAL a como aparece aquí: si escribes 'Carlos M' donde ya dice 'Carlos', quedan como dos " +
			"personas distintas y ninguno de los dos saldos será cierto. " +
			"Si es_categoria viene en true, ese nombre también es una categoría del usuario: avísale, " +
			"porque no son lo mismo (la categoría dice de qué es la plata; la contraparte, con quién es la deuda).",
	})
}

// --------------------------------------------------------------------------
// proponer_abono
// --------------------------------------------------------------------------

type argumentosAbono struct {
	MovimientoID int64  `json:"movimiento_id"`
	Monto        string `json:"monto"`
	Fecha        string `json:"fecha"`
	Medio        string `json:"medio"`
	Nota         string `json:"nota"`
}

// datosAbono es lo que pinta la tarjeta.
type datosAbono struct {
	MovimientoID int64  `json:"movimiento_id"`
	Tipo         string `json:"tipo"` // preste | me_prestaron
	AQuien       string `json:"a_quien"`
	Monto        string `json:"monto"`
	Fecha        string `json:"fecha"`
	MedioID      int64  `json:"medio_id"`
	Medio        string `json:"medio"`
	Nota         string `json:"nota,omitempty"`

	// Lo que se debia ANTES de este abono y lo que quedaria despues. Van en la
	// tarjeta para que el usuario vea la consecuencia antes de confirmar, que
	// es toda la gracia de que esto pase por una tarjeta.
	SaldoActual string `json:"saldo_actual"`
}

func (c *Catalogo) proponerAbono(ctx context.Context, usuarioID int64, crudos json.RawMessage) (Resultado, error) {
	var args argumentosAbono
	if err := json.Unmarshal(crudos, &args); err != nil {
		return texto(errorParaElModelo("no entendí los argumentos: %v", err)), nil
	}
	if args.MovimientoID <= 0 {
		return texto(errorParaElModelo(
			"falta movimiento_id. Búscalo con listar_movimientos usando tipo=preste o tipo=me_prestaron")), nil
	}

	// PorID filtra por usuario: un id de otra persona simplemente "no existe".
	m, err := c.movimientos.PorID(ctx, usuarioID, args.MovimientoID)
	if errors.Is(err, movimientos.ErrNoEncontrado) {
		return texto(errorParaElModelo("el usuario no tiene ningún movimiento con id %d", args.MovimientoID)), nil
	}
	if err != nil {
		return Resultado{}, fmt.Errorf("herramienta proponer_abono: %w", err)
	}

	if !movimientos.EsDeuda(m.Tipo) {
		return texto(errorParaElModelo(
			"ese movimiento no es una deuda (es un %q), así que no se le pueden registrar abonos", m.Tipo)), nil
	}
	if valor(m.Estado) == movimientos.EstadoPagado {
		return texto(errorParaElModelo("esa deuda ya está saldada: no queda nada por abonar")), nil
	}

	// Las MISMAS reglas del formulario de abonos. Si el agente tuviera las
	// suyas, el dia que cambie una regla tendria tambien su propio bug.
	entrada := movimientos.EntradaAbono{
		Monto: strings.TrimSpace(args.Monto),
		Fecha: strings.TrimSpace(args.Fecha),
		Nota:  strings.TrimSpace(args.Nota),
	}
	datos, campos := movimientos.ValidarAbono(entrada)
	if len(campos) > 0 {
		return texto(errorConCampos("el abono no quedó bien", campos)), nil
	}

	// Que el abono no se pase del saldo lo vuelve a revisar el store al
	// confirmar, dentro de su transaccion. Aqui se avisa antes para que el
	// modelo corrija en la misma conversacion en vez de dejar una tarjeta que
	// va a fallar al guardarse.
	if mayor(datos.Monto, m.Saldo) {
		return texto(errorParaElModelo(
			"ese abono (%s) es mayor que lo que falta de la deuda (%s). "+
				"Si le pagaron todo, usa proponer_marcar_pagado; si no, pregúntale cuánto fue exactamente",
			datos.Monto, m.Saldo)), nil
	}

	medioID, medio, aviso, err := c.resolverMedio(ctx, usuarioID, args.Medio, "medio")
	if err != nil {
		return Resultado{}, err
	}
	if aviso != "" {
		return texto(aviso), nil
	}

	propuesta := datosAbono{
		MovimientoID: m.ID,
		Tipo:         m.Tipo,
		AQuien:       valor(m.AQuien),
		Monto:        datos.Monto,
		Fecha:        datos.Fecha,
		MedioID:      medioID,
		Medio:        medio,
		Nota:         datos.Nota,
		SaldoActual:  m.Saldo,
	}

	instruccion := "El abono NO está registrado todavía. Al usuario le apareció una tarjeta para confirmarlo. " +
		"Dile en una frase de qué deuda se trata, cuánto es el abono y cuánto quedaría faltando, " +
		"y pídele que confirme ahí."
	if m.Tipo == movimientos.TipoMePrestaron && medioID > 0 {
		aviso, err := c.avisoSinFondos(ctx, usuarioID, medioID, datos.Monto)
		if err != nil {
			return Resultado{}, err
		}
		instruccion += aviso
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
		Propuesta: &PropuestaNueva{Tipo: TipoPropuestaAbono, Datos: propuesta},
	}, nil
}

// mayor compara dos montos en texto SIN pasar por float.
//
// Los dos vienen normalizados por el paquete dinero (hasta 12 enteros y 2
// decimales), asi que alcanza con alinear los decimales y comparar como texto
// una vez igualado el largo de la parte entera. Es fea pero es exacta, que es
// lo unico que importa cuando se compara plata.
func mayor(a, b string) bool {
	ea, da := partirMonto(a)
	eb, db := partirMonto(b)

	// Se rellena con ceros a la izquierda para que "100" y "99" se comparen
	// como "100" y "099": sin esto, "99" > "100" como texto.
	for len(ea) < len(eb) {
		ea = "0" + ea
	}
	for len(eb) < len(ea) {
		eb = "0" + eb
	}
	if ea != eb {
		return ea > eb
	}
	return da > db
}

func partirMonto(m string) (entero, decimales string) {
	entero, decimales, _ = strings.Cut(strings.TrimSpace(m), ".")
	// Siempre dos decimales: "5" y "50" no se pueden comparar directo.
	for len(decimales) < 2 {
		decimales += "0"
	}
	return entero, decimales
}

// --------------------------------------------------------------------------
// La escritura, solo desde la confirmacion del usuario
// --------------------------------------------------------------------------

// Abonar registra el abono. NO aparece en Esquemas(): el modelo no puede
// llamarla, no existe para el. La usa el endpoint de confirmar, o sea que
// detras hay un clic de una persona que vio la tarjeta.
func (c *Catalogo) Abonar(ctx context.Context, usuarioID, movimientoID int64, entrada movimientos.EntradaAbono) (*movimientos.Movimiento, map[string]string, error) {
	datos, campos := movimientos.ValidarAbono(entrada)
	if len(campos) > 0 {
		return nil, campos, nil
	}

	m, err := c.movimientos.Abonar(ctx, usuarioID, movimientoID, datos)
	switch {
	case errors.Is(err, movimientos.ErrAbonoDeMas):
		return nil, map[string]string{"monto": "El abono es mayor que lo que falta"}, nil
	case errors.Is(err, movimientos.ErrSinSaldo):
		return nil, map[string]string{"monto": "Esa deuda ya está saldada"}, nil
	case errors.Is(err, movimientos.ErrMedioInvalido):
		return nil, map[string]string{"medio_id": "El medio de pago no existe"}, nil
	case err != nil:
		return nil, nil, err
	}
	return m, nil, nil
}

// avisoSinFondos es lo que se le agrega a la instruccion cuando el usuario va
// a PAGAR (una deuda suya) con plata que no hay en ese medio. Vacio si
// alcanza. Sin esto, la tarjeta salia normal y solo al confirmarla aparecia
// el "no te alcanza": la conversacion ya habia seguido de largo.
func (c *Catalogo) avisoSinFondos(ctx context.Context, usuarioID, medioID int64, monto string) (string, error) {
	var falta *movimientos.FaltaPlata
	err := c.movimientos.VerificarPago(ctx, usuarioID, medioID, monto)
	if !errors.As(err, &falta) {
		return "", err
	}
	aviso := fmt.Sprintf(" OJO: en %s solo hay %s y este pago es de %s, así que faltan %s. "+
		"Nadie puede quedar en negativo: pregúntale de dónde salió lo que falta (si se lo prestaron, "+
		"si fue un ingreso o si lo pasó de otro medio). La tarjeta también se lo preguntará al confirmar.",
		falta.Medio, dinero.Formatear(falta.Disponible), dinero.Formatear(falta.Monto), dinero.Formatear(falta.Falta))
	if len(falta.EnOtros) > 0 {
		aviso += fmt.Sprintf(" En sus otros medios tiene %s: pregúntale primero si usó esa plata.",
			dinero.Formatear(falta.OtrosTotal))
	}
	return aviso, nil
}
