package movimientos_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"finanzas/internal/auth"
	"finanzas/internal/categorias"
	"finanzas/internal/db"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
)

// Estas pruebas corren contra un Postgres DE VERDAD, porque lo que estamos
// probando ES el SQL: los CHECK que protegen las reglas de "presté" y las
// sumas del resumen. Con una base falsa no se probaría nada real.
//
// Se saltan solas si no hay base de prueba configurada, para que
// `go test ./...` siga funcionando sin Docker:
//
//	TEST_DATABASE_URL="postgres://finanzas:clave@localhost:5436/finanzas_test?sslmode=disable" go test ./...
func abrirBasePrueba(t *testing.T) *sql.DB {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL no está definida: se omiten las pruebas de integración")
	}

	pool, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("no se pudo conectar a la base de prueba: %v", err)
	}
	if err := db.Migrate(pool); err != nil {
		t.Fatalf("no se pudieron aplicar las migraciones: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

// entorno deja la base limpia y crea lo mínimo para trabajar.
type entorno struct {
	pool      *sql.DB
	usuarioID int64
	categoria int64
	efectivo  int64
	banco     int64
	store     *movimientos.Store
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := abrirBasePrueba(t)
	ctx := context.Background()

	hash, err := auth.HashPassword("ClaveDePrueba123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	// Un usuario nuevo por prueba, con correo único, y al terminar se borra
	// solo lo suyo.
	//
	// Antes esto vaciaba las tablas enteras, y funcionó mientras este fue el
	// único paquete con pruebas de integración. `go test ./...` corre los
	// paquetes en PARALELO contra la misma base: una limpieza global le
	// arranca los datos al paquete de al lado a mitad de prueba. Como todas
	// las consultas filtran por usuario_id, un usuario nuevo ya es un
	// compartimento limpio.
	email := fmt.Sprintf("prueba-%d-%d@finanzas.local", time.Now().UnixNano(), contador.Add(1))
	usuario, err := auth.NewStore(pool).Crear(ctx, email, "Prueba", auth.RolUsuario, hash)
	if err != nil {
		t.Fatalf("creando usuario: %v", err)
	}

	// El orden importa por las llaves foráneas: primero lo que apunta. Los
	// movimientos van antes que las categorías porque esa FK es RESTRICT.
	t.Cleanup(func() {
		limpieza := context.Background()
		for _, tabla := range []string{"movimientos", "categorias", "medios_pago"} {
			if _, err := pool.ExecContext(limpieza, "DELETE FROM "+tabla+" WHERE usuario_id = $1", usuario.ID); err != nil {
				t.Errorf("limpiando %s: %v", tabla, err)
			}
		}
		if _, err := pool.ExecContext(limpieza, "DELETE FROM usuarios WHERE id = $1", usuario.ID); err != nil {
			t.Errorf("limpiando usuarios: %v", err)
		}
	})

	cat, err := categorias.NewStore(pool).Crear(ctx, usuario.ID, "Negocio")
	if err != nil {
		t.Fatalf("creando categoría: %v", err)
	}

	mStore := medios.NewStore(pool)
	efectivo, err := mStore.Crear(ctx, usuario.ID, "Efectivo")
	if err != nil {
		t.Fatalf("creando medio: %v", err)
	}
	banco, err := mStore.Crear(ctx, usuario.ID, "Transferencia")
	if err != nil {
		t.Fatalf("creando medio: %v", err)
	}

	return &entorno{
		pool:      pool,
		usuarioID: usuario.ID,
		categoria: cat.ID,
		efectivo:  efectivo.ID,
		banco:     banco.ID,
		store:     movimientos.NewStore(pool),
	}
}

func (e *entorno) crear(t *testing.T, d movimientos.Datos) *movimientos.Movimiento {
	t.Helper()
	if d.CategoriaID == 0 {
		d.CategoriaID = e.categoria
	}
	m, err := e.store.Crear(context.Background(), e.usuarioID, d)
	if err != nil {
		t.Fatalf("creando movimiento %+v: %v", d, err)
	}
	return m
}

func (e *entorno) resumen(t *testing.T) *movimientos.Resumen {
	t.Helper()
	r, err := e.store.Resumen(context.Background(), e.usuarioID)
	if err != nil {
		t.Fatalf("resumen: %v", err)
	}
	return r
}

func ptr[T any](v T) *T { return &v }

// contador da correos únicos: el índice de usuarios no deja repetirlos y estas
// pruebas se corren muchas veces contra la misma base.
var contador atomic.Int64

// --------------------------------------------------------------------------
// Las reglas de "presté" viven en un CHECK de la base de datos, no solo en Go.
// Esto comprueba que ni un UPDATE manual podría dejar datos incoherentes.
// --------------------------------------------------------------------------

func TestPrestamoExigeAQuienYEstado(t *testing.T) {
	e := nuevoEntorno(t)

	_, err := e.store.Crear(context.Background(), e.usuarioID, movimientos.Datos{
		CategoriaID: e.categoria,
		Tipo:        movimientos.TipoPreste,
		Monto:       "100000",
		Fecha:       "2026-09-16",
		MedioPagoID: ptr(e.efectivo),
		// sin AQuien ni Estado
	})
	if err == nil {
		t.Fatal("la base aceptó un préstamo sin a_quien ni estado")
	}
}

func TestNoPrestamoNoPuedeTenerAQuien(t *testing.T) {
	e := nuevoEntorno(t)

	_, err := e.store.Crear(context.Background(), e.usuarioID, movimientos.Datos{
		CategoriaID: e.categoria,
		Tipo:        movimientos.TipoPague,
		Monto:       "100000",
		Fecha:       "2026-09-16",
		MedioPagoID: ptr(e.efectivo),
		AQuien:      ptr("Alguien"),
		Estado:      ptr(movimientos.EstadoPendiente),
	})
	if err == nil {
		t.Fatal("la base aceptó un 'pagué' con a_quien y estado")
	}
}

func TestMontoDebeSerPositivo(t *testing.T) {
	e := nuevoEntorno(t)

	for _, monto := range []string{"0", "-100"} {
		_, err := e.store.Crear(context.Background(), e.usuarioID, movimientos.Datos{
			CategoriaID: e.categoria,
			Tipo:        movimientos.TipoPague,
			Monto:       monto,
			Fecha:       "2026-09-16",
			MedioPagoID: ptr(e.efectivo),
		})
		if err == nil {
			t.Errorf("la base aceptó un monto de %s", monto)
		}
	}
}

// --------------------------------------------------------------------------
// La matemática del dinero. Si algo de esto falla, al cliente le cuadran mal
// las cuentas, que es lo peor que puede pasar en esta app.
// --------------------------------------------------------------------------

func TestBalanceGeneral(t *testing.T) {
	e := nuevoEntorno(t)

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "1000000", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "300000", Fecha: "2026-09-02", MedioPagoID: ptr(e.efectivo)})

	r := e.resumen(t)

	if r.Totales.Recibido != "1000000.00" {
		t.Errorf("recibido = %s, se esperaba 1000000.00", r.Totales.Recibido)
	}
	if r.Totales.Pagado != "300000.00" {
		t.Errorf("pagado = %s, se esperaba 300000.00", r.Totales.Pagado)
	}
	if r.Totales.Balance != "700000.00" {
		t.Errorf("balance = %s, se esperaba 700000.00", r.Totales.Balance)
	}
}

// LA regla del negocio: un préstamo pendiente resta del balance; uno ya
// devuelto queda en CERO (salió y volvió), no suma el doble.
func TestPrestamoDevueltoQuedaEnCero(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "1000000", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})
	prestamo := e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "200000", Fecha: "2026-09-02",
		AQuien: ptr("Carlos"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})

	antes := e.resumen(t)
	if antes.Totales.Balance != "800000.00" {
		t.Fatalf("con el préstamo pendiente el balance debería ser 800000.00, es %s", antes.Totales.Balance)
	}
	if antes.PrestadoPendiente != "200000.00" {
		t.Errorf("te deben = %s, se esperaba 200000.00", antes.PrestadoPendiente)
	}

	// Nos pagan.
	if _, err := e.store.CambiarEstado(ctx, e.usuarioID, prestamo.ID, movimientos.EstadoPagado, ptr(e.efectivo)); err != nil {
		t.Fatalf("CambiarEstado: %v", err)
	}

	despues := e.resumen(t)

	// La plata volvió: el balance sube EXACTAMENTE los 200.000 que salieron.
	// Si diera 1.200.000 estaríamos contando el mismo dinero dos veces.
	if despues.Totales.Balance != "1000000.00" {
		t.Errorf("tras el cobro el balance debería ser 1000000.00, es %s", despues.Totales.Balance)
	}
	if despues.PrestadoPendiente != "0.00" {
		t.Errorf("ya no deberían deberte nada, dice %s", despues.PrestadoPendiente)
	}
	if despues.Totales.Recuperado != "200000.00" {
		t.Errorf("recuperado = %s, se esperaba 200000.00", despues.Totales.Recuperado)
	}
}

