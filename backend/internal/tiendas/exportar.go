package tiendas

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"finanzas/internal/httpx"
)

// Exportar los cierres a Excel: para el contador, o para guardarlos fuera de
// la app. Se exporta lo MISMO que se esta viendo — los filtros del listado.

// MaxExportarCierres es el tope de hojas por archivo. Con un cierre diario son
// mas de trece años; el tope existe para que una peticion no ponga a la
// Raspberry a armar un archivo enorme.
const MaxExportarCierres = 5000

var ErrDemasiadosCierres = fmt.Errorf("hay más de %d cierres en ese rango", MaxExportarCierres)

// ExportarCierres responde el .xlsx.
//
// Solo Excel: un cierre tiene veinte columnas y un PDF con esa tabla no se
// lee en papel. Si algun dia hace falta imprimirlo, sera una hoja por cierre,
// con la forma del formato — no esta tabla.
func (h *Handler) ExportarCierres(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	filtros, ok := filtrosDeQuery(w, r)
	if !ok {
		return
	}

	lista, err := h.store.ListarTodosLosCierres(r.Context(), usuarioID, filtros)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: exportando cierres")
		return
	}
	if len(lista) > MaxExportarCierres {
		httpx.Error(w, http.StatusUnprocessableEntity, ErrDemasiadosCierres.Error())
		return
	}
	if len(lista) == 0 {
		httpx.Error(w, http.StatusUnprocessableEntity, "No hay cierres en ese rango para exportar")
		return
	}

	var buf bytes.Buffer
	if err := excelDeCierres(lista, filtros, &buf); err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: armando el Excel de cierres")
		return
	}

	nombre := "cierres"
	if filtros.Desde != "" || filtros.Hasta != "" {
		nombre += "-" + filtros.Desde + "-" + filtros.Hasta
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.xlsx"`, nombre))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// columnasExcel es la tabla, en el orden en que se lee la hoja de papel.
var columnasExcel = []struct {
	titulo string
	ancho  float64
	// dinero dice si la celda va con formato de pesos.
	dinero bool
}{
	{"Día", 12, false},
	{"Tienda", 18, false},
	{"Responsable", 18, false},
	{"QR · banco", 14, true},
	{"QR · tienda", 14, true},
	{"Datáfono", 14, true},
	{"Datáfono · tienda", 16, true},
	{"Efectivo contado", 16, true},
	{"Efectivo · tienda", 16, true},
	{"Ventas", 14, true},
	{"Compras", 13, true},
	{"Gastos", 13, true},
	{"Descuentos", 13, true},
	{"Vales", 13, true},
	{"Pagos Nequi", 14, true},
	{"Salió de la caja", 16, true},
	{"Debería quedar", 16, true},
	{"Hay en caja y bancos", 18, true},
	{"Diferencia", 14, true},
	{"Novedades", 40, false},
}

// valoresDe arma la fila de un cierre, en el mismo orden que columnasExcel.
func valoresDe(c Cierre) []any {
	return []any{
		c.Fecha, c.TiendaNombre, c.Responsable,
		c.QRBanco, c.QRTienda,
		c.DatafonoReporte, c.DatafonoTienda,
		c.Totales.EfectivoTotal, c.EfectivoTienda,
		c.VentaTienda,
		c.Totales.Compras, c.Totales.Gastos, c.Totales.Descuentos, c.Totales.Vales,
		c.Totales.PagosNequi,
		c.Totales.Salidas, c.Totales.DeberiaQuedar, c.Totales.MetodosPago,
		c.Totales.QuedaDiferencia,
		c.Novedades,
	}
}

func excelDeCierres(lista []Cierre, filtros FiltrosCierres, destino *bytes.Buffer) error {
	libro := excelize.NewFile()
	defer libro.Close()

	const hoja = "Cierres"
	if err := libro.SetSheetName("Sheet1", hoja); err != nil {
		return err
	}

	// Aqui SI se convierte el monto a float, y es a proposito: Excel guarda
	// todos sus numeros como float de 64 bits, y una celda de texto no se
	// podria sumar ni graficar. El archivo es para leer; las cuentas que
	// valen siguen siendo las de Postgres.
	numero := func(monto string) float64 {
		n, _ := strconv.ParseFloat(monto, 64)
		return n
	}

	formatoPesos := `"$" #,##0`
	titulo, err := libro.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	if err != nil {
		return err
	}
	tenue, err := libro.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "667085"}})
	if err != nil {
		return err
	}
	pesos, err := libro.NewStyle(&excelize.Style{CustomNumFmt: &formatoPesos})
	if err != nil {
		return err
	}
	pesosNegrita, err := libro.NewStyle(&excelize.Style{
		CustomNumFmt: &formatoPesos, Font: &excelize.Font{Bold: true},
	})
	if err != nil {
		return err
	}
	negrita, err := libro.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
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
	poner(1, fila, "Cierres de caja", titulo)
	fila++
	if filtros.Desde != "" || filtros.Hasta != "" {
		poner(1, fila, "Del "+fechaCorta(filtros.Desde)+" al "+fechaCorta(filtros.Hasta), 0)
		fila++
	}
	generado := "Generado el " + time.Now().In(zonaColombia).Format("02/01/2006 15:04")
	if filtros.Activos() {
		generado += " · con filtros aplicados"
	}
	poner(1, fila, generado, tenue)
	fila += 2

	filaTitulos := fila
	for i, c := range columnasExcel {
		poner(i+1, fila, c.titulo, encabezado)
		nombreCol, _ := excelize.ColumnNumberToName(i + 1)
		_ = libro.SetColWidth(hoja, nombreCol, nombreCol, c.ancho)
	}
	fila++

	for _, c := range lista {
		valores := valoresDe(c)
		for i, col := range columnasExcel {
			texto, _ := valores[i].(string)
			if col.dinero {
				poner(i+1, fila, numero(texto), pesos)
			} else if col.titulo == "Día" {
				// La fecha va como texto dd/mm/aaaa y no como fecha de Excel:
				// asi se ve igual en cualquier configuracion regional.
				poner(i+1, fila, fechaCorta(texto), 0)
			} else {
				poner(i+1, fila, texto, 0)
			}
		}
		fila++
	}

	// La fila de totales: las mismas sumas que muestra la pantalla, pero
	// escritas como formulas para que sigan cuadrando si alguien borra una
	// fila en Excel.
	poner(1, fila, "Totales", negrita)
	for i, col := range columnasExcel {
		if !col.dinero {
			continue
		}
		letra, _ := excelize.ColumnNumberToName(i + 1)
		formula := fmt.Sprintf("SUM(%s%d:%s%d)", letra, filaTitulos+1, letra, fila-1)
		c := celda(i+1, fila)
		_ = libro.SetCellFormula(hoja, c, formula)
		_ = libro.SetCellStyle(hoja, c, c, pesosNegrita)
	}

	// Filtros de Excel y la fila de titulos fija al bajar: con un año de
	// cierres, sin esto toca acordarse de que columna es cada una.
	ultima, _ := excelize.ColumnNumberToName(len(columnasExcel))
	_ = libro.AutoFilter(hoja, fmt.Sprintf("A%d:%s%d", filaTitulos, ultima, fila-1), nil)
	_ = libro.SetPanes(hoja, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: filaTitulos,
		TopLeftCell: fmt.Sprintf("A%d", filaTitulos+1), ActivePane: "bottomLeft",
	})

	if _, err := libro.WriteTo(destino); err != nil {
		return fmt.Errorf("escribiendo el Excel: %w", err)
	}
	return nil
}

// fechaCorta pasa "2026-09-15" a "15/09/2026" sin pasar por time: es texto
// que ya viene validado, y convertirlo a fecha solo para volverlo a escribir
// abre la puerta a un corrimiento de zona horaria.
func fechaCorta(iso string) string {
	partes := strings.Split(iso, "-")
	if len(partes) != 3 {
		return iso
	}
	return partes[2] + "/" + partes[1] + "/" + partes[0]
}
