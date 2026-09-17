package movimientos

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"

	"finanzas/internal/dinero"
)

// Como se le muestran al usuario los valores tecnicos.
var nombresTipo = map[string]string{
	TipoRecibi: "Recibí",
	TipoPague:  "Pagué",
	TipoPreste: "Presté",
}

var nombresEstado = map[string]string{
	EstadoPendiente: "Pendiente",
	EstadoPagado:    "Devuelto",
}

func texto(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// fechaCorta pasa "2026-09-15" a "15/09/2026" sin pasar por time (ver el
// comentario de formatearFecha en el frontend).
func fechaCorta(iso string) string {
	partes := strings.Split(iso, "-")
	if len(partes) != 3 {
		return iso
	}
	return partes[2] + "/" + partes[1] + "/" + partes[0]
}

func (inf *Informe) rango() string {
	return fechaCorta(inf.Desde) + " al " + fechaCorta(inf.Hasta)
}

// filasTotales son las cifras de arriba del informe, en el orden del Resumen.
func (inf *Informe) filasTotales() [][2]string {
	return [][2]string{
		{"Recibido", inf.Totales.Recibido},
		{"Pagado", inf.Totales.Pagado},
		{"Por cobrar (préstamos pendientes)", inf.Totales.PorCobrar},
		{"Recuperado (préstamos devueltos)", inf.Totales.Recuperado},
		{"Balance", inf.Totales.Balance},
	}
}

// ---------------------------------------------------------------- Excel

// generarExcel arma un .xlsx con el resumen arriba y la tabla abajo, con
// filtros y la fila de titulos fija al bajar.
func generarExcel(inf *Informe, destino *bytes.Buffer) error {
	libro := excelize.NewFile()
	defer libro.Close()

	const hoja = "Movimientos"
	if err := libro.SetSheetName("Sheet1", hoja); err != nil {
		return err
	}

	// Aqui SI se convierte el monto a float, y es a proposito: Excel guarda
	// todos sus numeros como float de 64 bits, y una celda de texto no se
	// podria sumar ni graficar. El archivo es para leer; las cuentas que
	// valen siguen siendo las de Postgres, que ya vienen en el resumen.
	numero := func(monto string) float64 {
		n, _ := strconv.ParseFloat(monto, 64)
		return n
	}

	// Formato de pesos: con centavos solo si alguno los tiene, para no llenar
	// la hoja de ",00".
	formatoPesos := `"$" #,##0`
	for _, m := range inf.Movimientos {
		if _, dec, _ := strings.Cut(m.Monto, "."); strings.Trim(dec, "0") != "" {
			formatoPesos = `"$" #,##0.00`
			break
		}
	}

	titulo, err := libro.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	if err != nil {
		return err
	}
	tenue, err := libro.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "667085"}})
	if err != nil {
		return err
	}
	negrita, err := libro.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	pesos, err := libro.NewStyle(&excelize.Style{CustomNumFmt: &formatoPesos})
	if err != nil {
		return err
	}
	pesosNegrita, err := libro.NewStyle(&excelize.Style{CustomNumFmt: &formatoPesos, Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	formatoFecha := "dd/mm/yyyy"
	fecha, err := libro.NewStyle(&excelize.Style{CustomNumFmt: &formatoFecha})
	if err != nil {
		return err
	}
	encabezado, err := libro.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1D4FD8"}},
	})
	if err != nil {
		return err
	}

	celda := func(col, fila int) string {
		nombre, _ := excelize.CoordinatesToCellName(col, fila)
		return nombre
	}
	poner := func(col, fila int, valor any, estilo int) {
		c := celda(col, fila)
		_ = libro.SetCellValue(hoja, c, valor)
		if estilo != 0 {
			_ = libro.SetCellStyle(hoja, c, c, estilo)
		}
	}

	fila := 1
	poner(1, fila, "Movimientos de "+inf.Titular, titulo)
	fila++
	poner(1, fila, "Del "+inf.rango(), 0)
	fila++
	generado := "Generado el " + inf.Generado.Format("02/01/2006 15:04")
	if inf.ConFiltros {
		generado += " · con filtros aplicados"
	}
	poner(1, fila, generado, tenue)
	fila += 2

	for _, t := range inf.filasTotales() {
		estiloNombre, estiloValor := 0, pesos
		if t[0] == "Balance" {
			estiloNombre, estiloValor = negrita, pesosNegrita
		}
		poner(1, fila, t[0], estiloNombre)
		poner(3, fila, numero(t[1]), estiloValor)
		fila++
	}
	fila++

	columnas := []struct {
		titulo string
		ancho  float64
	}{
		{"Fecha", 12}, {"Tipo", 10}, {"Categoría", 18}, {"Descripción", 40},
		{"Medio", 16}, {"Monto", 16}, {"A quién", 18}, {"Estado", 12}, {"Cobrar el", 12}, {"Devuelto por", 16},
	}
	filaTitulos := fila
	for i, c := range columnas {
		poner(i+1, fila, c.titulo, encabezado)
		nombreCol, _ := excelize.ColumnNumberToName(i + 1)
		_ = libro.SetColWidth(hoja, nombreCol, nombreCol, c.ancho)
	}
	fila++

	for _, m := range inf.Movimientos {
		if dia, err := time.Parse(FormatoFecha, m.Fecha); err == nil {
			poner(1, fila, dia, fecha)
		} else {
			poner(1, fila, m.Fecha, 0)
		}
		poner(2, fila, nombresTipo[m.Tipo], 0)
		poner(3, fila, m.CategoriaNombre, 0)
		// SetCellValue con un string guarda TEXTO, nunca una formula: una
		// descripcion que empiece por "=" no se ejecuta al abrir el archivo.
		poner(4, fila, m.Descripcion, 0)
		poner(5, fila, texto(m.MedioPagoNombre), 0)
		poner(6, fila, numero(m.Monto), pesos)
		poner(7, fila, texto(m.AQuien), 0)
		if m.Estado != nil {
			poner(8, fila, nombresEstado[*m.Estado], 0)
		}
		if m.CobrarEl != nil {
			if dia, err := time.Parse(FormatoFecha, *m.CobrarEl); err == nil {
				poner(9, fila, dia, fecha)
			}
		}
		poner(10, fila, texto(m.MedioCobroNombre), 0)
		fila++
	}

	if len(inf.Movimientos) == 0 {
		poner(1, fila, "No hay movimientos en este rango.", tenue)
	} else {
		ultima := celda(len(columnas), fila-1)
		if err := libro.AutoFilter(hoja, celda(1, filaTitulos)+":"+ultima, nil); err != nil {
			return err
		}
	}

	// La fila de titulos queda fija al bajar por la tabla.
	if err := libro.SetPanes(hoja, &excelize.Panes{
		Freeze:      true,
		YSplit:      filaTitulos,
		TopLeftCell: celda(1, filaTitulos+1),
		ActivePane:  "bottomLeft",
	}); err != nil {
		return err
	}

	_, err = libro.WriteTo(destino)
	return err
}

