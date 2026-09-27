package tiendas_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"finanzas/internal/tiendas"
)

// crearTienda deja una tienda lista y devuelve su id.
func (e *entorno) crearTienda(t *testing.T, usuarioID int64, nombre string) int64 {
	t.Helper()
	w := e.pedir(t, usuarioID, "POST", "/", fmt.Sprintf(`{"nombre":%q}`, unico(nombre)))
	if w.Code != http.StatusCreated {
		t.Fatalf("creando tienda: status = %d, cuerpo %s", w.Code, w.Body)
	}
	var tienda tiendas.Tienda
	if err := json.Unmarshal(w.Body.Bytes(), &tienda); err != nil {
		t.Fatalf("leyendo la tienda creada: %v", err)
	}
	return tienda.ID
}

// La hoja del papel, con las cifras de un día que no cuadró en efectivo.
const hojaDeUnDia = `{
	"fecha": "2026-09-20",
	"responsable": "Ana",
	"qr_banco": "1441000",       "qr_tienda": "1441000",
	"datafono_reporte": "1258800", "datafono_tienda": "1258800",
	"efectivo_billete": "1615000", "efectivo_moneda": "0",
	"efectivo_tienda": "1606800",
	"venta_tienda": "5100500",
	"novedades": "",
	"lineas": [
		{"grupo": "compra", "descripcion": "Salsamentaria", "monto": "25000"},
		{"grupo": "compra", "descripcion": "Panadería",     "monto": "30500"},
		{"grupo": "compra", "descripcion": "Verduras",      "monto": "34200"},
		{"grupo": "gasto",  "descripcion": "Salsamentaria", "monto": "41000"},
		{"grupo": "gasto",  "descripcion": "Plata grande",  "monto": "205000"},
		{"grupo": "vale",   "descripcion": "Kailovis",      "monto": "160000"},
		{"grupo": "vale",   "descripcion": "Edwars",        "monto": "80000"},
		{"grupo": "vale",   "descripcion": "Ana Maria",     "monto": "85000"},
		{"grupo": "pago_nequi", "descripcion": "Adelanto",  "monto": "810000"},
		{"grupo": "descuento",  "descripcion": "Albert",    "monto": "45000"},
		{"grupo": "descuento",  "descripcion": "Andres",    "monto": "80000"},
		{"grupo": "compra", "descripcion": "",              "monto": ""}
	]
}`

func (e *entorno) crearCierre(t *testing.T, usuarioID, tiendaID int64, cuerpo string) tiendas.Cierre {
	t.Helper()
	ruta := fmt.Sprintf("/%d/cierres", tiendaID)
	w := e.pedir(t, usuarioID, "POST", ruta, cuerpo)
	if w.Code != http.StatusCreated {
		t.Fatalf("creando cierre: status = %d, cuerpo %s", w.Code, w.Body)
	}
	var c tiendas.Cierre
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("leyendo el cierre creado: %v", err)
	}
	return c
}

// Las diferencias y los totales NO los manda el formulario: salen de las
// cifras escritas. Si alguna vez se guardaran, esta prueba seguiría pasando y
// el día de una corrección la hoja quedaría mintiendo.
func TestElCierreCalculaSusDiferenciasYTotales(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")

	c := e.crearCierre(t, ana, tienda, hojaDeUnDia)

	casos := []struct {
		que      string
		dio      string
		esperado string
	}{
		// Lo que contó la tienda contra lo que reporta el banco: cuadra.
		{"diferencia del QR", c.Totales.QRDiferencia, "0.00"},
		{"diferencia del datáfono", c.Totales.DatafonoDiferencia, "0.00"},
		// Billetes + monedas.
		{"total del efectivo", c.Totales.EfectivoTotal, "1615000.00"},
		// Contado menos lo que decía la tienda: sobran 8.200.
		{"diferencia del efectivo", c.Totales.EfectivoDiferencia, "8200.00"},
		// QR + datáfono + efectivo contado.
		{"total de métodos de pago", c.Totales.MetodosPago, "4314800.00"},
		// Lo cobrado contra lo que dice la tienda que vendio: faltan 785.700,
		// que es justo lo que salio de la caja durante el dia.
		{"diferencia de la venta", c.Totales.VentaDiferencia, "-785700.00"},
		// Los totales de cada lista.
		{"total de compras", c.Totales.Compras, "89700.00"},
		{"total de gastos", c.Totales.Gastos, "246000.00"},
		{"total de vales", c.Totales.Vales, "325000.00"},
		{"total de pagos Nequi", c.Totales.PagosNequi, "810000.00"},
		{"total de descuentos", c.Totales.Descuentos, "125000.00"},
		// La cuenta que cierra la hoja: compras + gastos + descuentos + vales.
		// Los pagos por Nequi no entran; si entraran, el dia no cuadraria.
		{"lo que salio de la caja", c.Totales.Salidas, "785700.00"},
		// Venta menos salidas = lo que tendria que haber en caja y bancos...
		{"lo que deberia quedar", c.Totales.DeberiaQuedar, "4314800.00"},
		// ...y es exactamente lo que hay: el dia cuadra.
		{"diferencia final", c.Totales.QuedaDiferencia, "0.00"},
	}
	for _, caso := range casos {
		if caso.dio != caso.esperado {
			t.Errorf("%s = %s, se esperaba %s", caso.que, caso.dio, caso.esperado)
		}
	}

	// El renglón en blanco de la hoja no se guarda: es un renglón que no se usó.
	ruta := fmt.Sprintf("/%d/cierres/%d", tienda, c.ID)
	w := e.pedir(t, ana, "GET", ruta, "")
	var completo tiendas.Cierre
	if err := json.Unmarshal(w.Body.Bytes(), &completo); err != nil {
		t.Fatalf("releyendo el cierre: %v", err)
	}
	if len(completo.Lineas) != 11 {
		t.Errorf("quedaron %d líneas, se esperaban 11 (la vacía no se guarda)", len(completo.Lineas))
	}
}

