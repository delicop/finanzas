package tiendas

import "testing"

// Las pruebas de este archivo no tocan la base: fraseRango y
// nombreArchivoCierres son texto puro a partir de un filtro.
func TestFraseRango(t *testing.T) {
	casos := []struct {
		nombre       string
		desde, hasta string
		frase        string
	}{
		{"ninguno", "", "", ""},
		{"los dos", "2026-09-01", "2026-09-15", "Del 01/09/2026 al 15/09/2026"},
		{"solo desde", "2026-09-01", "", "Desde el 01/09/2026"},
		{"solo hasta", "", "2026-09-15", "Hasta el 15/09/2026"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := fraseRango(FiltrosCierres{Desde: c.desde, Hasta: c.hasta})
			if got != c.frase {
				t.Errorf("fraseRango(%q, %q) = %q, se esperaba %q", c.desde, c.hasta, got, c.frase)
			}
		})
	}
}

func TestNombreArchivoCierres(t *testing.T) {
	casos := []struct {
		nombre       string
		desde, hasta string
		esperado     string
	}{
		{"los dos", "2026-09-01", "2026-09-15", "cierres-2026-09-01-2026-09-15"},
		{"solo desde", "2026-09-01", "", "cierres-desde-2026-09-01"},
		{"solo hasta", "", "2026-09-15", "cierres-hasta-2026-09-15"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := nombreArchivoCierres(FiltrosCierres{Desde: c.desde, Hasta: c.hasta})
			if got != c.esperado {
				t.Errorf("nombreArchivoCierres(%q, %q) = %q, se esperaba %q", c.desde, c.hasta, got, c.esperado)
			}
		})
	}

	// Sin ningun filtro: lleva la fecha de hoy, para no pisar la descarga
	// anterior. No comparamos la fecha exacta (dependeria de la zona
	// horaria del momento en que corre la prueba), solo el prefijo.
	t.Run("sin filtros", func(t *testing.T) {
		got := nombreArchivoCierres(FiltrosCierres{})
		if len(got) != len("cierres-2026-09-01") || got[:8] != "cierres-" {
			t.Errorf("nombreArchivoCierres sin filtros = %q, no tiene la forma esperada", got)
		}
	})
}
