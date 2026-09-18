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

// Repartir es lo que arma las cuotas de un acuerdo de pago. La regla que no se
// puede romper: las partes tienen que SUMAR EXACTAMENTE el total. Si se pierde
// un peso por cuota, un acuerdo a 24 cuotas deja al final una deuda fantasma
// de 24 pesos que nadie sabe de dónde salió.
func TestRepartir(t *testing.T) {
	casos := []struct {
		nombre   string
		total    string
		cuotas   int
		esperado []string
	}{
		{"division exacta", "300000", 3, []string{"100000.00", "100000.00", "100000.00"}},
		{
			// 100.000 entre 3 son 33.333,33 y sobra un centavo: se lo lleva la
			// primera. 33333.34 + 33333.33 + 33333.33 = 100000.00
			"con centavos que sobran", "100000", 3,
			[]string{"33333.34", "33333.33", "33333.33"},
		},
		{
			// Sobran 2 centavos: uno para cada una de las dos primeras.
			"dos centavos de sobra", "100", 3,
			[]string{"33.34", "33.33", "33.33"},
		},
		{"una sola cuota", "45000.50", 1, []string{"45000.50"}},
		{"con decimales", "1000.05", 2, []string{"500.03", "500.02"}},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			partes, err := Repartir(c.total, c.cuotas)
			if err != nil {
				t.Fatalf("Repartir(%q, %d): %v", c.total, c.cuotas, err)
			}
			if len(partes) != len(c.esperado) {
				t.Fatalf("partes = %v, se esperaban %d", partes, len(c.esperado))
			}
			for i := range partes {
				if partes[i] != c.esperado[i] {
					t.Errorf("cuota %d = %s, se esperaba %s", i+1, partes[i], c.esperado[i])
				}
			}
		})
	}
}

// La propiedad que de verdad importa, comprobada sobre muchos casos: sumar las
// partes en centavos tiene que dar el total, siempre.
func TestRepartirSiempreCuadra(t *testing.T) {
	totales := []string{"1", "7", "100", "999.99", "45000.50", "1234567.89", "10.01"}
	cantidades := []int{1, 2, 3, 4, 5, 7, 12, 24}

	for _, total := range totales {
		for _, n := range cantidades {
			partes, err := Repartir(total, n)
			if err != nil {
				continue // el total no alcanza para tantas cuotas: es correcto
			}

			var suma int64
			for _, p := range partes {
				c, err := aCentavos(p)
				if err != nil {
					t.Fatalf("la parte %q no se pudo leer: %v", p, err)
				}
				suma += c
			}

			normalizado, _ := Normalizar(total)
			esperado, _ := aCentavos(normalizado)
			if suma != esperado {
				t.Errorf("Repartir(%q, %d) suma %d centavos, se esperaban %d", total, n, suma, esperado)
			}
		}
	}
}

func TestRepartirRechazaLoImposible(t *testing.T) {
	// 5 pesos son 500 centavos: no alcanzan para 600 cuotas sin que alguna
	// quede en cero, y una cuota de cero no es una cuota.
	if _, err := Repartir("5", 600); !errors.Is(err, ErrReparto) {
		t.Errorf("Repartir(5, 600) = %v, se esperaba ErrReparto", err)
	}
	if _, err := Repartir("1000", 0); !errors.Is(err, ErrReparto) {
		t.Errorf("Repartir con 0 cuotas debería fallar")
	}
	if _, err := Repartir("abc", 3); err == nil {
		t.Errorf("Repartir con un monto inválido debería fallar")
	}
}