// Un día, una hoja. Dos cierres del mismo día en la misma tienda serían dos
// verdades distintas sobre la misma caja.
func TestNoSePuedenHacerDosCierresDelMismoDia(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	otra := e.crearTienda(t, ana, "norte")

	e.crearCierre(t, ana, tienda, hojaDeUnDia)

	w := e.pedir(t, ana, "POST", fmt.Sprintf("/%d/cierres", tienda), hojaDeUnDia)
	if w.Code == http.StatusCreated {
		t.Fatal("se creó un segundo cierre del mismo día en la misma tienda")
	}
	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
		t.Errorf("repetido: status = %d, se esperaba un error de campo", w.Code)
	}

	// Pero el mismo día en OTRA tienda es otra caja y otra hoja.
	e.crearCierre(t, ana, otra, hojaDeUnDia)
}

// Una URL con la tienda en blanco — `/api/tiendas//cierres`, que es lo que
// armaba el formulario cuando se quedaba sin tienda elegida — es un id
// invalido, no una hoja sin dueño. El arreglo de verdad esta en el formulario,
// que ya no manda esa URL; esto fija que el servidor jamas la interprete de
// otra forma.
func TestUnCierreSinTiendaEnLaURLNoSeCrea(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)

	// El host va escrito en la ruta porque "//cierres" a secas se lee como una
	// URL relativa al esquema y el path saldria vacio: lo que se quiere probar
	// es el segmento en blanco, no un path vacio.
	w := e.pedir(t, ana, "POST", "http://prueba.local//cierres", hojaDeUnDia)
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST con la tienda en blanco: status = %d, se esperaba 400, cuerpo %s", w.Code, w.Body)
	}
}