// Prestar en efectivo y que devuelvan por transferencia mueve la plata de un
// medio al otro.
func TestPrestarEnEfectivoYCobrarPorBanco(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	prestamo := e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "500000", Fecha: "2026-09-01",
		AQuien: ptr("Andrés"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})

	saldo := func(nombre string) string {
		t.Helper()
		for _, m := range e.resumen(t).Medios {
			if m.Nombre == nombre {
				return m.Saldo
			}
		}
		t.Fatalf("no apareció el medio %q en el resumen", nombre)
		return ""
	}

	if s := saldo("Efectivo"); s != "-500000.00" {
		t.Errorf("al prestar, el efectivo debería quedar en -500000.00, está en %s", s)
	}

	if _, err := e.store.CambiarEstado(ctx, e.usuarioID, prestamo.ID, movimientos.EstadoPagado, ptr(e.banco)); err != nil {
		t.Fatalf("CambiarEstado: %v", err)
	}

	if s := saldo("Efectivo"); s != "-500000.00" {
		t.Errorf("el efectivo debe seguir en -500000.00 (de ahí salió), está en %s", s)
	}
	if s := saldo("Transferencia"); s != "500000.00" {
		t.Errorf("la transferencia debería quedar en 500000.00 (por ahí volvió), está en %s", s)
	}
}

