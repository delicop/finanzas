package movimientos_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"finanzas/internal/httpx"
	"finanzas/internal/movimientos"
)

func (e *entorno) exportar(t *testing.T, usuarioID int64, consulta string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/exportar?"+consulta, nil)
	req = req.WithContext(httpx.ConUsuarioID(context.Background(), usuarioID))
	res := httptest.NewRecorder()
	movimientos.NewHandler(e.store, nil).Rutas().ServeHTTP(res, req)
	return res
}

// datosDeSeptiembre deja movimientos dentro y fuera del rango que se exporta.
func (e *entorno) datosDeSeptiembre(t *testing.T) {
	t.Helper()
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "1000000", Fecha: "2026-09-01", Descripcion: "Sueldo", MedioPagoID: ptr(e.efectivo)})
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "45000.50", Fecha: "2026-09-10", Descripcion: "=HYPERLINK(\"http://malo\")", MedioPagoID: ptr(e.efectivo)})
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPreste, Monto: "200000", Fecha: "2026-09-15", Descripcion: "Préstamo", AQuien: ptr("Ñoño"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo)})
	// Fuera del rango: no puede aparecer ni sumar.
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "999", Fecha: "2026-10-01", Descripcion: "Octubre", MedioPagoID: ptr(e.efectivo)})
}

func TestInformeSoloTraeElRango(t *testing.T) {
	e := nuevoEntorno(t)
	e.datosDeSeptiembre(t)

	inf, err := e.store.Informe(context.Background(), e.usuarioID, movimientos.Filtros{Desde: "2026-09-01", Hasta: "2026-09-30"})
	if err != nil {
		t.Fatalf("informe: %v", err)
	}
	if len(inf.Movimientos) != 3 {
		t.Fatalf("movimientos = %d, se esperaban 3", len(inf.Movimientos))
	}
	// En orden cronológico, como un extracto.
	if inf.Movimientos[0].Descripcion != "Sueldo" || inf.Movimientos[2].Descripcion != "Préstamo" {
		t.Errorf("orden inesperado: %s ... %s", inf.Movimientos[0].Descripcion, inf.Movimientos[2].Descripcion)
	}

	// Las mismas cuentas del resumen: el saldo del préstamo resta, y lo que se
	// debe (nada, aquí) sumaría.
	esperado := movimientos.Totales{
		Recibido:   "1000000.00",
		Pagado:     "45000.50",
		PorCobrar:  "200000.00",
		PorPagar:   "0.00",
		Recuperado: "0.00",
		Abonado:    "0.00",
		Balance:    "754999.50",
	}
	if inf.Totales != esperado {
		t.Errorf("totales = %+v, se esperaba %+v", inf.Totales, esperado)
	}
	if inf.Titular != "Prueba" {
		t.Errorf("titular = %q", inf.Titular)
	}
}

func TestExportarExcel(t *testing.T) {
	e := nuevoEntorno(t)
	e.datosDeSeptiembre(t)

	res := e.exportar(t, e.usuarioID, "formato=xlsx&desde=2026-09-01&hasta=2026-09-30")
	if res.Code != http.StatusOK {
		t.Fatalf("exportar: %d — %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Disposition"), "movimientos-2026-09-01-a-2026-09-30.xlsx") {
		t.Errorf("nombre del archivo: %q", res.Header().Get("Content-Disposition"))
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Error("el archivo se podría quedar en caché")
	}

	libro, err := excelize.OpenReader(bytes.NewReader(res.Body.Bytes()))
	if err != nil {
		t.Fatalf("el Excel no abre: %v", err)
	}
	defer libro.Close()

	filas, err := libro.GetRows("Movimientos")
	if err != nil {
		t.Fatalf("leyendo filas: %v", err)
	}
	todo := ""
	for _, f := range filas {
		todo += strings.Join(f, "|") + "\n"
	}
	for _, debe := range []string{"Sueldo", "Préstamo", "Ñoño", "Pendiente", "Presté", "Balance"} {
		if !strings.Contains(todo, debe) {
			t.Errorf("falta %q en el Excel:\n%s", debe, todo)
		}
	}
	if strings.Contains(todo, "Octubre") {
		t.Error("se coló un movimiento fuera del rango")
	}

	// Una descripción que parece fórmula queda como texto.
	for _, f := range filas {
		if len(f) > 3 && strings.HasPrefix(f[3], "=HYPERLINK") {
			celda, _ := excelize.CoordinatesToCellName(4, indiceDe(filas, f)+1)
			formula, _ := libro.GetCellFormula("Movimientos", celda)
			if formula != "" {
				t.Errorf("la descripción quedó como fórmula: %q", formula)
			}
			return
		}
	}
	t.Error("no se encontró la fila con la descripción sospechosa")
}

func indiceDe(filas [][]string, buscada []string) int {
	for i, f := range filas {
		if strings.Join(f, "|") == strings.Join(buscada, "|") {
			return i
		}
	}
	return -1
}