// Un renglon con monto y sin nombre no se guarda a medias: se rechaza la hoja
// entera. El de arriba, el del todo vacio, si se salta a proposito (es un
// renglon que no se uso); este no, porque hay plata escrita que alguien conto
// en su cuenta. Es lo que impide que el formulario vuelva a mandarlos.
func TestUnRenglonConMontoYSinNombreRechazaLaHoja(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")

	ruta := fmt.Sprintf("/%d/cierres", tienda)
	w := e.pedir(t, ana, "POST", ruta, `{
		"fecha": "2026-09-20", "responsable": "Ana",
		"qr_banco": "0", "qr_tienda": "0",
		"datafono_reporte": "0", "datafono_tienda": "0",
		"efectivo_billete": "0", "efectivo_moneda": "0", "efectivo_tienda": "0",
		"venta_tienda": "150000", "novedades": "",
		"lineas": [{"grupo": "compra", "descripcion": "   ", "monto": "150000"}]
	}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("renglon con monto y sin nombre: status = %d, se esperaba 422, cuerpo %s", w.Code, w.Body)
	}
	var respuesta struct {
		Campos map[string]string `json:"campos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &respuesta); err != nil {
		t.Fatalf("leyendo el error: %v", err)
	}
	if respuesta.Campos["lineas.0"] == "" {
		t.Errorf("el error no señala el renglon: campos = %v", respuesta.Campos)
	}

	// Y no quedo media hoja guardada: la lista de la tienda sigue vacia.
	w = e.pedir(t, ana, "GET", ruta, "")
	var lista []tiendas.Cierre
	if err := json.Unmarshal(w.Body.Bytes(), &lista); err != nil {
		t.Fatalf("listando los cierres: %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("quedaron %d cierres, se esperaba ninguno", len(lista))
	}
}

// Un cierre es el arqueo de un dia que ya cerro, asi que una fecha por delante
// no se guarda: no es un caso de uso, es un dedazo — y el mas facil es el año,
// que el <input type="date"> deja escribir a mano. El dia de hoy si entra: la
// hoja se llena al cerrar la caja.
//
// El "hoy" es el de Colombia, no el UTC del servidor: por eso las fechas de
// esta prueba se sacan de alla y no de time.Now() pelado.
func TestUnCierreNoPuedeSerDeUnDiaQueNoLlega(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")

	hoja := func(fecha string) string {
		return fmt.Sprintf(`{
			"fecha": %q, "responsable": "Ana",
			"qr_banco": "0", "qr_tienda": "0",
			"datafono_reporte": "0", "datafono_tienda": "0",
			"efectivo_billete": "0", "efectivo_moneda": "0", "efectivo_tienda": "0",
			"venta_tienda": "0", "novedades": "", "lineas": []
		}`, fecha)
	}
	hoy := time.Now().In(time.FixedZone("COT", -5*60*60))
	ruta := fmt.Sprintf("/%d/cierres", tienda)

	w := e.pedir(t, ana, "POST", ruta, hoja(hoy.AddDate(0, 0, 1).Format("2006-01-02")))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cierre de mañana: status = %d, se esperaba 422, cuerpo %s", w.Code, w.Body)
	}
	var respuesta struct {
		Campos map[string]string `json:"campos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &respuesta); err != nil {
		t.Fatalf("leyendo el error: %v", err)
	}
	if respuesta.Campos["fecha"] == "" {
		t.Errorf("el error no señala la fecha: campos = %v", respuesta.Campos)
	}

	e.crearCierre(t, ana, tienda, hoja(hoy.Format("2006-01-02")))
}

// El cierre de otro no se ve ni se toca, aunque los dos tengan tiendas.
func TestElCierreDeOtroNoSeVeNiSeEdita(t *testing.T) {
	e := nuevoEntorno(t)

	ana := e.crearCliente(t)
	beto := e.crearCliente(t)
	e.conPlan(t, ana, true)
	e.conPlan(t, beto, true)

	tiendaDeAna := e.crearTienda(t, ana, "centro")
	tiendaDeBeto := e.crearTienda(t, beto, "centro")
	c := e.crearCierre(t, ana, tiendaDeAna, hojaDeUnDia)

	// Con el id de la tienda de Ana: la tienda no es suya.
	ruta := fmt.Sprintf("/%d/cierres/%d", tiendaDeAna, c.ID)
	if w := e.pedir(t, beto, "GET", ruta, ""); w.Code != http.StatusNotFound {
		t.Errorf("Beto viendo el cierre de Ana: status = %d, se esperaba 404", w.Code)
	}
	if w := e.pedir(t, beto, "DELETE", ruta, ""); w.Code != http.StatusNotFound {
		t.Errorf("Beto borrando el cierre de Ana: status = %d, se esperaba 404", w.Code)
	}

	// Y colgándolo de SU propia tienda tampoco: el cierre no es de esa tienda.
	suyo := fmt.Sprintf("/%d/cierres/%d", tiendaDeBeto, c.ID)
	if w := e.pedir(t, beto, "GET", suyo, ""); w.Code != http.StatusNotFound {
		t.Errorf("Beto colando el cierre en su tienda: status = %d, se esperaba 404", w.Code)
	}

	// El de Ana sigue ahí.
	if w := e.pedir(t, ana, "GET", ruta, ""); w.Code != http.StatusOK {
		t.Errorf("Ana ya no puede ver su propio cierre: status = %d", w.Code)
	}
}

// Editar reemplaza la hoja entera, listas incluidas: se guarda como quedó en
// pantalla, no renglón por renglón.
func TestEditarReemplazaLaHojaEntera(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	c := e.crearCierre(t, ana, tienda, hojaDeUnDia)

	ruta := fmt.Sprintf("/%d/cierres/%d", tienda, c.ID)
	w := e.pedir(t, ana, "PUT", ruta, `{
		"fecha": "2026-09-20", "responsable": "Beto",
		"qr_banco": "100", "qr_tienda": "90",
		"datafono_reporte": "0", "datafono_tienda": "0",
		"efectivo_billete": "0", "efectivo_moneda": "0", "efectivo_tienda": "0",
		"venta_tienda": "100", "novedades": "Faltaron 10",
		"lineas": [{"grupo": "gasto", "descripcion": "Bolsas", "monto": "10"}]
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("editando: status = %d, cuerpo %s", w.Code, w.Body)
	}

	var editado tiendas.Cierre
	if err := json.Unmarshal(w.Body.Bytes(), &editado); err != nil {
		t.Fatalf("leyendo el cierre editado: %v", err)
	}
	if len(editado.Lineas) != 1 {
		t.Errorf("quedaron %d líneas, se esperaba 1: las viejas tenían que irse", len(editado.Lineas))
	}
	if editado.Totales.QRDiferencia != "-10.00" {
		t.Errorf("diferencia del QR = %s, se esperaba -10.00 (faltan 10)", editado.Totales.QRDiferencia)
	}
	if editado.Responsable != "Beto" {
		t.Errorf("responsable = %q, se esperaba Beto", editado.Responsable)
	}
}

// Una tienda con cierres no se borra: esas hojas son registros de plata.
func TestNoSeBorraUnaTiendaConCierres(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	e.crearCierre(t, ana, tienda, hojaDeUnDia)

	w := e.pedir(t, ana, "DELETE", fmt.Sprintf("/%d", tienda), "")
	if w.Code != http.StatusConflict {
		t.Errorf("borrando una tienda con cierres: status = %d, se esperaba 409", w.Code)
	}
}