// Los saldos de los medios tienen que sumar el balance general. Si no cuadran,
// el usuario no tiene forma de confiar en el resumen.
func TestLosSaldosPorMedioSumanElBalance(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "2500000", Fecha: "2026-09-01", MedioPagoID: ptr(e.banco)})
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "400000", Fecha: "2026-09-02", MedioPagoID: ptr(e.efectivo)})
	e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "150000", Fecha: "2026-09-03",
		AQuien: ptr("Marcela"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})
	cobrado := e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "90000", Fecha: "2026-09-04",
		AQuien: ptr("Ana"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.banco),
	})
	if _, err := e.store.CambiarEstado(ctx, e.usuarioID, cobrado.ID, movimientos.EstadoPagado, ptr(e.efectivo)); err != nil {
		t.Fatalf("CambiarEstado: %v", err)
	}

	r := e.resumen(t)

	// Sumamos con NUMERIC en Postgres, no en Go: comparar centavos con float
	// sería justo el error que toda la app evita.
	var suma string
	err := e.pool.QueryRowContext(context.Background(),
		`SELECT (coalesce(sum(monto) FILTER (WHERE tipo='recibi'),0)
		       - coalesce(sum(monto) FILTER (WHERE tipo='pague'),0)
		       - coalesce(sum(monto) FILTER (WHERE tipo='preste' AND estado='pendiente'),0)
		       )::numeric(14,2)::text
		 FROM movimientos WHERE usuario_id=$1`, e.usuarioID).Scan(&suma)
	if err != nil {
		t.Fatalf("calculando el balance esperado: %v", err)
	}

	if r.Totales.Balance != suma {
		t.Errorf("balance del resumen = %s, calculado = %s", r.Totales.Balance, suma)
	}

	// La expresión va dentro del texto del SQL (no como parámetro) porque es
	// una SUMA, no un valor. Es seguro: los números salen de nuestro propio
	// resumen, no de una entrada del usuario. En código de producción esto
	// no se haría nunca.
	var totalMedios string
	consulta := "SELECT (" + sumarSaldos(r) + ")::numeric(14,2)::text"
	if err := e.pool.QueryRowContext(context.Background(), consulta).Scan(&totalMedios); err != nil {
		t.Fatalf("sumando saldos: %v", err)
	}
	if totalMedios != r.Totales.Balance {
		t.Errorf("la suma de los saldos por medio (%s) no da el balance (%s)", totalMedios, r.Totales.Balance)
	}
}

