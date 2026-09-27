package tiendas_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

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
