package suscripciones_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"finanzas/internal/admin"
	"finanzas/internal/auth"
	"finanzas/internal/db"
	"finanzas/internal/suscripciones"
)

// Contra un Postgres de verdad: lo que se prueba es el SQL (la restriccion que
// impide solapar pagos y las sumas del tablero). Se saltan sin base de prueba.
//
// El tablero suma TODOS los clientes del servidor, y la base de prueba la
// comparten otros paquetes. Por eso las cifras globales se comparan como
// diferencias (antes/despues) y no como valores absolutos.
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

var contador atomic.Int64

func unico(prefijo string) string {
	return fmt.Sprintf("%s-%d-%d", prefijo, time.Now().UnixNano(), contador.Add(1))
}

type entorno struct {
	pool    *sql.DB
	store   *suscripciones.Store
	admin   *admin.Store
	cliente int64
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := abrirBasePrueba(t)
	return &entorno{
		pool:    pool,
		store:   suscripciones.NewStore(pool),
		admin:   admin.NewStore(pool),
		cliente: crearCliente(t, pool),
	}
}

func crearCliente(t *testing.T, pool *sql.DB) int64 {
	t.Helper()
	hash, err := auth.HashPassword("ClaveDePrueba123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := auth.NewStore(pool).Crear(context.Background(), unico("cliente")+"@prueba.local",
		"Cliente", auth.RolUsuario, hash)
	if err != nil {
		t.Fatalf("creando cliente: %v", err)
	}
	t.Cleanup(func() {
		// Los pagos quedan huérfanos (SET NULL) y no chocan con nada, pero se
		// borran igual para no ensuciar la base de prueba.
		limpieza := context.Background()
		_, _ = pool.ExecContext(limpieza, "DELETE FROM pagos WHERE usuario_id = $1", u.ID)
		_, _ = pool.ExecContext(limpieza, "DELETE FROM usuarios WHERE id = $1", u.ID)
	})
	return u.ID
}

func (e *entorno) crearPlan(t *testing.T, d suscripciones.DatosPlan) *suscripciones.Plan {
	t.Helper()
	d.Nombre = unico(d.Nombre)
	d.Activo = true
	p, err := e.store.CrearPlan(context.Background(), d)
	if err != nil {
		t.Fatalf("creando plan: %v", err)
	}
	t.Cleanup(func() {
		limpieza := context.Background()
		_, _ = e.pool.ExecContext(limpieza, "UPDATE usuarios SET plan_id = NULL WHERE plan_id = $1", p.ID)
		_, _ = e.pool.ExecContext(limpieza, "DELETE FROM planes WHERE id = $1", p.ID)
	})
	return p
}

func (e *entorno) asignar(t *testing.T, planID int64, ciclo string) {
	t.Helper()
	if _, err := e.admin.AsignarPlan(context.Background(), e.cliente, &planID, ciclo); err != nil {
		t.Fatalf("asignando plan %s: %v", ciclo, err)
	}
}

func (e *entorno) pagar(periodo string) (*suscripciones.Pago, error) {
	return e.store.RegistrarPago(context.Background(), e.cliente, periodo+"-01", "", periodo+"-05", "")
}

func (e *entorno) pendienteDe(t *testing.T, periodo string) *suscripciones.Pendiente {
	t.Helper()
	lista, err := e.store.Pendientes(context.Background(), periodo+"-01")
	if err != nil {
		t.Fatalf("pendientes: %v", err)
	}
	for _, p := range lista {
		if p.UsuarioID == e.cliente {
			return &p
		}
	}
	return nil
}

// ---------------------------------------------------------------------------

func TestPlanGuardaPrecioAnualEIA(t *testing.T) {
	e := nuevoEntorno(t)

	p := e.crearPlan(t, suscripciones.DatosPlan{
		Nombre: "Pro", PrecioMensual: "20000", PrecioAnual: "200000", IncluyeIA: true,
	})
	if p.PrecioAnual != "200000.00" || !p.IncluyeIA {
		t.Fatalf("se guardó %+v", p)
	}

	sinAnual := e.crearPlan(t, suscripciones.DatosPlan{Nombre: "Basico", PrecioMensual: "10000"})
	if sinAnual.PrecioAnual != "" || sinAnual.IncluyeIA {
		t.Fatalf("un plan sin anual ni IA quedó como %+v", sinAnual)
	}
}

// Un cliente solo puede pagar por año si su plan tiene precio anual.
func TestAnualSoloSiElPlanLoOfrece(t *testing.T) {
	e := nuevoEntorno(t)
	basico := e.crearPlan(t, suscripciones.DatosPlan{Nombre: "Basico", PrecioMensual: "10000"})

	_, err := e.admin.AsignarPlan(context.Background(), e.cliente, &basico.ID, "anual")
	if !errors.Is(err, admin.ErrPlanSinAnual) {
		t.Fatalf("se esperaba ErrPlanSinAnual, se obtuvo %v", err)
	}
}