// sumarSaldos suma los saldos como texto usando Postgres, para no tocar float.
func sumarSaldos(r *movimientos.Resumen) string {
	partes := make([]string, 0, len(r.Medios)+1)
	partes = append(partes, "0")
	for _, m := range r.Medios {
		partes = append(partes, fmt.Sprintf("(%s)", m.Saldo))
	}
	return strings.Join(partes, " + ")
}

// Cobrar un préstamo ya no escribe un flag: registra un abono por lo que
// faltaba. Volver a pendiente borra esos abonos, y el saldo vuelve a ser el
// monto completo.
func TestVolverAPendienteBorraLosAbonos(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	p := e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "100000", Fecha: "2026-09-01",
		AQuien: ptr("Luis"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})

	cobrado, err := e.store.CambiarEstado(ctx, e.usuarioID, p.ID, movimientos.EstadoPagado, ptr(e.banco))
	if err != nil {
		t.Fatalf("CambiarEstado: %v", err)
	}
	if cobrado.Abonos != 1 {
		t.Fatalf("saldar debió dejar un abono, dejó %d", cobrado.Abonos)
	}
	if cobrado.Saldo != "0.00" {
		t.Errorf("saldo después de cobrar = %s, se esperaba 0.00", cobrado.Saldo)
	}

	abonos, err := e.store.ListarAbonos(ctx, e.usuarioID, p.ID)
	if err != nil {
		t.Fatalf("ListarAbonos: %v", err)
	}
	if len(abonos) != 1 || abonos[0].MedioID == nil || *abonos[0].MedioID != e.banco {
		t.Errorf("no quedó registrado por dónde pagaron: %+v", abonos)
	}

	pendiente, err := e.store.CambiarEstado(ctx, e.usuarioID, p.ID, movimientos.EstadoPendiente, nil)
	if err != nil {
		t.Fatalf("CambiarEstado: %v", err)
	}
	if pendiente.Abonos != 0 || pendiente.Saldo != "100000.00" {
		t.Errorf("al volver a pendiente debe quedar sin abonos y con el saldo completo: abonos=%d saldo=%s",
			pendiente.Abonos, pendiente.Saldo)
	}
}

// Un abono parcial deja la deuda en "parcial" y baja el saldo, sin tocar el
// monto original.
func TestAbonoParcial(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	p := e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "200000", Fecha: "2026-09-01",
		AQuien: ptr("Carlos"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})

	con, err := e.store.Abonar(ctx, e.usuarioID, p.ID, movimientos.AbonoDatos{
		Monto: "50000", Fecha: "2026-09-10", MedioID: ptr(e.banco),
	})
	if err != nil {
		t.Fatalf("Abonar: %v", err)
	}
	if con.Estado == nil || *con.Estado != movimientos.EstadoParcial {
		t.Errorf("estado = %v, se esperaba parcial", con.Estado)
	}
	if con.Saldo != "150000.00" || con.Abonado != "50000.00" {
		t.Errorf("saldo = %s, abonado = %s; se esperaba 150000.00 y 50000.00", con.Saldo, con.Abonado)
	}
	if con.Monto != "200000.00" {
		t.Errorf("el monto original no se debe tocar: %s", con.Monto)
	}

	// El dashboard cuenta el SALDO, no el monto: si contara el monto, el
	// "te deben" no bajaría nunca aunque fueran pagando.
	r := e.resumen(t)
	if r.Totales.PorCobrar != "150000.00" {
		t.Errorf("por_cobrar = %s, se esperaba 150000.00", r.Totales.PorCobrar)
	}

	// Abonar más de lo que falta es un error, no un saldo negativo.
	if _, err := e.store.Abonar(ctx, e.usuarioID, p.ID, movimientos.AbonoDatos{
		Monto: "150001", Fecha: "2026-09-11",
	}); !errors.Is(err, movimientos.ErrAbonoDeMas) {
		t.Errorf("abonar de más devolvió %v, se esperaba ErrAbonoDeMas", err)
	}

	// El que completa el saldo la deja pagada.
	saldada, err := e.store.Abonar(ctx, e.usuarioID, p.ID, movimientos.AbonoDatos{
		Monto: "150000", Fecha: "2026-09-12", MedioID: ptr(e.efectivo),
	})
	if err != nil {
		t.Fatalf("Abonar (el último): %v", err)
	}
	if saldada.Estado == nil || *saldada.Estado != movimientos.EstadoPagado {
		t.Errorf("estado = %v, se esperaba pagado", saldada.Estado)
	}
}