// ---------------------------------------------------------------- PDF

// generarPDF arma un extracto en A4: encabezado con el resumen y la tabla,
// que repite sus titulos en cada pagina.
func generarPDF(inf *Informe, destino *bytes.Buffer) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Movimientos de "+inf.Titular, true)
	pdf.SetCreator("Finanzas", true)
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 16)
	pdf.AliasNbPages("{total}")

	// Las fuentes que trae fpdf son de un solo byte (cp1252). Este traductor
	// convierte el UTF-8 de Go para que las tildes y la ñ salgan bien.
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	type columna struct {
		titulo string
		ancho  float64
		alinea string
	}
	columnas := []columna{
		{"Fecha", 20, "L"}, {"Tipo", 16, "L"}, {"Categoría", 32, "L"},
		{"Descripción", 66, "L"}, {"Medio", 24, "L"}, {"Monto", 28, "R"},
	}

	titulos := func() {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetFillColor(29, 79, 216)
		pdf.SetTextColor(255, 255, 255)
		for _, c := range columnas {
			pdf.CellFormat(c.ancho, 7, tr(c.titulo), "", 0, c.alinea, true, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetTextColor(20, 20, 20)
		pdf.SetFont("Helvetica", "", 9)
	}

	primeraPagina := true
	pdf.SetHeaderFunc(func() {
		if primeraPagina {
			return
		}
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(102, 112, 133)
		pdf.CellFormat(0, 5, tr("Movimientos de "+inf.Titular+" · del "+inf.rango()), "", 1, "L", false, 0, "")
		pdf.Ln(1)
		titulos()
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(102, 112, 133)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("Página %d de {total}", pdf.PageNo())), "", 0, "R", false, 0, "")
	})

	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(0, 9, tr("Movimientos de "+inf.Titular), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, tr("Del "+inf.rango()), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(102, 112, 133)
	generado := "Generado el " + inf.Generado.Format("02/01/2006 15:04")
	if inf.ConFiltros {
		generado += " · con filtros aplicados"
	}
	pdf.CellFormat(0, 5, tr(generado), "", 1, "L", false, 0, "")
	pdf.Ln(3)

	// El resumen, en un recuadro.
	pdf.SetTextColor(20, 20, 20)
	pdf.SetFillColor(244, 245, 247)
	for _, t := range inf.filasTotales() {
		estilo := ""
		if t[0] == "Balance" {
			estilo = "B"
		}
		pdf.SetFont("Helvetica", estilo, 10)
		pdf.CellFormat(70, 6.5, tr(t[0]), "", 0, "L", true, 0, "")
		pdf.CellFormat(40, 6.5, tr(dinero.Formatear(t[1])), "", 1, "R", true, 0, "")
	}
	pdf.Ln(6)

	titulos()
	primeraPagina = false

	if len(inf.Movimientos) == 0 {
		pdf.SetTextColor(102, 112, 133)
		pdf.CellFormat(0, 8, tr("No hay movimientos en este rango."), "", 1, "L", false, 0, "")
	}

	// recortar deja el texto en el ancho de su columna, con "..." al final.
	recortar := func(s string, ancho float64) string {
		s = tr(s)
		if pdf.GetStringWidth(s) <= ancho-2 {
			return s
		}
		for len(s) > 0 && pdf.GetStringWidth(s+"...") > ancho-2 {
			s = s[:len(s)-1]
		}
		return s + "..."
	}

	for i, m := range inf.Movimientos {
		// Filas alternadas: se sigue la linea sin regla.
		pdf.SetFillColor(247, 248, 250)
		relleno := i%2 == 1

		descripcion := m.Descripcion
		if m.Tipo == TipoPreste {
			detalle := "a " + texto(m.AQuien)
			if m.Estado != nil {
				detalle += " · " + strings.ToLower(nombresEstado[*m.Estado])
			}
			if m.CobrarEl != nil && (m.Estado == nil || *m.Estado == EstadoPendiente) {
				detalle += ", cobrar el " + fechaCorta(*m.CobrarEl)
			}
			if m.MedioCobroNombre != nil {
				detalle += " por " + *m.MedioCobroNombre
			}
			if descripcion != "" {
				descripcion += " (" + detalle + ")"
			} else {
				descripcion = detalle
			}
		}

		valores := []string{
			fechaCorta(m.Fecha),
			nombresTipo[m.Tipo],
			m.CategoriaNombre,
			descripcion,
			texto(m.MedioPagoNombre),
			dinero.Formatear(m.Monto),
		}
		// El tr() ya lo hace recortar: aqui los textos entran en UTF-8.
		for j, c := range columnas {
			pdf.CellFormat(c.ancho, 6, recortar(valores[j], c.ancho), "", 0, c.alinea, relleno, 0, "")
		}
		pdf.Ln(-1)
	}

	if err := pdf.Output(destino); err != nil {
		return fmt.Errorf("generando PDF: %w", err)
	}
	return nil
}
