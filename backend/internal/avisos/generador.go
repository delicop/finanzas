package avisos

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"finanzas/internal/suscripciones"
)

// Generador decide que avisos toca crear. Lo llama una tarea de fondo cada
// pocas horas; volver a evaluarlo todo es barato y, como cada aviso lleva su
// clave de periodo, repetir una corrida no repite un aviso.
type Generador struct {
	store    *Store
	negocio  *suscripciones.Store
	redactor Redactor // opcional: sin modelo configurado, es nil
}

func NuevoGenerador(store *Store, negocio *suscripciones.Store, redactor Redactor) *Generador {
	return &Generador{store: store, negocio: negocio, redactor: redactor}
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
		g.cobrosDelMes,
		g.cobrosDelDia,
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
	prestamos, err := g.store.PrestamosViejos(ctx, d.ID, DiasPrestamoViejo)
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

		var titulo, base string
		if c.CobrarEl == hoy.Format(formatoFecha) {
			titulo = fmt.Sprintf("Hoy te paga %s", quien)
			base = fmt.Sprintf("Hoy es el día en que %s quedó de devolverte %s", quien, pesos(c.Monto))
		} else {
			titulo = fmt.Sprintf("%s quedó de pagarte el %s", quien, diaEnEspanol(c.CobrarEl))
			base = fmt.Sprintf("El %s era el día en que %s quedó de devolverte %s, y sigue pendiente",
				diaEnEspanol(c.CobrarEl), quien, pesos(c.Monto))
		}
		base += fmt.Sprintf(" (se lo prestaste el %s", diaEnEspanol(c.Fecha))
		if desc := strings.TrimSpace(c.Descripcion); desc != "" {
			base += ": " + desc
		}
		base += "). Cuando te pague, márcalo como pagado en Movimientos."

		n, err := g.guardar(ctx, d, Nuevo{
			UsuarioID: d.ID,
			Tipo:      TipoCobroDelDia,
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
	return 1, nil
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
