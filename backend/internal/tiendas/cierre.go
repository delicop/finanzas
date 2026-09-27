package tiendas

import "errors"

// Los cinco grupos de la hoja. Son los mismos que acepta el CHECK de la base:
// si aqui se agrega uno, hay que agregarlo alla.
const (
	GrupoPagoNequi = "pago_nequi"
	GrupoCompra    = "compra"
	GrupoGasto     = "gasto"
	GrupoDescuento = "descuento"
	GrupoVale      = "vale"
)

func GrupoValido(g string) bool {
	switch g {
	case GrupoPagoNequi, GrupoCompra, GrupoGasto, GrupoDescuento, GrupoVale:
		return true
	}
	return false
}

// Linea es un renglon de cualquiera de las listas: una descripcion y un valor.
type Linea struct {
	Grupo       string `json:"grupo"`
	Descripcion string `json:"descripcion"`
	Monto       string `json:"monto"` // string, como todo el dinero
}

// Cierre es la hoja del arqueo de un dia.
//
// Los campos de arriba son lo que se escribe; los de Totales son lo que sale
// de ellos. Las cuentas las hace Postgres al leer y no se guardan: guardar una
// resta es guardar dos veces el mismo dato, y el dia que una cifra se corrija
// la resta vieja quedaria mintiendo.
type Cierre struct {
	ID           int64  `json:"id"`
	TiendaID     int64  `json:"tienda_id"`
	TiendaNombre string `json:"tienda_nombre"`
	Fecha        string `json:"fecha"` // AAAA-MM-DD
	Responsable  string `json:"responsable"`

	QRBanco  string `json:"qr_banco"`
	QRTienda string `json:"qr_tienda"`

	DatafonoReporte string `json:"datafono_reporte"`
	DatafonoTienda  string `json:"datafono_tienda"`

	EfectivoBillete string `json:"efectivo_billete"`
	EfectivoMoneda  string `json:"efectivo_moneda"`
	EfectivoTienda  string `json:"efectivo_tienda"`

	VentaTienda string `json:"venta_tienda"`
	Novedades   string `json:"novedades"`

	// La foto de la hoja firmada. nil = no tiene.
	Foto *Foto `json:"foto"`

	Totales Totales `json:"totales"`

	// Las lineas solo se llenan al pedir UN cierre. En el listado van vacias:
	// la lista muestra fecha, responsable y cuanto cuadro, no el detalle.
	Lineas []Linea `json:"lineas"`
}

// Totales son las casillas que la hoja tiene calculadas.
//
// Una diferencia negativa significa que la tienda conto MENOS de lo que
// reporta el banco (o menos de lo que dice su propia venta): falta plata.
type Totales struct {
	QRDiferencia       string `json:"qr_diferencia"`       // tienda - banco
	DatafonoDiferencia string `json:"datafono_diferencia"` // tienda - datafono

	// EfectivoTotal es lo contado: billetes + monedas.
	EfectivoTotal      string `json:"efectivo_total"`
	EfectivoDiferencia string `json:"efectivo_diferencia"` // contado - lo que dice la tienda

	// MetodosPago es lo que suman las tres formas de cobro segun quien las
	// reporta (banco, datafono y el efectivo contado), y VentaDiferencia lo
	// compara con la venta que dice la tienda.
	MetodosPago     string `json:"metodos_pago"`
	VentaDiferencia string `json:"venta_diferencia"`

	// El total de cada lista, en el mismo orden de la hoja.
	PagosNequi string `json:"pagos_nequi"`
	Compras    string `json:"compras"`
	Gastos     string `json:"gastos"`
	Descuentos string `json:"descuentos"`
	Vales      string `json:"vales"`

	// --- cuanto quedo de verdad ---------------------------------------
	//
	// La venta del dia menos lo que salio de la caja tiene que dar lo que
	// hay en caja y bancos. Es la cuenta que cierra la hoja:
	//
	//	Salidas       = compras + gastos + descuentos + vales
	//	DeberiaQuedar = venta de la tienda - Salidas
	//	Queda         = MetodosPago (lo que de verdad hay)
	//	QuedaDiferencia = Queda - DeberiaQuedar
	//
	// Los pagos por Nequi NO se restan aqui: en la hoja van aparte y su
	// plata ya viene contada en lo que reporta el banco.
	Salidas         string `json:"salidas"`
	DeberiaQuedar   string `json:"deberia_quedar"`
	QuedaDiferencia string `json:"queda_diferencia"`
}

var (
	ErrCierreNoEncontrado = errors.New("cierre no encontrado")
	// Un dia, una hoja: lo impide la base, no la capa Go.
	ErrCierreDuplicado = errors.New("esa tienda ya tiene un cierre de ese dia")
)
