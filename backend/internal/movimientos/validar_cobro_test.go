package movimientos

import "testing"

func TestValidarFechaDeCobro(t *testing.T) {
	base := Entrada{
		CategoriaID: 1, MedioPagoID: 1, Tipo: TipoPreste, Monto: "1000",
		Fecha: "2026-09-10", AQuien: "Ana", Estado: EstadoPendiente,
	}

	casos := []struct {
		nombre   string
		cambiar  func(*Entrada)
		errCampo bool
		cobrarEl string
	}{
		{"sin fecha es válido", func(e *Entrada) {}, false, ""},
		{"con fecha", func(e *Entrada) { e.CobrarEl = "2026-09-20" }, false, "2026-09-20"},
		{"el mismo día", func(e *Entrada) { e.CobrarEl = "2026-09-10" }, false, "2026-09-10"},
		{"antes de prestar", func(e *Entrada) { e.CobrarEl = "2026-09-09" }, true, ""},
		{"fecha basura", func(e *Entrada) { e.CobrarEl = "20/09/2026" }, true, ""},
		{"en un gasto se ignora", func(e *Entrada) { e.Tipo = TipoPague; e.CobrarEl = "2026-09-20" }, false, ""},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			entrada := base
			c.cambiar(&entrada)
			datos, campos := Validar(entrada)

			if _, hay := campos["cobrar_el"]; hay != c.errCampo {
				t.Fatalf("error en cobrar_el = %v, se esperaba %v (%v)", hay, c.errCampo, campos)
			}
			if c.errCampo {
				return
			}
			obtenido := ""
			if datos.CobrarEl != nil {
				obtenido = *datos.CobrarEl
			}
			if obtenido != c.cobrarEl {
				t.Errorf("cobrar_el = %q, se esperaba %q", obtenido, c.cobrarEl)
			}
		})
	}
}