// Lo que te prestan a ti entra al bolsillo y suma al balance, aunque lo debas.
// Es el espejo exacto de prestar.
func TestDeudaPropiaSumaAlBalance(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoMePrestaron, Monto: "300000", Fecha: "2026-09-01",
		AQuien: ptr("Negocio 2"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})

	r := e.resumen(t)
	if r.Totales.PorPagar != "300000.00" {
		t.Errorf("por_pagar = %s, se esperaba 300000.00", r.Totales.PorPagar)
	}
	if r.Totales.Balance != "300000.00" {
		t.Errorf("balance = %s: la plata prestada está en el bolsillo, aunque se deba", r.Totales.Balance)
	}

	// Y está en el medio por el que entró.
	if efectivo := medioLlamado(r, "Efectivo"); efectivo == nil || efectivo.Saldo != "300000.00" {
		t.Errorf("el saldo en efectivo no refleja lo que le prestaron: %+v", efectivo)
	}

	// La contraparte sale del lado correcto.
	if len(r.Contrapartes) != 1 {
		t.Fatalf("contrapartes = %+v", r.Contrapartes)
	}
	c := r.Contrapartes[0]
	if c.LeDebes != "300000.00" || c.TeDeben != "0.00" || c.Neto != "-300000.00" {
		t.Errorf("la contraparte quedó del lado equivocado: %+v", c)
	}

	_ = ctx
}

// Un traslado mueve los dos saldos por medio y deja el balance general igual.
func TestTrasladoNoCambiaElBalance(t *testing.T) {
	e := nuevoEntorno(t)

	e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoRecibi, Monto: "500000", Fecha: "2026-09-01",
		MedioPagoID: ptr(e.efectivo),
	})
	antes := e.resumen(t)

	e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoTraslado, Monto: "200000", Fecha: "2026-09-02",
		MedioPagoID: ptr(e.efectivo), MedioCobroID: ptr(e.banco),
	})
	despues := e.resumen(t)

	if antes.Totales.Balance != despues.Totales.Balance {
		t.Errorf("el traslado cambió el balance: %s -> %s", antes.Totales.Balance, despues.Totales.Balance)
	}
	if despues.Totales.Recibido != "500000.00" || despues.Totales.Pagado != "0.00" {
		t.Errorf("un traslado no es ingreso ni gasto: recibido=%s pagado=%s",
			despues.Totales.Recibido, despues.Totales.Pagado)
	}

	if efectivo := medioLlamado(despues, "Efectivo"); efectivo == nil || efectivo.Saldo != "300000.00" {
		t.Errorf("saldo en efectivo = %+v, se esperaba 300000.00", efectivo)
	}
	if banco := medioLlamado(despues, "Transferencia"); banco == nil || banco.Saldo != "200000.00" {
		t.Errorf("saldo en Transferencia = %+v, se esperaba 200000.00", banco)
	}
}

func medioLlamado(r *movimientos.Resumen, nombre string) *movimientos.ResumenMedio {
	for i := range r.Medios {
		if r.Medios[i].Nombre == nombre {
			return &r.Medios[i]
		}
	}
	return nil
}

