package dinero

import (
	"errors"
	"testing"
)

// El monto es lo más delicado de toda la app: un error aquí le cuesta plata
// al cliente. Por eso es lo primero que se prueba.
func TestNormalizar(t *testing.T) {
	casos := []struct {
		nombre   string
		entrada  string
		esperado string
		err      error
	}{
		// Válidos
		{"entero simple", "1500", "1500", nil},
		{"con decimales", "1500.50", "1500.50", nil},
		{"un decimal", "1500.5", "1500.5", nil},
		{"un peso", "1", "1", nil},
		{"espacios alrededor", "  2000  ", "2000", nil},
		{"separadores de miles con coma", "1,500,000", "1500000", nil},
		{"coma y decimales", "1,500,000.75", "1500000.75", nil},
		{"doce enteros (el máximo)", "999999999999", "999999999999", nil},
		{"centavos solos", "0.01", "0.01", nil},

		// Formato inválido
		{"vacío", "", "", ErrFormato},
		{"solo espacios", "   ", "", ErrFormato},
		{"letras", "abc", "", ErrFormato},
		{"negativo", "-500", "", ErrFormato},
		{"tres decimales", "1500.555", "", ErrFormato},
		{"trece enteros", "1234567890123", "", ErrFormato},
		{"punto colgando", "1500.", "", ErrFormato},
		{"dos puntos", "1.500.000", "", ErrFormato},
		{"notación científica", "1e5", "", ErrFormato},
		{"suma", "100+100", "", ErrFormato},

		// Cero en todas sus formas: es válido como número pero no como monto.
		{"cero", "0", "", ErrCero},
		{"cero con decimales", "0.00", "", ErrCero},
		{"cero con un decimal", "0.0", "", ErrCero},
		{"ceros a la izquierda", "000", "", ErrCero},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			obtenido, err := Normalizar(c.entrada)

			if !errors.Is(err, c.err) {
				t.Fatalf("Normalizar(%q) devolvió error %v, se esperaba %v", c.entrada, err, c.err)
			}
			if obtenido != c.esperado {
				t.Errorf("Normalizar(%q) = %q, se esperaba %q", c.entrada, obtenido, c.esperado)
			}
		})
	}
}

// Un monto con ceros a la izquierda debe quedar igual que sin ellos:
// si no, "007" y "7" se guardarían distinto y los filtros fallarían.
func TestNormalizarNoPierdeValor(t *testing.T) {
	casos := map[string]string{
		"0000001":  "1",
		"0100":     "100",
		"00100.50": "100.50",
	}

	for entrada, esperado := range casos {
		obtenido, err := Normalizar(entrada)
		if err != nil {
			t.Fatalf("Normalizar(%q) falló: %v", entrada, err)
		}
		if obtenido != esperado {
			t.Errorf("Normalizar(%q) = %q, se esperaba %q", entrada, obtenido, esperado)
		}
	}
}

func TestFormatear(t *testing.T) {
	casos := map[string]string{
		"0":              "$ 0",
		"0.00":           "$ 0",
		"-0.00":          "$ 0",
		"5":              "$ 5",
		"999":            "$ 999",
		"1000":           "$ 1.000",
		"18000.00":       "$ 18.000",
		"1234567.5":      "$ 1.234.567,50",
		"1234567.05":     "$ 1.234.567,05",
		"-250000.00":     "-$ 250.000",
		"999999999999":   "$ 999.999.999.999",
		"no es un monto": "no es un monto",
		"12a4":           "12a4",
	}
	for entrada, esperado := range casos {
		if obtenido := Formatear(entrada); obtenido != esperado {
			t.Errorf("Formatear(%q) = %q, se esperaba %q", entrada, obtenido, esperado)
		}
	}
}