// El pago anual cobra el precio anual y cubre doce meses: ni once ni trece.
func TestElPagoAnualCubreDoceMeses(t *testing.T) {
	e := nuevoEntorno(t)
	pro := e.crearPlan(t, suscripciones.DatosPlan{
		Nombre: "Pro", PrecioMensual: "20000", PrecioAnual: "200000",
	})
	e.asignar(t, pro.ID, suscripciones.CicloAnual)

	pago, err := e.pagar("2026-03")
	if err != nil {
		t.Fatalf("registrando el pago anual: %v", err)
	}
	if pago.Monto != "200000.00" || pago.Ciclo != "anual" {
		t.Errorf("pago = %+v, se esperaba 200000.00 anual", pago)
	}
	if pago.Periodo != "2026-03" || pago.CubreHasta != "2027-02" {
		t.Errorf("cubre %s a %s, se esperaba 2026-03 a 2027-02", pago.Periodo, pago.CubreHasta)
	}

	for _, mes := range []string{"2026-03", "2026-08", "2027-02"} {
		if p := e.pendienteDe(t, mes); p != nil {
			t.Errorf("%s debería estar cubierto y figura pendiente", mes)
		}
	}
	for _, mes := range []string{"2026-02", "2027-03"} {
		p := e.pendienteDe(t, mes)
		if p == nil {
			t.Errorf("%s no está cubierto y no figura pendiente", mes)
			continue
		}
		if p.Monto != "200000.00" || p.Ciclo != "anual" {
			t.Errorf("%s: pendiente %+v, se esperaba el precio anual", mes, p)
		}
	}
}

// Dos pagos no pueden cubrir el mismo mes. Lo impide la base, así que un doble
// clic o un pago mensual metido en medio del año no inflan los ingresos.
func TestNoSeSolapanLosPagos(t *testing.T) {
	e := nuevoEntorno(t)
	pro := e.crearPlan(t, suscripciones.DatosPlan{
		Nombre: "Pro", PrecioMensual: "20000", PrecioAnual: "200000",
	})
	e.asignar(t, pro.ID, suscripciones.CicloAnual)

	if _, err := e.pagar("2026-03"); err != nil {
		t.Fatalf("primer pago: %v", err)
	}

	// Otro anual que empieza dentro del primero.
	if _, err := e.pagar("2026-09"); !errors.Is(err, suscripciones.ErrPagoDuplicado) {
		t.Errorf("anual encima de anual: se esperaba ErrPagoDuplicado, se obtuvo %v", err)
	}

	// Un mensual en un mes que el anual ya cubre.
	e.asignar(t, pro.ID, suscripciones.CicloMensual)
	if _, err := e.pagar("2026-06"); !errors.Is(err, suscripciones.ErrPagoDuplicado) {
		t.Errorf("mensual dentro del anual: se esperaba ErrPagoDuplicado, se obtuvo %v", err)
	}

	// Justo al terminar el año, sí.
	if _, err := e.pagar("2027-03"); err != nil {
		t.Errorf("el mes siguiente al año debería poder pagarse: %v", err)
	}
}

func TestDobleClicEnUnMesMensual(t *testing.T) {
	e := nuevoEntorno(t)
	basico := e.crearPlan(t, suscripciones.DatosPlan{Nombre: "Basico", PrecioMensual: "10000"})
	e.asignar(t, basico.ID, suscripciones.CicloMensual)

	if _, err := e.pagar("2026-04"); err != nil {
		t.Fatalf("primer pago: %v", err)
	}
	if _, err := e.pagar("2026-04"); !errors.Is(err, suscripciones.ErrPagoDuplicado) {
		t.Errorf("segundo pago del mismo mes: se esperaba ErrPagoDuplicado, se obtuvo %v", err)
	}
}