// Nadie puede tocar datos de otro usuario aunque adivine el id (IDOR).
func TestNoSeVenDatosDeOtroUsuario(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	mio := e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "1000", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})

	const otroUsuario = int64(999999)

	if _, err := e.store.PorID(ctx, otroUsuario, mio.ID); err == nil {
		t.Error("otro usuario pudo LEER el movimiento")
	}
	if _, err := e.store.Eliminar(ctx, otroUsuario, mio.ID); err == nil {
		t.Error("otro usuario pudo BORRAR el movimiento")
	}

	lista, _, err := e.store.Listar(ctx, otroUsuario, movimientos.Filtros{})
	if err != nil {
		t.Fatalf("Listar: %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("otro usuario vio %d movimientos ajenos", len(lista))
	}
}

func TestNoSePuedeUsarUnaCategoriaAjena(t *testing.T) {
	e := nuevoEntorno(t)

	_, err := e.store.Crear(context.Background(), e.usuarioID, movimientos.Datos{
		CategoriaID: 999999, // no existe
		Tipo:        movimientos.TipoPague,
		Monto:       "1000",
		Fecha:       "2026-09-01",
		MedioPagoID: ptr(e.efectivo),
	})
	if err == nil {
		t.Fatal("se creó un movimiento con una categoría inexistente")
	}
}

func TestFiltros(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "1000", Fecha: "2026-09-01", Descripcion: "Venta grande", MedioPagoID: ptr(e.banco)})
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "2000", Fecha: "2026-09-15", Descripcion: "Arriendo", MedioPagoID: ptr(e.efectivo)})
	e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "3000", Fecha: "2026-09-20",
		AQuien: ptr("Pedro"), Estado: ptr(movimientos.EstadoPendiente), MedioPagoID: ptr(e.efectivo),
	})

	casos := []struct {
		nombre   string
		filtros  movimientos.Filtros
		esperado int
	}{
		{"sin filtros", movimientos.Filtros{}, 3},
		{"por tipo", movimientos.Filtros{Tipo: movimientos.TipoPague}, 1},
		{"por estado", movimientos.Filtros{Estado: movimientos.EstadoPendiente}, 1},
		{"por medio", movimientos.Filtros{MedioPagoID: e.efectivo}, 2},
		{"desde", movimientos.Filtros{Desde: "2026-09-15"}, 2},
		{"hasta", movimientos.Filtros{Hasta: "2026-09-15"}, 2},
		{"rango", movimientos.Filtros{Desde: "2026-09-10", Hasta: "2026-09-16"}, 1},
		{"texto en descripción", movimientos.Filtros{Texto: "arriendo"}, 1},
		{"texto en la persona", movimientos.Filtros{Texto: "pedro"}, 1},
		{"texto sin resultados", movimientos.Filtros{Texto: "zzzz"}, 0},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			lista, total, err := e.store.Listar(ctx, e.usuarioID, c.filtros)
			if err != nil {
				t.Fatalf("Listar: %v", err)
			}
			if total != c.esperado || len(lista) != c.esperado {
				t.Errorf("total=%d len=%d, se esperaban %d", total, len(lista), c.esperado)
			}
		})
	}
}

// El texto de búsqueda va como parámetro, nunca pegado dentro del SQL.
func TestBusquedaNoEsVulnerableAInyeccion(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "1000", Fecha: "2026-09-01", Descripcion: "normal", MedioPagoID: ptr(e.efectivo)})

	ataques := []string{
		"'; DROP TABLE movimientos; --",
		"' OR '1'='1",
		"%' OR 1=1 --",
	}

	for _, ataque := range ataques {
		if _, _, err := e.store.Listar(ctx, e.usuarioID, movimientos.Filtros{Texto: ataque}); err != nil {
			t.Fatalf("la búsqueda falló con %q: %v", ataque, err)
		}
	}

	// Lo importante: la tabla sigue viva y con su fila.
	_, total, err := e.store.Listar(ctx, e.usuarioID, movimientos.Filtros{})
	if err != nil {
		t.Fatalf("Listar tras los ataques: %v", err)
	}
	if total != 1 {
		t.Fatalf("quedaron %d movimientos, se esperaba 1: la inyección hizo algo", total)
	}
}

// No se puede borrar una categoría que tiene movimientos: se perderían
// registros de dinero.
func TestNoSeBorraCategoriaConMovimientos(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "1000", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})

	err := categorias.NewStore(e.pool).Eliminar(ctx, e.usuarioID, e.categoria)
	if err == nil {
		t.Fatal("se borró una categoría que tenía movimientos")
	}
}

