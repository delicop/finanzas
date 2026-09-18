package avisos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"finanzas/internal/movimientos"
	"finanzas/internal/push"
	"finanzas/internal/recurrentes"
	"finanzas/internal/suscripciones"
)

// Generador decide que avisos toca crear. Lo llama una tarea de fondo cada
// pocas horas; volver a evaluarlo todo es barato y, como cada aviso lleva su
// clave de periodo, repetir una corrida no repite un aviso.
type Generador struct {
	store       *Store
	negocio     *suscripciones.Store
	recurrentes *recurrentes.Store
	redactor    Redactor // opcional: sin modelo configurado, es nil

	// notificador manda el aviso ademas al celular. Opcional: sin llaves VAPID
	// configuradas es nil y todo lo demas funciona igual, con el aviso
	// quedandose en la campana de la app.
	notificador *push.Notificador
}

func NuevoGenerador(store *Store, negocio *suscripciones.Store, rec *recurrentes.Store, redactor Redactor, notificador *push.Notificador) *Generador {
	return &Generador{
		store:       store,
		negocio:     negocio,
		recurrentes: rec,
		redactor:    redactor,
		notificador: notificador,
	}
}

// Correr evalua los avisos para cada cuenta activa y devuelve cuantos
// creo. Un fallo con un usuario no detiene a los demas: se junta al final.
func (g *Generador) Correr(ctx context.Context, ahora time.Time) (int, error) {
	destinatarios, err := g.store.Destinatarios(ctx)
	if err != nil {
		return 0, err
	}

	var creados int
	var fallos []error

	for _, d := range destinatarios {
		// Si el context se cancelo (se esta apagando el servidor) paramos aqui
		// en vez de arrastrar errores por cada usuario que quede.
		if ctx.Err() != nil {
			return creados, ctx.Err()
		}

		n, err := g.paraUsuario(ctx, d, ahora)
		creados += n
		if err != nil {
			fallos = append(fallos, fmt.Errorf("usuario %d: %w", d.ID, err))
		}
	}

	return creados, errors.Join(fallos...)
}

func (g *Generador) paraUsuario(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	var creados int
	var fallos []error

	for _, regla := range []func(context.Context, Destinatario, time.Time) (int, error){
		g.resumenSemanal,
		g.prestamosPendientes,
		g.deudasPropias,
		g.cobrosDelMes,
		g.cobrosDelDia,
		g.cuotasVencidas,
		g.recurrentesPendientes,
	} {
		n, err := regla(ctx, d, ahora)
		creados += n
		if err != nil {
			fallos = append(fallos, err)
		}
	}

	return creados, errors.Join(fallos...)
}

// --------------------------------------------------------------------------
// Las reglas. Cada una devuelve cuantos avisos creo.
// --------------------------------------------------------------------------

// resumenSemanal: que paso la semana pasada (lunes a domingo).
//
// Sin movimientos no hay aviso. Un "esta semana no registraste nada" no le
// sirve a nadie, y de paso eso deja fuera al administrador, que no lleva
// finanzas propias.
func (g *Generador) resumenSemanal(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	desde, hasta, clave := semanaAnterior(ahora)

	semana, err := g.store.ResumenSemana(ctx, d.ID, desde.Format(formatoFecha), hasta.Format(formatoFecha))
	if err != nil {
		return 0, err
	}
	if semana.Movimientos == 0 {
		return 0, nil
	}

	// El titulo cuenta lo que mas pesa de la semana: "pagaste $0" en una
	// semana en la que solo se presto no le dice nada a nadie.
	titulo := fmt.Sprintf("Tu semana: pagaste %s", pesos(semana.Pagado))
	switch {
	case !esCero(semana.Pagado):
	case !esCero(semana.Prestado):
		titulo = fmt.Sprintf("Tu semana: prestaste %s", pesos(semana.Prestado))
	case !esCero(semana.Recibido):
		titulo = fmt.Sprintf("Tu semana: recibiste %s", pesos(semana.Recibido))
	}

	var base strings.Builder
	fmt.Fprintf(&base, "Del %s registraste %d movimiento%s: recibiste %s y pagaste %s.",
		rangoEnEspanol(desde, hasta), semana.Movimientos, plural(semana.Movimientos),
		pesos(semana.Recibido), pesos(semana.Pagado))

	if semana.CategoriaTop != "" {
		fmt.Fprintf(&base, " En lo que más se te fue fue en %s: %s.",
			semana.CategoriaTop, pesos(semana.MontoTop))
	}
	if !esCero(semana.Prestado) {
		fmt.Fprintf(&base, " Además prestaste %s.", pesos(semana.Prestado))
	}
	if !esCero(semana.MePrestaron) {
		fmt.Fprintf(&base, " Y te prestaron %s.", pesos(semana.MePrestaron))
	}

	return g.guardar(ctx, d, Nuevo{
		UsuarioID: d.ID,
		Tipo:      TipoResumenSemanal,
		Clave:     clave,
		Titulo:    titulo,
		Cuerpo:    base.String(),
	})
}

