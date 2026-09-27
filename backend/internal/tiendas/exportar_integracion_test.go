package tiendas_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"finanzas/internal/tiendas"
)

// otroDia devuelve la misma hoja con otra fecha, para poder probar rangos.
func otroDia(fecha string) string {
	return strings.Replace(hojaDeUnDia, `"fecha": "2026-09-20"`, fmt.Sprintf(`"fecha": %q`, fecha), 1)
}

// La seccion Cierres se lee como los movimientos: filtrando. Y lo que se
// exporta es lo mismo que se esta viendo, no todo.
func TestLosCierresSeFiltranPorTiendaYPorFecha(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)

	centro := e.crearTienda(t, ana, "centro")
	norte := e.crearTienda(t, ana, "norte")

	e.crearCierre(t, ana, centro, otroDia("2026-09-18"))
	e.crearCierre(t, ana, centro, otroDia("2026-09-20"))
	e.crearCierre(t, ana, norte, otroDia("2026-09-20"))

	// La ruta global no cuelga del router de tiendas: se monta aparte.
	global := e.rutasTodos()

	leer := func(t *testing.T, ruta string) []tiendas.Cierre {
		t.Helper()
		w := e.pedirEn(t, global, ana, "GET", ruta, "")
		if w.Code != http.StatusOK {
			t.Fatalf("listando %s: status = %d, cuerpo %s", ruta, w.Code, w.Body)
		}
		var lista []tiendas.Cierre
		if err := json.Unmarshal(w.Body.Bytes(), &lista); err != nil {
			t.Fatalf("leyendo la lista: %v", err)
		}
		return lista
	}

	if lista := leer(t, "/"); len(lista) != 3 {
		t.Errorf("sin filtros salieron %d cierres, se esperaban 3", len(lista))
	}
	// Lo mas nuevo primero, como los movimientos.
	if lista := leer(t, "/"); lista[0].Fecha != "2026-09-20" {
		t.Errorf("el primero es del %s: la lista no viene de lo mas nuevo a lo mas viejo", lista[0].Fecha)
	}
	if lista := leer(t, fmt.Sprintf("/?tienda_id=%d", centro)); len(lista) != 2 {
		t.Errorf("filtrando por tienda salieron %d, se esperaban 2", len(lista))
	}
	if lista := leer(t, "/?desde=2026-09-19"); len(lista) != 2 {
		t.Errorf("filtrando desde salieron %d, se esperaban 2", len(lista))
	}
	if lista := leer(t, "/?hasta=2026-09-19"); len(lista) != 1 {
		t.Errorf("filtrando hasta salieron %d, se esperaba 1", len(lista))
	}
	if lista := leer(t, fmt.Sprintf("/?tienda_id=%d&desde=2026-09-19", centro)); len(lista) != 1 {
		t.Errorf("con los dos filtros salieron %d, se esperaba 1", len(lista))
	}

	// Una fecha inventada no puede llegar a Postgres: se responde un error de
	// campo, no un 500.
	if w := e.pedirEn(t, global, ana, "GET", "/?desde=ayer", ""); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("con una fecha basura: status = %d, se esperaba un error de campo", w.Code)
	}
}

// El Excel sale, respeta los filtros y no se genera vacio.
func TestElExcelDeCierresRespetaLosFiltros(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	e.crearCierre(t, ana, tienda, hojaDeUnDia)

	global := e.rutasTodos()

	w := e.pedirEn(t, global, ana, "GET", "/exportar", "")
	if w.Code != http.StatusOK {
		t.Fatalf("exportando: status = %d, cuerpo %s", w.Code, w.Body)
	}
	// Un .xlsx es un ZIP: empieza por "PK".
	if !strings.HasPrefix(w.Body.String(), "PK") {
		t.Error("lo que volvió no parece un archivo de Excel")
	}
	if disposicion := w.Header().Get("Content-Disposition"); !strings.Contains(disposicion, "attachment") {
		t.Errorf("Content-Disposition = %q: el archivo no se descarga", disposicion)
	}

	// Un rango sin cierres no genera un archivo vacío: lo dice.
	w = e.pedirEn(t, global, ana, "GET", "/exportar?desde=2020-01-01&hasta=2020-01-31", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("exportando un rango vacío: status = %d, se esperaba 422", w.Code)
	}

	// Y sin el plan no se exporta nada, igual que el resto de la sección.
	otro := e.crearCliente(t)
	if w := e.pedirEn(t, global, otro, "GET", "/exportar", ""); w.Code != http.StatusForbidden {
		t.Errorf("exportando sin plan: status = %d, se esperaba 403", w.Code)
	}
}