func TestExportarPDF(t *testing.T) {
	e := nuevoEntorno(t)
	e.datosDeSeptiembre(t)

	res := e.exportar(t, e.usuarioID, "formato=pdf&desde=2026-09-01&hasta=2026-09-30")
	if res.Code != http.StatusOK {
		t.Fatalf("exportar: %d — %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/pdf" {
		t.Errorf("tipo = %q", res.Header().Get("Content-Type"))
	}
	if !bytes.HasPrefix(res.Body.Bytes(), []byte("%PDF-")) {
		t.Error("la respuesta no es un PDF")
	}
}

// Lo de otro usuario no sale en el archivo aunque pida el mismo rango.
func TestExportarNoTraeDatosAjenos(t *testing.T) {
	e := nuevoEntorno(t)
	e.datosDeSeptiembre(t)
	otro := nuevoEntorno(t)

	inf, err := otro.store.Informe(context.Background(), otro.usuarioID, movimientos.Filtros{Desde: "2026-09-01", Hasta: "2026-09-30"})
	if err != nil {
		t.Fatalf("informe: %v", err)
	}
	if len(inf.Movimientos) != 0 || inf.Totales.Recibido != "0.00" {
		t.Errorf("el otro usuario ve %d movimientos ajenos (recibido %s)", len(inf.Movimientos), inf.Totales.Recibido)
	}

	// Y el archivo vacío se genera igual, con su aviso.
	for _, formato := range []string{"xlsx", "pdf"} {
		if res := otro.exportar(t, otro.usuarioID, "formato="+formato+"&desde=2026-09-01&hasta=2026-09-30"); res.Code != http.StatusOK {
			t.Errorf("%s vacío: %d", formato, res.Code)
		}
	}
}

func TestExportarValidaLaPeticion(t *testing.T) {
	e := nuevoEntorno(t)

	casos := map[string]string{
		"formato=docx&desde=2026-09-01&hasta=2026-09-30":           "formato",
		"formato=pdf&hasta=2026-09-30":                             "desde",
		"formato=pdf&desde=2026-09-01":                             "hasta",
		"formato=pdf&desde=2026-09-30&hasta=2026-09-01":            "hasta",
		"formato=pdf&desde=2010-01-01&hasta=2026-09-01":            "desde",
		"formato=pdf&desde=2026-09-01&hasta=2026-09-30&tipo=robar": "tipo",
	}
	for consulta, campo := range casos {
		res := e.exportar(t, e.usuarioID, consulta)
		if res.Code != http.StatusUnprocessableEntity && res.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, se esperaba un error de validación", consulta, res.Code)
			continue
		}
		if !strings.Contains(res.Body.String(), `"`+campo+`"`) {
			t.Errorf("%s: el error no señala %q: %s", consulta, campo, res.Body.String())
		}
	}
}

// Los filtros del listado también aplican al archivo.
func TestExportarRespetaLosFiltros(t *testing.T) {
	e := nuevoEntorno(t)
	e.datosDeSeptiembre(t)

	inf, err := e.store.Informe(context.Background(), e.usuarioID, movimientos.Filtros{
		Desde: "2026-09-01", Hasta: "2026-09-30", Tipo: movimientos.TipoPague,
	})
	if err != nil {
		t.Fatalf("informe: %v", err)
	}
	if len(inf.Movimientos) != 1 || !inf.ConFiltros {
		t.Errorf("con filtro de tipo: %d movimientos, ConFiltros=%v", len(inf.Movimientos), inf.ConFiltros)
	}
	if inf.Totales.Recibido != "0.00" || inf.Totales.Pagado != "45000.50" {
		t.Errorf("los totales no respetan el filtro: %+v", inf.Totales)
	}
}

// El camino completo del formulario: POST sin decir de dónde salió el resto
// da 409 con las cifras; el mismo POST con `cubrir` guarda las dos cosas.
func TestElEndpointPreguntaDeDondeSaleLaPlata(t *testing.T) {
	e := nuevoEntorno(t)
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "400000", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})

	enviar := func(cuerpo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(httpx.ConUsuarioID(context.Background(), e.usuarioID))
		res := httptest.NewRecorder()
		movimientos.NewHandler(e.store, nil).Rutas().ServeHTTP(res, req)
		return res
	}

	gasto := fmt.Sprintf(`{"categoria_id":%d,"medio_pago_id":%d,"tipo":"pague","monto":"600000","fecha":"2026-09-02"`,
		e.categoria, e.efectivo)

	res := enviar(gasto + `}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("sin cubrir: %d — %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"falta":"200000.00"`) {
		t.Errorf("la respuesta no dice cuánto falta: %s", res.Body.String())
	}

	res = enviar(gasto + `,"cubrir":{"tipo":"me_prestaron","a_quien":""}}`)
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "cubrir.a_quien") {
		t.Errorf("préstamo sin a quién: %d — %s", res.Code, res.Body.String())
	}

	res = enviar(gasto + `,"cubrir":{"tipo":"me_prestaron","a_quien":"Mi hermano"}}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("con cubrir: %d — %s", res.Code, res.Body.String())
	}
	if s := e.saldoDe(t, "Efectivo"); s != "0.00" {
		t.Errorf("efectivo = %s, se esperaba 0.00", s)
	}
}