// prestamosPendientes: la plata que lleva un mes o mas afuera.
//
// La clave es el mes, no la semana: recordar lo mismo cada siete dias no hace
// que se lo paguen mas rapido, solo que deje de leer los avisos.
func (g *Generador) prestamosPendientes(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	prestamos, err := g.store.DeudasViejas(ctx, d.ID, movimientos.TipoPreste, DiasPrestamoViejo)
	if err != nil {
		return 0, err
	}
	if prestamos.Cantidad == 0 {
		return 0, nil
	}

	titulo := fmt.Sprintf("Tienes %s sin cobrar", pesos(prestamos.Total))

	base := fmt.Sprintf(
		"Llevas %d préstamo%s pendiente%s desde hace más de %d días, por %s en total.",
		prestamos.Cantidad, plural(prestamos.Cantidad), plural(prestamos.Cantidad),
		DiasPrestamoViejo, pesos(prestamos.Total))

	if prestamos.AQuien != "" {
		base += fmt.Sprintf(" El más antiguo es el de %s, de hace %d días.",
			prestamos.AQuien, prestamos.DiasMasViejo)
	}

	return g.guardar(ctx, d, Nuevo{
		UsuarioID: d.ID,
		Tipo:      TipoPrestamosPendientes,
		Clave:     ahora.Format(formatoMes),
		Titulo:    titulo,
		Cuerpo:    base,
	})
}

// deudasPropias es el espejo del anterior: lo que TU llevas tiempo debiendo.
//
// Va aparte y no mezclado en un solo aviso porque son dos acciones distintas:
// una se resuelve escribiendole a alguien, la otra sacando plata. Juntarlas en
// un parrafo ("te deben 300 y debes 200") no le dice a nadie que hacer hoy.
func (g *Generador) deudasPropias(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	deudas, err := g.store.DeudasViejas(ctx, d.ID, movimientos.TipoMePrestaron, DiasPrestamoViejo)
	if err != nil {
		return 0, err
	}
	if deudas.Cantidad == 0 {
		return 0, nil
	}

	titulo := fmt.Sprintf("Debes %s", pesos(deudas.Total))

	base := fmt.Sprintf(
		"Llevas %d deuda%s sin pagar desde hace más de %d días, por %s en total.",
		deudas.Cantidad, plural(deudas.Cantidad), DiasPrestamoViejo, pesos(deudas.Total))

	if deudas.AQuien != "" {
		base += fmt.Sprintf(" La más antigua es con %s, de hace %d días.",
			deudas.AQuien, deudas.DiasMasViejo)
	}

	return g.guardar(ctx, d, Nuevo{
		UsuarioID: d.ID,
		Tipo:      TipoDeudasPropias,
		Clave:     ahora.Format(formatoMes),
		Titulo:    titulo,
		Cuerpo:    base,
	})
}

