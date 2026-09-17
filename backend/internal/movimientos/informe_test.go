package movimientos

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
	"time"
)

// Un informe largo tiene que partir en páginas sin romperse, y un texto con
// emojis o letras fuera de cp1252 no puede tumbar el PDF.
func TestPDFLargoYConTextoRaro(t *testing.T) {
	inf := &Informe{
		Titular:  "José Ñúñez",
		Desde:    "2026-01-01",
		Hasta:    "2026-12-31",
		Generado: time.Date(2026, 9, 17, 10, 0, 0, 0, zonaColombia),
		Totales:  Totales{Recibido: "1.00", Pagado: "0", PorCobrar: "0", Recuperado: "0", Balance: "1.00"},
	}
	for i := range 300 {
		inf.Movimientos = append(inf.Movimientos, Movimiento{
			Tipo: TipoPague, Monto: "12345.67", Fecha: "2026-03-15",
			CategoriaNombre: "Categoría con un nombre bastante largo",
			Descripcion:     fmt.Sprintf("Almuerzo 🍔 número %d con una descripción larguísima que no cabe en la columna — 中文", i),
		})
	}

	var pdf bytes.Buffer
	if err := generarPDF(inf, &pdf); err != nil {
		t.Fatalf("generando: %v", err)
	}
	// 300 filas a ~40 por página: varias páginas.
	paginas := len(regexp.MustCompile(`/Type /Page\b`).FindAll(pdf.Bytes(), -1))
	if paginas < 5 {
		t.Errorf("páginas = %d, se esperaban varias", paginas)
	}

	var xlsx bytes.Buffer
	if err := generarExcel(inf, &xlsx); err != nil {
		t.Fatalf("generando Excel: %v", err)
	}
}

func TestFechaCorta(t *testing.T) {
	if f := fechaCorta("2026-09-05"); f != "05/09/2026" {
		t.Errorf("fechaCorta = %q", f)
	}
	if f := fechaCorta("raro"); f != "raro" {
		t.Errorf("fechaCorta con basura = %q", f)
	}
}