// Las cifras del Excel son las mismas que las de la API. Entre las dos hay un
// ParseFloat (exportar.go), la unica conversion a float del paquete: si un
// dia se corriera una columna o se perdieran los centavos, el archivo que va
// al contador diria otra cosa que la pantalla y ninguna otra prueba lo veria.
func TestLasCifrasDelExcelSonLasDeLaAPI(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")

	// Con un vale de centavos, para que el float tenga algo que perder.
	hoja := strings.Replace(hojaDeUnDia, `"monto": "85000"`, `"monto": "85000.55"`, 1)
	creado := e.crearCierre(t, ana, tienda, hoja)

	w := e.pedir(t, ana, "GET", fmt.Sprintf("/%d/cierres/%d", tienda, creado.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("leyendo el cierre: status = %d, cuerpo %s", w.Code, w.Body)
	}
	var c tiendas.Cierre
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("leyendo el cierre: %v", err)
	}

	w = e.pedirEn(t, e.rutasTodos(), ana, "GET", "/exportar", "")
	if w.Code != http.StatusOK {
		t.Fatalf("exportando: status = %d, cuerpo %s", w.Code, w.Body)
	}
	libro, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("abriendo el Excel: %v", err)
	}
	defer libro.Close()
	// El valor crudo y no el que se ve: con el formato de pesos, "85000.55"
	// se veria "$ 85,001".
	filas, err := libro.GetRows("Cierres", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("leyendo la hoja: %v", err)
	}

	// La fila de titulos es la que empieza por "Día", y la del cierre es la
	// siguiente: es el unico del rango.
	titulos := -1
	for i, f := range filas {
		if len(f) > 0 && f[0] == "Día" {
			titulos = i
			break
		}
	}
	if titulos < 0 || titulos+1 >= len(filas) {
		t.Fatalf("no se encontro la fila del cierre en %v", filas)
	}
	encabezado, datos := filas[titulos], filas[titulos+1]

	esperado := map[string]string{
		"QR · banco":           c.QRBanco,
		"QR · tienda":          c.QRTienda,
		"Datáfono":             c.DatafonoReporte,
		"Datáfono · tienda":    c.DatafonoTienda,
		"Efectivo contado":     c.Totales.EfectivoTotal,
		"Efectivo · tienda":    c.EfectivoTienda,
		"Ventas":               c.VentaTienda,
		"Compras":              c.Totales.Compras,
		"Gastos":               c.Totales.Gastos,
		"Descuentos":           c.Totales.Descuentos,
		"Vales":                c.Totales.Vales,
		"Pagos Nequi":          c.Totales.PagosNequi,
		"Salió de la caja":     c.Totales.Salidas,
		"Debería quedar":       c.Totales.DeberiaQuedar,
		"Hay en caja y bancos": c.Totales.MetodosPago,
		"Diferencia":           c.Totales.QuedaDiferencia,
	}
	sinCifra := map[string]bool{"Día": true, "Tienda": true, "Responsable": true, "Novedades": true}

	for i, titulo := range encabezado {
		api, ok := esperado[titulo]
		if !ok {
			// Una columna de plata nueva tiene que entrar en el mapa de
			// arriba: si no, quedaria sin comparar y nadie se enteraria.
			if !sinCifra[titulo] {
				t.Errorf("la columna %q no se compara con la API", titulo)
			}
			continue
		}
		delete(esperado, titulo)

		celda := ""
		if i < len(datos) {
			celda = datos[i]
		}
		// Como racionales y no como texto: la API dice "89700.00" y Excel
		// guarda 89700, que es la misma cifra.
		enExcel, ok1 := new(big.Rat).SetString(celda)
		enAPI, ok2 := new(big.Rat).SetString(api)
		if !ok1 || !ok2 || enExcel.Cmp(enAPI) != 0 {
			t.Errorf("%s: el Excel dice %q y la API %q", titulo, celda, api)
		}
	}
	for titulo := range esperado {
		t.Errorf("la columna %q no salio en el Excel", titulo)
	}
}