func TestNoSeBorraMedioEnUso(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoPague, Monto: "1000", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})

	if err := medios.NewStore(e.pool).Eliminar(ctx, e.usuarioID, e.efectivo); err == nil {
		t.Fatal("se borró un medio de pago que estaba en uso")
	}
}

// Los centavos no se pueden perder: es el motivo de usar NUMERIC y no float.
func TestLosCentavosNoSePierden(t *testing.T) {
	e := nuevoEntorno(t)

	// 0.1 + 0.2 en float da 0.30000000000000004. Aquí debe dar 0.30 exacto.
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "0.10", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})
	e.crear(t, movimientos.Datos{Tipo: movimientos.TipoRecibi, Monto: "0.20", Fecha: "2026-09-01", MedioPagoID: ptr(e.efectivo)})

	r := e.resumen(t)
	if r.Totales.Recibido != "0.30" {
		t.Errorf("0.10 + 0.20 = %s, se esperaba 0.30 exacto", r.Totales.Recibido)
	}
}

// ---------------------------------------------------------------------------
// Fecha de cobro de los préstamos
// ---------------------------------------------------------------------------

func TestPrestamoConFechaDeCobro(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	m := e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "200000", Fecha: "2026-09-08",
		AQuien: ptr("Carlos"), Estado: ptr(movimientos.EstadoPendiente),
		MedioPagoID: ptr(e.efectivo), CobrarEl: ptr("2026-09-20"),
	})
	if m.CobrarEl == nil || *m.CobrarEl != "2026-09-20" {
		t.Fatalf("cobrar_el = %v", m.CobrarEl)
	}

	// Se ve en "Te deben", con la fecha más cercana de esa persona.
	e.crear(t, movimientos.Datos{
		Tipo: movimientos.TipoPreste, Monto: "50000", Fecha: "2026-09-09",
		AQuien: ptr("Carlos"), Estado: ptr(movimientos.EstadoPendiente),
		MedioPagoID: ptr(e.efectivo), CobrarEl: ptr("2026-09-12"),
	})
	r := e.resumen(t)
	if len(r.Contrapartes) != 1 || r.Contrapartes[0].ProximaFecha == nil || *r.Contrapartes[0].ProximaFecha != "2026-09-12" {
		t.Errorf("contrapartes = %+v", r.Contrapartes)
	}

	// Al volverlo "pagué", la fecha se va con a_quien y estado.
	pague, err := e.store.Actualizar(ctx, e.usuarioID, m.ID, movimientos.Datos{
		CategoriaID: e.categoria, Tipo: movimientos.TipoPague, Monto: "200000",
		Fecha: "2026-09-08", MedioPagoID: ptr(e.efectivo),
	})
	if err != nil {
		t.Fatalf("actualizando: %v", err)
	}
	if pague.CobrarEl != nil {
		t.Errorf("un gasto quedó con fecha de cobro: %v", *pague.CobrarEl)
	}
}

// La base no deja cobrar antes de prestar ni poner fecha de cobro a un gasto,
// aunque la validación se salte.
func TestLaBaseCuidaLaFechaDeCobro(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	_, err := e.store.Crear(ctx, e.usuarioID, movimientos.Datos{
		CategoriaID: e.categoria, Tipo: movimientos.TipoPreste, Monto: "1000", Fecha: "2026-09-10",
		AQuien: ptr("Ana"), Estado: ptr(movimientos.EstadoPendiente),
		MedioPagoID: ptr(e.efectivo), CobrarEl: ptr("2026-09-01"),
	})
	if !errors.Is(err, movimientos.ErrCobroAntes) {
		t.Errorf("cobrar antes de prestar: err = %v", err)
	}

	_, err = e.store.Crear(ctx, e.usuarioID, movimientos.Datos{
		CategoriaID: e.categoria, Tipo: movimientos.TipoPague, Monto: "1000", Fecha: "2026-09-10",
		MedioPagoID: ptr(e.efectivo), CobrarEl: ptr("2026-09-20"),
	})
	if err == nil {
		t.Error("un gasto quedó con fecha de cobro")
	}
}