// cobrosDelMes: a quien le falta pagar la suscripcion. Solo para el dueno del
// servidor, que es el unico que cobra.
//
// No sale el dia 1 a proposito: el mes recien empieza y "te faltan todos" no
// es informacion. A partir del dia 5 la cifra ya dice algo.
func (g *Generador) cobrosDelMes(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	if d.Rol != rolAdmin || ahora.Day() < diaDelAvisoDeCobros {
		return 0, nil
	}

	periodo := ahora.Format(formatoMes)

	// El tablero del negocio recibe el periodo como una fecha con dia 1, que
	// es como estan normalizados los pagos en la base. La clave del aviso, en
	// cambio, es el mes pelado: es lo que se lee.
	resumen, err := g.negocio.Resumen(ctx, periodo+"-01")
	if err != nil {
		return 0, err
	}
	if len(resumen.Pendientes) == 0 {
		return 0, nil
	}

	titulo := fmt.Sprintf("Te falta cobrar %s este mes", pesos(resumen.Pendiente))

	// Sin "de X esperados": con clientes anuales, lo esperado es un promedio
	// mensual y no lo que toca cobrar este mes, y juntar las dos cifras en una
	// frase invitaria a restarlas.
	base := fmt.Sprintf(
		"Este mes llevas %s cobrados. Faltan %d cliente%s por pagar: %s.",
		pesos(resumen.Cobrado),
		len(resumen.Pendientes), plural(len(resumen.Pendientes)),
		nombresDe(resumen.Pendientes))

	return g.guardar(ctx, d, Nuevo{
		UsuarioID: d.ID,
		Tipo:      TipoCobrosDelMes,
		Clave:     periodo,
		Titulo:    titulo,
		Cuerpo:    base,
	})
}

// cobrosDelDia: hoy es el dia en que quedaron de devolver un prestamo.
//
// Un aviso por prestamo, con la fecha en la clave: si el usuario cambia la
// fecha de cobro, el nuevo dia tambien avisa. Si el servidor estuvo apagado
// ese dia, sale al volver (hasta DiasGraciaCobro despues), diciendo que era
// para ese dia y no "hoy".
func (g *Generador) cobrosDelDia(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	hoy := ahora.In(zonaColombia)
	desde := hoy.AddDate(0, 0, -DiasGraciaCobro)

	cobros, err := g.store.CobrosDelDia(ctx, d.ID, desde.Format(formatoFecha), hoy.Format(formatoFecha))
	if err != nil {
		return 0, err
	}

	var creados int
	var fallos []error
	for _, c := range cobros {
		quien := strings.TrimSpace(c.AQuien)
		esHoy := c.CobrarEl == hoy.Format(formatoFecha)

		// Los dos sentidos usan la misma consulta y el mismo ciclo; lo unico
		// que cambia es de quien es la plata y, por tanto, que tiene que hacer
		// el usuario hoy.
		tipo := TipoCobroDelDia
		if c.Tipo == movimientos.TipoMePrestaron {
			tipo = TipoPagoDelDia
		}

		var titulo, base string
		switch {
		case tipo == TipoCobroDelDia && esHoy:
			titulo = fmt.Sprintf("Hoy te paga %s", quien)
			base = fmt.Sprintf("Hoy es el día en que %s quedó de devolverte %s", quien, pesos(c.Monto))
		case tipo == TipoCobroDelDia:
			titulo = fmt.Sprintf("%s quedó de pagarte el %s", quien, diaEnEspanol(c.CobrarEl))
			base = fmt.Sprintf("El %s era el día en que %s quedó de devolverte %s, y sigue pendiente",
				diaEnEspanol(c.CobrarEl), quien, pesos(c.Monto))
		case esHoy:
			titulo = fmt.Sprintf("Hoy le pagas a %s", quien)
			base = fmt.Sprintf("Hoy es el día en que quedaste de devolverle %s a %s", pesos(c.Monto), quien)
		default:
			titulo = fmt.Sprintf("Le debías a %s desde el %s", quien, diaEnEspanol(c.CobrarEl))
			base = fmt.Sprintf("El %s era el día en que quedaste de devolverle %s a %s, y sigue pendiente",
				diaEnEspanol(c.CobrarEl), pesos(c.Monto), quien)
		}

		if tipo == TipoCobroDelDia {
			base += fmt.Sprintf(" (se lo prestaste el %s", diaEnEspanol(c.Fecha))
		} else {
			base += fmt.Sprintf(" (te lo prestó el %s", diaEnEspanol(c.Fecha))
		}
		if desc := strings.TrimSpace(c.Descripcion); desc != "" {
			base += ": " + desc
		}
		if tipo == TipoCobroDelDia {
			base += "). Cuando te pague, regístralo en Movimientos."
		} else {
			base += "). Cuando le pagues, regístralo en Movimientos."
		}

		n, err := g.guardar(ctx, d, Nuevo{
			UsuarioID: d.ID,
			Tipo:      tipo,
			Clave:     fmt.Sprintf("%d:%s", c.MovimientoID, c.CobrarEl),
			Titulo:    titulo,
			Cuerpo:    base,
		})
		creados += n
		if err != nil {
			fallos = append(fallos, err)
		}
	}
	return creados, errors.Join(fallos...)
}

