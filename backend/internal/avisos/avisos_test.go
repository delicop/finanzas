package avisos

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Estas pruebas no necesitan base de datos: son las reglas de texto y de
// fechas, que es donde se cuelan los errores tontos.

func TestPesos(t *testing.T) {
	casos := map[string]string{
		"0.00":        "$0",
		"450.00":      "$450",
		"45000.00":    "$45.000",
		"1500000.50":  "$1.500.000,50",
		"900000":      "$900.000",
		"-45000.00":   "-$45.000",
		"12345678.00": "$12.345.678",
	}

	for monto, esperado := range casos {
		if dio := pesos(monto); dio != esperado {
			t.Errorf("pesos(%q) = %q, se esperaba %q", monto, dio, esperado)
		}
	}
}

// La clave de la semana sale de ISOWeek y no de una cuenta propia: en los
// cambios de año, calcularla a mano significa un resumen repetido o uno que
// nunca llega.
func TestSemanaAnterior(t *testing.T) {
	casos := []struct {
		nombre string
		ahora  string
		desde  string
		hasta  string
		clave  string
	}{
		{"un miércoles", "2026-09-16", "2026-09-07", "2026-09-13", "2026-W37"},
		{"un lunes", "2026-09-14", "2026-09-07", "2026-09-13", "2026-W37"},
		{"un domingo", "2026-09-20", "2026-09-07", "2026-09-13", "2026-W37"},
		// El 1 de enero de 2027 cae jueves: la semana pasada es todavía de 2026.
		{"cambio de año", "2027-01-01", "2026-12-21", "2026-12-27", "2026-W52"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			ahora, err := time.Parse(formatoFecha, caso.ahora)
			if err != nil {
				t.Fatalf("fecha de prueba inválida: %v", err)
			}

			desde, hasta, clave := semanaAnterior(ahora)

			if desde.Format(formatoFecha) != caso.desde || hasta.Format(formatoFecha) != caso.hasta {
				t.Errorf("rango = %s a %s, se esperaba %s a %s",
					desde.Format(formatoFecha), hasta.Format(formatoFecha), caso.desde, caso.hasta)
			}
			if clave != caso.clave {
				t.Errorf("clave = %q, se esperaba %q", clave, caso.clave)
			}
		})
	}
}

func TestRangoEnEspanol(t *testing.T) {
	desde, _ := time.Parse(formatoFecha, "2026-09-07")
	hasta, _ := time.Parse(formatoFecha, "2026-09-13")
	if dio := rangoEnEspanol(desde, hasta); dio != "7 al 13 de septiembre" {
		t.Errorf("rango = %q", dio)
	}

	desde, _ = time.Parse(formatoFecha, "2026-09-28")
	hasta, _ = time.Parse(formatoFecha, "2026-10-04")
	if dio := rangoEnEspanol(desde, hasta); dio != "28 de septiembre al 4 de octubre" {
		t.Errorf("rango que cruza de mes = %q", dio)
	}
}

// --------------------------------------------------------------------------
// La guardia: el modelo redacta, pero no inventa cifras
// --------------------------------------------------------------------------

type redactorFalso struct {
	respuesta string
	err       error
	llamadas  int
}

func (r *redactorFalso) Redactar(ctx context.Context, instrucciones, datos string) (string, error) {
	r.llamadas++
	if r.err != nil {
		return "", r.err
	}
	return r.respuesta, nil
}

const textoBase = "Del 7 al 13 de septiembre registraste 4 movimientos: recibiste $900.000 y pagaste $45.000."

func TestElModeloPuedeRedactarPeroNoInventarCifras(t *testing.T) {
	casos := []struct {
		nombre    string
		respuesta string
		aceptada  bool
	}{
		{
			nombre:    "reescribe con las mismas cifras",
			respuesta: "Esta semana te entraron $900.000 y se te fueron $45.000 en 4 movimientos.",
			aceptada:  true,
		},
		{
			nombre: "puede cambiarles el formato",
			// 900000 sin puntos es la misma cifra: formatear no es inventar.
			respuesta: "Recibiste 900000 y pagaste 45000 en 4 movimientos, del 7 al 13 de septiembre.",
			aceptada:  true,
		},
		{
			nombre: "se inventa una comparación",
			// El 20% no lo calculó nadie: es justo el error que importa.
			respuesta: "Gastaste $45.000, un 20% más que la semana pasada.",
			aceptada:  false,
		},
		{
			nombre:    "se inventa un total",
			respuesta: "Te quedaron $855.000 libres esta semana.",
			aceptada:  false,
		},
		{
			nombre:    "aproxima el monto",
			respuesta: "Pagaste como 45 mil esta semana.",
			aceptada:  false,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			redactor := &redactorFalso{respuesta: caso.respuesta}
			dio := redactarSeguro(context.Background(), redactor, textoBase)

			if caso.aceptada && dio != caso.respuesta {
				t.Errorf("se descartó una redacción válida: %q", caso.respuesta)
			}
			if !caso.aceptada && dio != textoBase {
				t.Errorf("se aceptó una cifra inventada: %q", dio)
			}
		})
	}
}

// Que el modelo se caiga no puede dejar al usuario sin aviso: las cifras ya
// estaban calculadas antes de llamarlo.
func TestSinModeloOConModeloCaidoSaleElTextoBase(t *testing.T) {
	if dio := redactarSeguro(context.Background(), nil, textoBase); dio != textoBase {
		t.Errorf("sin redactor = %q", dio)
	}

	caido := &redactorFalso{err: errors.New("el proveedor no respondió")}
	if dio := redactarSeguro(context.Background(), caido, textoBase); dio != textoBase {
		t.Errorf("con el modelo caído = %q", dio)
	}

	vacio := &redactorFalso{respuesta: "   "}
	if dio := redactarSeguro(context.Background(), vacio, textoBase); dio != textoBase {
		t.Errorf("con respuesta vacía = %q", dio)
	}
}
