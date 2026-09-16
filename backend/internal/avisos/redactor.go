package avisos

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// Redactor convierte un aviso armado por la app en uno que se lea como algo
// que diria una persona. Es una interfaz de un solo metodo, y opcional: si el
// servidor no tiene modelo configurado, el aviso sale con su texto base y no
// pasa nada.
type Redactor interface {
	Redactar(ctx context.Context, instrucciones, datos string) (string, error)
}

// instruccionesRedaccion es lo unico que se le pide al modelo en este paquete.
// Fijate en lo que NO se le pide: ninguna cuenta. Los numeros ya vienen
// hechos; su trabajo es la prosa.
const instruccionesRedaccion = `Eres el asistente de una app de finanzas personales.
Te paso un aviso ya redactado por el sistema. Reescribelo en una o dos frases
naturales, tuteando, en español de Colombia, para que se lea como algo que
diria una persona y no un reporte.

REGLA ABSOLUTA: usa EXACTAMENTE las mismas cifras, fechas y nombres que te
paso. No sumes, no restes, no redondees, no estimes, no agregues numeros que
no esten ahi (ni porcentajes ni comparaciones con otras semanas). Si te falta
un dato para decir algo, no lo digas.

No saludes, no te despidas, no uses vinetas ni titulos. Solo el parrafo.`

// redactarSeguro pide la version del modelo y la acepta SOLO si no trae
// ninguna cifra que no estuviera en el texto base.
//
// Es una guardia barata contra el unico error que de verdad importa aqui: que
// el aviso diga un numero que no es. El modelo puede darle la vuelta a la
// frase todo lo que quiera, pero si aparece un "20% mas que la semana pasada"
// —una cuenta que nadie hizo— su version se descarta y sale el texto base.
//
// Es deliberadamente estricta: "45 mil" tambien se rechaza, porque 45 no es
// ninguna de las cifras que le dimos. Preferimos un aviso mas seco a uno
// bonito con un numero inventado.
func redactarSeguro(ctx context.Context, redactor Redactor, base string) string {
	if redactor == nil {
		return base
	}

	redactado, err := redactor.Redactar(ctx, instruccionesRedaccion, base)
	if err != nil {
		// Que el modelo falle no puede dejar al usuario sin aviso.
		slog.Warn("no se pudo redactar el aviso: se usa el texto base", "error", err)
		return base
	}

	redactado = strings.TrimSpace(redactado)
	if redactado == "" {
		return base
	}

	if nuevas := cifrasNuevas(base, redactado); len(nuevas) > 0 {
		slog.Warn("el modelo inventó cifras en un aviso: se usa el texto base",
			"cifras", nuevas, "texto", redactado)
		return base
	}

	return redactado
}

// numeros atrapa cualquier cifra, con o sin separadores: 45000, 45.000, 1,5.
var numeros = regexp.MustCompile(`\d[\d.,]*`)

// cifrasNuevas devuelve las del texto redactado que no estaban en el original.
//
// Se comparan sin separadores, asi "45000" y "45.000" son la misma cifra: el
// modelo puede darle formato, que para eso se lee mejor.
func cifrasNuevas(base, redactado string) []string {
	conocidas := map[string]bool{}
	for _, n := range numeros.FindAllString(base, -1) {
		conocidas[soloDigitos(n)] = true
	}

	var nuevas []string
	for _, n := range numeros.FindAllString(redactado, -1) {
		if limpio := soloDigitos(n); limpio != "" && !conocidas[limpio] {
			nuevas = append(nuevas, n)
		}
	}
	return nuevas
}

func soloDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.TrimLeft(b.String(), "0")
}