// En el tablero, un cliente anual aporta la doceava parte al ingreso mensual,
// y su pago cuenta entero como cobrado en el mes en que se hizo.
func TestTableroConUnClienteAnual(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()
	const mes = "2031-01" // un mes lejano, donde la base compartida no tiene pagos
	periodo := mes + "-01"

	antes, err := e.store.Resumen(ctx, periodo)
	if err != nil {
		t.Fatalf("resumen: %v", err)
	}

	pro := e.crearPlan(t, suscripciones.DatosPlan{
		Nombre: "Pro", PrecioMensual: "20000", PrecioAnual: "120000",
	})
	e.asignar(t, pro.ID, suscripciones.CicloAnual)

	sinPagar, err := e.store.Resumen(ctx, periodo)
	if err != nil {
		t.Fatalf("resumen: %v", err)
	}
	if d := restar(t, e, sinPagar.Esperado, antes.Esperado); d != "10000.00" {
		t.Errorf("el anual de 120.000 sumó %s al ingreso mensual, se esperaba 10000.00", d)
	}
	if d := restar(t, e, sinPagar.Pendiente, antes.Pendiente); d != "120000.00" {
		t.Errorf("sin pagar, el pendiente subió %s, se esperaba el año entero (120000.00)", d)
	}

	if _, err := e.pagar(mes); err != nil {
		t.Fatalf("pagando: %v", err)
	}

	pagado, err := e.store.Resumen(ctx, periodo)
	if err != nil {
		t.Fatalf("resumen: %v", err)
	}
	if d := restar(t, e, pagado.Cobrado, antes.Cobrado); d != "120000.00" {
		t.Errorf("el cobrado del mes subió %s, se esperaba 120000.00", d)
	}
	if d := restar(t, e, pagado.Pendiente, antes.Pendiente); d != "0.00" {
		t.Errorf("pagado el año, el pendiente sigue %s por encima", d)
	}
	if pagado.ClientesPagaron != antes.ClientesPagaron+1 {
		t.Errorf("clientes al día: %d, se esperaba %d", pagado.ClientesPagaron, antes.ClientesPagaron+1)
	}

	// Un mes después el cliente sigue al día, sin cobro nuevo.
	despues, err := e.store.Resumen(ctx, "2031-02-01")
	if err != nil {
		t.Fatalf("resumen: %v", err)
	}
	for _, p := range despues.Pendientes {
		if p.UsuarioID == e.cliente {
			t.Error("el mes siguiente al pago anual figura como pendiente")
		}
	}
}

// Quitarle el precio anual a un plan que alguien paga por año dejaría a ese
// cliente sin precio que cobrarle.
func TestNoSeQuitaElAnualSiAlguienLoPaga(t *testing.T) {
	e := nuevoEntorno(t)
	pro := e.crearPlan(t, suscripciones.DatosPlan{
		Nombre: "Pro", PrecioMensual: "20000", PrecioAnual: "200000",
	})
	e.asignar(t, pro.ID, suscripciones.CicloAnual)

	_, err := e.store.ActualizarPlan(context.Background(), pro.ID, suscripciones.DatosPlan{
		Nombre: pro.Nombre, PrecioMensual: "20000", PrecioAnual: "", Activo: true,
	})
	if !errors.Is(err, suscripciones.ErrPlanConAnuales) {
		t.Fatalf("se esperaba ErrPlanConAnuales, se obtuvo %v", err)
	}

	// Pasado a mensual, ya se puede.
	e.asignar(t, pro.ID, suscripciones.CicloMensual)
	if _, err := e.store.ActualizarPlan(context.Background(), pro.ID, suscripciones.DatosPlan{
		Nombre: pro.Nombre, PrecioMensual: "20000", PrecioAnual: "", Activo: true,
	}); err != nil {
		t.Fatalf("sin clientes anuales debería poder quitarse: %v", err)
	}
}

// La IA sale del plan: sin plan no hay, y el cambio en el plan se ve al instante.
func TestLaIASaleDelPlan(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()
	usuarios := auth.NewStore(e.pool)

	if ia, err := usuarios.TieneIA(ctx, e.cliente); err != nil || ia {
		t.Fatalf("sin plan: ia=%v err=%v, se esperaba false", ia, err)
	}

	pro := e.crearPlan(t, suscripciones.DatosPlan{Nombre: "Pro", PrecioMensual: "20000", IncluyeIA: true})
	e.asignar(t, pro.ID, suscripciones.CicloMensual)
	if ia, _ := usuarios.TieneIA(ctx, e.cliente); !ia {
		t.Error("con un plan con IA, TieneIA dio false")
	}

	if _, err := e.store.ActualizarPlan(ctx, pro.ID, suscripciones.DatosPlan{
		Nombre: pro.Nombre, PrecioMensual: "20000", IncluyeIA: false, Activo: true,
	}); err != nil {
		t.Fatalf("quitando la IA: %v", err)
	}
	if ia, _ := usuarios.TieneIA(ctx, e.cliente); ia {
		t.Error("tras quitarle la IA al plan, TieneIA sigue en true")
	}
}

// restar hace a - b en Postgres: el dinero no pasa por float ni en las pruebas.
func restar(t *testing.T, e *entorno, a, b string) string {
	t.Helper()
	var r string
	if err := e.pool.QueryRow(`SELECT ($1::numeric - $2::numeric)::numeric(14,2)::text`, a, b).Scan(&r); err != nil {
		t.Fatalf("restando %s - %s: %v", a, b, err)
	}
	return r
}