// cuotasVencidas: una cuota del acuerdo llego a su fecha y los abonos no la
// cubren.
//
// Un aviso POR CUOTA, con su id en la clave. No se junta todo en "tienes 3
// cuotas vencidas" porque cada una tiene su monto y su fecha, y lo que el
// usuario necesita saber es cuanto poner para ponerse al dia con la primera.
func (g *Generador) cuotasVencidas(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	hoy := ahora.In(zonaColombia)
	desde := hoy.AddDate(0, 0, -DiasGraciaCuota)

	cuotas, err := g.store.CuotasVencidas(ctx, d.ID, desde.Format(formatoFecha), hoy.Format(formatoFecha))
	if err != nil {
		return 0, err
	}

	var creados int
	var fallos []error
	for _, c := range cuotas {
		quien := strings.TrimSpace(c.AQuien)
		esHoy := c.VenceEl == hoy.Format(formatoFecha)

		var titulo, base string
		if c.Tipo == movimientos.TipoMePrestaron {
			if esHoy {
				titulo = fmt.Sprintf("Hoy vence tu cuota %d con %s", c.Numero, quien)
			} else {
				titulo = fmt.Sprintf("Se te venció la cuota %d con %s", c.Numero, quien)
			}
			base = fmt.Sprintf("La cuota %d de %d del acuerdo con %s vencía el %s y falta %s.",
				c.Numero, c.DeCuantas, quien, diaEnEspanol(c.VenceEl), pesos(c.Falta))
		} else {
			if esHoy {
				titulo = fmt.Sprintf("Hoy vence la cuota %d de %s", c.Numero, quien)
			} else {
				titulo = fmt.Sprintf("%s se atrasó con la cuota %d", quien, c.Numero)
			}
			base = fmt.Sprintf("La cuota %d de %d que %s quedó de pagarte vencía el %s y falta %s.",
				c.Numero, c.DeCuantas, quien, diaEnEspanol(c.VenceEl), pesos(c.Falta))
		}

		n, err := g.guardar(ctx, d, Nuevo{
			UsuarioID: d.ID,
			Tipo:      TipoCuotaVencida,
			// El id de la cuota, no el numero: si el acuerdo se renegocia, las
			// cuotas nuevas son otras filas y vuelven a avisar. Es lo correcto
			// — es un acuerdo distinto.
			Clave:  fmt.Sprint(c.CuotaID),
			Titulo: titulo,
			Cuerpo: base,
		})
		creados += n
		if err != nil {
			fallos = append(fallos, err)
		}
	}
	return creados, errors.Join(fallos...)
}

// recurrentesPendientes genera las ocurrencias que tocan y avisa de las que
// quedan sin resolver.
//
// Dos pasos en una regla porque son la misma idea: primero se ponen al dia las
// ocurrencias (el indice unico impide repetirlas), y despues se avisa de las
// que estan pendientes. Si el usuario ya confirmo el arriendo, no hay aviso.
func (g *Generador) recurrentesPendientes(ctx context.Context, d Destinatario, ahora time.Time) (int, error) {
	if g.recurrentes == nil {
		return 0, nil
	}

	hoy := ahora.In(zonaColombia)

	plantillas, err := g.recurrentes.Listar(ctx, d.ID)
	if err != nil {
		return 0, err
	}

	var fallos []error
	for _, r := range plantillas {
		if _, err := g.recurrentes.Generar(ctx, r, d.ID, hoy); err != nil {
			fallos = append(fallos, err)
		}
	}

	pendientes, err := g.recurrentes.Pendientes(ctx, d.ID)
	if err != nil {
		return 0, errors.Join(append(fallos, err)...)
	}

	var creados int
	for _, o := range pendientes {
		verbo := "pagaste"
		if o.Tipo == movimientos.TipoRecibi {
			verbo = "recibiste"
		}

		titulo := fmt.Sprintf("¿Ya %s %s?", verbo, o.Descripcion)
		base := fmt.Sprintf("Tocaba el %s: %s por %s con %s. Confírmalo si ya pasó, o córrelo si este mes fue distinto.",
			diaEnEspanol(o.Fecha), o.Descripcion, pesos(o.Monto), o.MedioPagoNombre)

		n, err := g.guardar(ctx, d, Nuevo{
			UsuarioID: d.ID,
			Tipo:      TipoRecurrentePendiente,
			// El id de la ocurrencia: una por recurrente y fecha, asi que el
			// aviso tampoco se repite.
			Clave:  fmt.Sprint(o.ID),
			Titulo: titulo,
			Cuerpo: base,
		})
		creados += n
		if err != nil {
			fallos = append(fallos, err)
		}
	}
	return creados, errors.Join(fallos...)
}

