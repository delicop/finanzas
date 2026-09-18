package recurrentes_test

import (
	"testing"
	"time"

	"finanzas/internal/recurrentes"
)

// El calendario es lo único del paquete con lógica de verdad, y es donde se
// esconden los errores clásicos: el arriendo "los 31" que se salta febrero, la
// quincena que cae en el mes siguiente, el año bisiesto.
//
// Va sin base de datos a propósito: es una función pura y se puede probar con
// fechas concretas, que es la única forma de estar seguro de estas reglas.

func dia(iso string) time.Time {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		panic(err)
	}
	return t
}

func fechas(lista []time.Time) []string {
	salida := make([]string, 0, len(lista))
	for _, f := range lista {
		salida = append(salida, f.Format("2006-01-02"))
	}
	return salida
}

func iguales(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestVencimientos(t *testing.T) {
	casos := []struct {
		nombre     string
		frecuencia string
		diaDelMes  int
		desdeR     string // desde cuándo aplica el recurrente
		hastaR     string // hasta cuándo (vacío = sin fin)
		desde      string // ventana consultada
		hasta      string
		esperado   []string
	}{
		{
			nombre: "mensual normal", frecuencia: recurrentes.Mensual, diaDelMes: 5,
			desdeR: "2026-01-01", desde: "2026-03-01", hasta: "2026-05-31",
			esperado: []string{"2026-03-05", "2026-04-05", "2026-05-05"},
		},
		{
			// El caso que rompe una implementación ingenua: time.Date con el
			// día 31 en febrero desborda al mes siguiente en silencio.
			nombre: "el dia 31 cae en el ultimo dia de cada mes", frecuencia: recurrentes.Mensual, diaDelMes: 31,
			desdeR: "2026-01-01", desde: "2026-01-01", hasta: "2026-04-30",
			esperado: []string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30"},
		},
		{
			nombre: "febrero de anio bisiesto", frecuencia: recurrentes.Mensual, diaDelMes: 30,
			desdeR: "2024-01-01", desde: "2024-02-01", hasta: "2024-02-29",
			esperado: []string{"2024-02-29"},
		},
		{
			// La segunda quincena cae 15 días después, aunque eso la pase al
			// mes siguiente.
			nombre: "quincenal desde el 20", frecuencia: recurrentes.Quincenal, diaDelMes: 20,
			desdeR: "2026-01-01", desde: "2026-03-01", hasta: "2026-04-10",
			esperado: []string{"2026-03-07", "2026-03-20", "2026-04-04"},
		},
		{
			nombre: "quincenal del 1", frecuencia: recurrentes.Quincenal, diaDelMes: 1,
			desdeR: "2026-01-01", desde: "2026-03-01", hasta: "2026-03-31",
			esperado: []string{"2026-03-01", "2026-03-16"},
		},
		{
			// 1 = lunes. En marzo de 2026 los lunes son 2, 9, 16, 23 y 30.
			nombre: "semanal los lunes", frecuencia: recurrentes.Semanal, diaDelMes: 1,
			desdeR: "2026-01-01", desde: "2026-03-01", hasta: "2026-03-20",
			esperado: []string{"2026-03-02", "2026-03-09", "2026-03-16"},
		},
		{
			nombre: "no empieza antes de su fecha de inicio", frecuencia: recurrentes.Mensual, diaDelMes: 10,
			desdeR: "2026-03-15", desde: "2026-01-01", hasta: "2026-05-31",
			esperado: []string{"2026-04-10", "2026-05-10"},
		},
		{
			nombre: "no sigue despues de su fecha de fin", frecuencia: recurrentes.Mensual, diaDelMes: 10,
			desdeR: "2026-01-01", hastaR: "2026-04-30", desde: "2026-03-01", hasta: "2026-06-30",
			esperado: []string{"2026-03-10", "2026-04-10"},
		},
		{
			nombre: "ventana que termina antes de empezar", frecuencia: recurrentes.Mensual, diaDelMes: 10,
			desdeR: "2026-01-01", desde: "2026-05-01", hasta: "2026-04-01",
			esperado: nil,
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			r := recurrentes.Recurrente{
				Frecuencia: c.frecuencia,
				Dia:        c.diaDelMes,
				Desde:      c.desdeR,
				Activo:     true,
			}
			if c.hastaR != "" {
				r.Hasta = &c.hastaR
			}

			obtenido := fechas(recurrentes.Vencimientos(r, dia(c.desde), dia(c.hasta)))
			if !iguales(obtenido, c.esperado) {
				t.Errorf("vencimientos = %v, se esperaba %v", obtenido, c.esperado)
			}
		})
	}
}

// Un recurrente pausado no vuelve a tocar: es toda la gracia de pausarlo en
// vez de borrarlo.
func TestProximoVencimiento(t *testing.T) {
	r := recurrentes.Recurrente{
		Frecuencia: recurrentes.Mensual, Dia: 5, Desde: "2026-01-01", Activo: true,
	}

	proxima := recurrentes.ProximoVencimiento(r, dia("2026-03-10"))
	if proxima == nil || *proxima != "2026-04-05" {
		t.Errorf("proxima = %v, se esperaba 2026-04-05", proxima)
	}

	// El mismo día cuenta: si hoy es 5, hoy toca.
	proxima = recurrentes.ProximoVencimiento(r, dia("2026-03-05"))
	if proxima == nil || *proxima != "2026-03-05" {
		t.Errorf("proxima = %v, se esperaba 2026-03-05", proxima)
	}

	r.Activo = false
	if proxima := recurrentes.ProximoVencimiento(r, dia("2026-03-10")); proxima != nil {
		t.Errorf("un recurrente pausado no tiene próxima fecha: %v", *proxima)
	}

	r.Activo = true
	fin := "2026-02-28"
	r.Hasta = &fin
	if proxima := recurrentes.ProximoVencimiento(r, dia("2026-03-10")); proxima != nil {
		t.Errorf("un recurrente vencido no tiene próxima fecha: %v", *proxima)
	}
}

func TestDiaValido(t *testing.T) {
	casos := []struct {
		frecuencia string
		dia        int
		valido     bool
	}{
		{recurrentes.Mensual, 1, true},
		{recurrentes.Mensual, 31, true},
		{recurrentes.Mensual, 0, false},
		{recurrentes.Mensual, 32, false},
		{recurrentes.Semanal, 1, true},
		{recurrentes.Semanal, 7, true},
		{recurrentes.Semanal, 8, false},
		{recurrentes.Semanal, 23, false},
	}

	for _, c := range casos {
		if recurrentes.DiaValido(c.frecuencia, c.dia) != c.valido {
			t.Errorf("DiaValido(%q, %d) debería ser %v", c.frecuencia, c.dia, c.valido)
		}
	}
}