// redactorPara devuelve el modelo solo si el plan de esa cuenta incluye IA.
// Sin IA el aviso sale igual, con el texto que arma la app: las cifras son
// las mismas, solo cambia la prosa. Lo que no se cobra no se paga.
func (g *Generador) redactorPara(d Destinatario) Redactor {
	if !d.ConIA {
		return nil
	}
	return g.redactor
}

// guardar crea el aviso si todavia no existe y devuelve cuantos creo (0 o 1).
//
// Primero pregunta si ya esta: la tarea corre cada hora y vuelve a evaluarlo
// todo, y redactar con el modelo un aviso que ya existe es pagar por nada.
// Solo si es nuevo se le pasa el texto base al modelo (si la cuenta tiene IA).
//
// "Ya existía" no es un error: es la tarea corriendo otra vez. El indice
// unico sigue siendo la regla de verdad: si dos corridas se cruzan, el
// ON CONFLICT descarta la segunda.
func (g *Generador) guardar(ctx context.Context, d Destinatario, nuevo Nuevo) (int, error) {
	existe, err := g.store.Existe(ctx, nuevo.UsuarioID, nuevo.Tipo, nuevo.Clave)
	if err != nil {
		return 0, err
	}
	if existe {
		return 0, nil
	}

	nuevo.Cuerpo = redactarSeguro(ctx, g.redactorPara(d), nuevo.Cuerpo)

	_, err = g.store.Guardar(ctx, nuevo)
	if errors.Is(err, ErrYaExiste) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	g.empujar(ctx, nuevo)
	return 1, nil
}

// empujar manda el aviso ademas al celular.
//
// NO propaga el error a proposito: el aviso YA quedo guardado y el usuario lo
// va a ver en la campana. Que el servicio de push de Google este caido no puede
// hacer que la tarea marque como fallida la generacion de un aviso que si se
// creo — y menos que se reintente y se duplique.
func (g *Generador) empujar(ctx context.Context, nuevo Nuevo) {
	if !g.notificador.Habilitado() {
		return
	}

	_, err := g.notificador.Avisar(ctx, nuevo.UsuarioID, push.Mensaje{
		Titulo: nuevo.Titulo,
		Cuerpo: nuevo.Cuerpo,
		URL:    rutaDelAviso(nuevo.Tipo),
		// La etiqueta agrupa por tipo: un resumen semanal nuevo reemplaza al
		// de la semana pasada en la bandeja del celular en vez de apilarse.
		// Los de cuota y cobro llevan su clave porque son de deudas distintas
		// y cada uno importa por separado.
		Etiqueta: etiquetaDelAviso(nuevo),
	})
	if err != nil {
		slog.Warn("no se pudo mandar el push de un aviso",
			"tipo", nuevo.Tipo, "usuario", nuevo.UsuarioID, "error", err)
	}
}

// rutaDelAviso es a donde lleva el clic en la notificacion del celular.
// Abrir la app en la pantalla equivocada es casi tan malo como no avisar.
func rutaDelAviso(tipo string) string {
	switch tipo {
	case TipoCobrosDelMes:
		return "/admin"
	case TipoRecurrentePendiente:
		return "/recurrentes"
	case TipoResumenSemanal:
		return "/"
	default:
		// Cobros, pagos, cuotas y deudas: todos terminan en la misma lista,
		// que es donde se registra el abono.
		return "/movimientos"
	}
}

func etiquetaDelAviso(nuevo Nuevo) string {
	switch nuevo.Tipo {
	case TipoCobroDelDia, TipoPagoDelDia, TipoCuotaVencida, TipoRecurrentePendiente:
		return nuevo.Tipo + ":" + nuevo.Clave
	default:
		return nuevo.Tipo
	}
}

// --------------------------------------------------------------------------

const (
	rolAdmin            = "admin"
	formatoFecha        = "2006-01-02"
	formatoMes          = "2006-01"
	diaDelAvisoDeCobros = 5
)

// semanaAnterior devuelve el lunes y el domingo de la semana pasada, con su
// clave ISO ("2026-W38").
//
// La clave sale de ISOWeek y no de una cuenta propia: en los cambios de año la
// semana 1 puede empezar en diciembre, y una clave mal calculada ahi significa
// un resumen repetido o uno que nunca llega.
func semanaAnterior(ahora time.Time) (desde, hasta time.Time, clave string) {
	// En Go el domingo es 0; para nosotros la semana empieza el lunes.
	diasDesdeLunes := (int(ahora.Weekday()) + 6) % 7

	lunesDeEstaSemana := time.Date(ahora.Year(), ahora.Month(), ahora.Day(), 0, 0, 0, 0, ahora.Location()).
		AddDate(0, 0, -diasDesdeLunes)

	desde = lunesDeEstaSemana.AddDate(0, 0, -7)
	hasta = desde.AddDate(0, 0, 6)

	anio, semana := desde.ISOWeek()
	return desde, hasta, fmt.Sprintf("%d-W%02d", anio, semana)
}

// zonaColombia es la hora de Colombia: UTC-5 todo el año. Fija, sin
// time.LoadLocation, porque la imagen de Docker no trae la base de zonas.
var zonaColombia = time.FixedZone("COT", -5*60*60)

// diaEnEspanol: "2026-09-15" -> "15 de septiembre".
func diaEnEspanol(iso string) string {
	t, err := time.Parse(formatoFecha, iso)
	if err != nil {
		return iso
	}
	return fmt.Sprintf("%d de %s", t.Day(), meses[t.Month()-1])
}

var meses = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio",
	"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}

// rangoEnEspanol: "8 al 14 de septiembre" y, si el rango cruza de mes,
// "29 de septiembre al 5 de octubre".
func rangoEnEspanol(desde, hasta time.Time) string {
	if desde.Month() == hasta.Month() {
		return fmt.Sprintf("%d al %d de %s", desde.Day(), hasta.Day(), meses[desde.Month()-1])
	}
	return fmt.Sprintf("%d de %s al %d de %s",
		desde.Day(), meses[desde.Month()-1], hasta.Day(), meses[hasta.Month()-1])
}

// pesos da formato de plata colombiana a un monto que viene como texto de
// Postgres ("45000.00" -> "$45.000").
//
// Formatea, no calcula: no convierte a float ni suma nada. Lo unico que hace
// es mover los digitos que ya venian.
func pesos(monto string) string {
	entero, decimales, _ := strings.Cut(strings.TrimSpace(monto), ".")
	entero = strings.TrimPrefix(entero, "-")

	var partes []string
	for len(entero) > 3 {
		partes = append([]string{entero[len(entero)-3:]}, partes...)
		entero = entero[:len(entero)-3]
	}
	if entero != "" {
		partes = append([]string{entero}, partes...)
	}

	texto := "$" + strings.Join(partes, ".")
	// Los centavos solo se muestran si los hay: en pesos casi nunca se usan.
	if decimales != "" && strings.Trim(decimales, "0") != "" {
		texto += "," + decimales
	}
	if strings.HasPrefix(strings.TrimSpace(monto), "-") {
		texto = "-" + texto
	}
	return texto
}

func esCero(monto string) bool {
	return strings.Trim(strings.NewReplacer(".", "", "-", "", " ", "").Replace(monto), "0") == ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func nombresDe(pendientes []suscripciones.Pendiente) string {
	nombres := make([]string, 0, len(pendientes))
	for _, p := range pendientes {
		nombre := p.Nombre
		if nombre == "" {
			nombre = p.Email
		}
		nombres = append(nombres, nombre)
	}
	return strings.Join(nombres, ", ")
}
