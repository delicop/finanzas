package avisos_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"finanzas/internal/admin"
	"finanzas/internal/auth"
	"finanzas/internal/avisos"
	"finanzas/internal/categorias"
	"finanzas/internal/db"
	"finanzas/internal/httpx"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
	"finanzas/internal/recurrentes"
	"finanzas/internal/suscripciones"
)

// Contra un Postgres de verdad, porque lo que se prueba es el SQL que suma las
// cifras del aviso y el índice único que impide repetirlo.
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

// El miércoles desde el que se evalúa todo: su semana pasada va del 7 al 13.
var miercoles = time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)

type entorno struct {
	pool        *sql.DB
	store       *avisos.Store
	generador   *avisos.Generador
	movimientos *movimientos.Store
	ana         int64
	categoria   int64
	efectivo    int64
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := abrirBasePrueba(t)
	ctx := context.Background()

	ana := crearUsuario(t, pool, "Ana", auth.RolUsuario)

	categoria, err := categorias.NewStore(pool).Crear(ctx, ana, "Negocio")
	if err != nil {
		t.Fatalf("creando categoría: %v", err)
	}
	efectivo, err := medios.NewStore(pool).Crear(ctx, ana, "Efectivo")
	if err != nil {
		t.Fatalf("creando medio: %v", err)
	}

	// Plata vieja en el efectivo para que las pruebas puedan gastar: ningun
	// medio puede quedar en negativo. Con fecha del 2000 para que no caiga en
	// ninguna ventana que las pruebas miren.
	if _, err := pool.ExecContext(ctx, `
		INSERT INTO movimientos (usuario_id, categoria_id, tipo, monto, fecha, descripcion, medio_pago_id)
		VALUES ($1, $2, 'recibi', 100000000, '2000-01-01', 'Saldo inicial', $3)`,
		ana, categoria.ID, efectivo.ID); err != nil {
		t.Fatalf("fondeando el efectivo: %v", err)
	}

	store := avisos.NewStore(pool)

	return &entorno{
		pool:        pool,
		store:       store,
		generador:   avisos.NuevoGenerador(store, suscripciones.NewStore(pool), recurrentes.NewStore(pool), nil, nil),
		movimientos: movimientos.NewStore(pool),
		ana:         ana,
		categoria:   categoria.ID,
		efectivo:    efectivo.ID,
	}
}

var contador atomic.Int64

// Cada prueba crea sus usuarios y borra solo los suyos: los paquetes corren en
// paralelo contra la misma base y una limpieza global le arrancaría los datos
// al de al lado.
func crearUsuario(t *testing.T, pool *sql.DB, nombre, rol string) int64 {
	t.Helper()
	ctx := context.Background()

	hash, err := auth.HashPassword("ClaveDePrueba123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	email := fmt.Sprintf("%s-%d-%d@prueba.local", strings.ToLower(nombre), time.Now().UnixNano(), contador.Add(1))
	usuario, err := auth.NewStore(pool).Crear(ctx, email, nombre, rol, hash)
	if err != nil {
		t.Fatalf("creando usuario: %v", err)
	}

	t.Cleanup(func() {
		limpieza := context.Background()
		for _, tabla := range []string{"movimientos", "categorias", "medios_pago"} {
			if _, err := pool.ExecContext(limpieza, "DELETE FROM "+tabla+" WHERE usuario_id = $1", usuario.ID); err != nil {
				t.Errorf("limpiando %s: %v", tabla, err)
			}
		}
		if _, err := pool.ExecContext(limpieza, "DELETE FROM usuarios WHERE id = $1", usuario.ID); err != nil {
			t.Errorf("limpiando el usuario de prueba: %v", err)
		}
	})

	return usuario.ID
}

func (e *entorno) crearMovimiento(t *testing.T, tipo, monto, fecha, descripcion string) *movimientos.Movimiento {
	t.Helper()

	datos := movimientos.Datos{
		CategoriaID: e.categoria,
		MedioPagoID: &e.efectivo,
		Tipo:        tipo,
		Monto:       monto,
		Fecha:       fecha,
		Descripcion: descripcion,
	}
	if tipo == movimientos.TipoPreste {
		aQuien, estado := "Juan", movimientos.EstadoPendiente
		datos.AQuien, datos.Estado = &aQuien, &estado
	}

	m, err := e.movimientos.Crear(context.Background(), e.ana, datos)
	if err != nil {
		t.Fatalf("creando movimiento: %v", err)
	}
	return m
}

func (e *entorno) avisosDe(t *testing.T, usuarioID int64) []avisos.Aviso {
	t.Helper()

	lista, _, err := e.store.Listar(context.Background(), usuarioID)
	if err != nil {
		t.Fatalf("listando avisos: %v", err)
	}
	return lista
}

func (e *entorno) correr(t *testing.T, ahora time.Time) {
	t.Helper()

	if _, err := e.generador.Correr(context.Background(), ahora); err != nil {
		t.Fatalf("generando avisos: %v", err)
	}
}

// --------------------------------------------------------------------------

func TestResumenSemanalConLasCifrasDeLaSemana(t *testing.T) {
	e := nuevoEntorno(t)

	// Dentro de la semana pasada (7 al 13)...
	e.crearMovimiento(t, movimientos.TipoRecibi, "900000", "2026-09-10", "pago cliente")
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-11", "almuerzo")
	// ...y uno de esta semana, que NO debe contar.
	e.crearMovimiento(t, movimientos.TipoPague, "7777", "2026-09-15", "café")

	e.correr(t, miercoles)

	lista := e.avisosDe(t, e.ana)
	if len(lista) != 1 {
		t.Fatalf("avisos = %d, se esperaba 1: %+v", len(lista), lista)
	}

	aviso := lista[0]
	if aviso.Tipo != avisos.TipoResumenSemanal {
		t.Errorf("tipo = %q", aviso.Tipo)
	}
	if !strings.Contains(aviso.Titulo, "$45.000") {
		t.Errorf("título = %q", aviso.Titulo)
	}
	for _, esperado := range []string{"7 al 13 de septiembre", "2 movimientos", "$900.000", "$45.000", "Negocio"} {
		if !strings.Contains(aviso.Cuerpo, esperado) {
			t.Errorf("el cuerpo no menciona %q: %s", esperado, aviso.Cuerpo)
		}
	}
	if strings.Contains(aviso.Cuerpo, "7.777") {
		t.Errorf("se coló un movimiento de otra semana: %s", aviso.Cuerpo)
	}
}

// Sin movimientos no hay nada que contar. De paso, esto deja fuera al
// administrador, que no lleva finanzas propias.
func TestSemanaSinMovimientosNoGeneraAviso(t *testing.T) {
	e := nuevoEntorno(t)

	e.correr(t, miercoles)

	if lista := e.avisosDe(t, e.ana); len(lista) != 0 {
		t.Errorf("avisos = %+v, se esperaba ninguno", lista)
	}
}

// La tarea corre cada pocas horas y vuelve a evaluarlo todo: lo que impide
// cuatro resúmenes al día es el índice único, no que la tarea lleve la cuenta.
func TestCorrerDosVecesNoRepiteElAviso(t *testing.T) {
	e := nuevoEntorno(t)
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-10", "almuerzo")

	e.correr(t, miercoles)
	e.correr(t, miercoles.Add(6*time.Hour))
	e.correr(t, miercoles.Add(12*time.Hour))

	if lista := e.avisosDe(t, e.ana); len(lista) != 1 {
		t.Errorf("avisos = %d, se esperaba 1", len(lista))
	}
}

// Pero la semana siguiente sí trae su resumen: la clave cambia.
func TestCadaSemanaTieneSuResumen(t *testing.T) {
	e := nuevoEntorno(t)
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-10", "almuerzo")
	e.crearMovimiento(t, movimientos.TipoPague, "30000", "2026-09-16", "mercado")

	e.correr(t, miercoles)                  // resumen del 7 al 13
	e.correr(t, miercoles.AddDate(0, 0, 7)) // resumen del 14 al 20

	if lista := e.avisosDe(t, e.ana); len(lista) != 2 {
		t.Errorf("avisos = %d, se esperaban 2", len(lista))
	}
}

func TestPrestamoViejoGeneraRecordatorio(t *testing.T) {
	e := nuevoEntorno(t)

	// La consulta compara contra la fecha de hoy de Postgres, así que el
	// préstamo se fecha hacia atrás desde hoy y no desde el miércoles fijo.
	hace40dias := time.Now().AddDate(0, 0, -40).Format("2006-01-02")
	e.crearMovimiento(t, movimientos.TipoPreste, "200000", hace40dias, "préstamo")

	e.correr(t, miercoles)

	var recordatorio *avisos.Aviso
	for _, a := range e.avisosDe(t, e.ana) {
		if a.Tipo == avisos.TipoPrestamosPendientes {
			recordatorio = &a
		}
	}
	if recordatorio == nil {
		t.Fatal("no se generó el recordatorio del préstamo")
	}
	if !strings.Contains(recordatorio.Titulo, "$200.000") {
		t.Errorf("título = %q", recordatorio.Titulo)
	}
	if !strings.Contains(recordatorio.Cuerpo, "Juan") {
		t.Errorf("el cuerpo no dice de quién es: %s", recordatorio.Cuerpo)
	}
}

// Un préstamo de la semana pasada todavía no es un olvido.
func TestPrestamoRecienteNoMolesta(t *testing.T) {
	e := nuevoEntorno(t)

	hace5dias := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	e.crearMovimiento(t, movimientos.TipoPreste, "200000", hace5dias, "préstamo")

	e.correr(t, miercoles)

	for _, a := range e.avisosDe(t, e.ana) {
		if a.Tipo == avisos.TipoPrestamosPendientes {
			t.Errorf("recordatorio prematuro: %s", a.Cuerpo)
		}
	}
}

// Los avisos son de cada quien, como todo en esta app.
func TestLosAvisosSonDeCadaQuien(t *testing.T) {
	e := nuevoEntorno(t)
	beto := crearUsuario(t, e.pool, "Beto", auth.RolUsuario)

	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-10", "almuerzo")
	e.correr(t, miercoles)

	if len(e.avisosDe(t, e.ana)) != 1 {
		t.Fatal("Ana no recibió su resumen")
	}
	if lista := e.avisosDe(t, beto); len(lista) != 0 {
		t.Errorf("Beto ve avisos que no son suyos: %+v", lista)
	}
}

func TestMarcarLeidos(t *testing.T) {
	e := nuevoEntorno(t)
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-10", "almuerzo")
	e.correr(t, miercoles)

	_, sinLeer, err := e.store.Listar(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("listando: %v", err)
	}
	if sinLeer != 1 {
		t.Fatalf("sin leer = %d, se esperaba 1", sinLeer)
	}

	if err := e.store.MarcarLeidos(context.Background(), e.ana); err != nil {
		t.Fatalf("marcando leídos: %v", err)
	}

	lista, sinLeer, err := e.store.Listar(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("listando: %v", err)
	}
	if sinLeer != 0 {
		t.Errorf("sin leer = %d después de marcarlos", sinLeer)
	}
	if lista[0].LeidaEn == nil {
		t.Error("el aviso sigue sin fecha de lectura")
	}
}

// El aviso de cobros es del dueño del servidor, y no sale el día 1: "te faltan
// todos" al empezar el mes no es información.
func TestCobrosDelMesSoloParaElAdminYNoElDiaUno(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	jefe := crearUsuario(t, e.pool, "Jefe", auth.RolAdmin)

	plan, err := suscripciones.NewStore(e.pool).CrearPlan(ctx, suscripciones.DatosPlan{
		Nombre:        fmt.Sprintf("Plan %d", time.Now().UnixNano()),
		PrecioMensual: "50000",
		Activo:        true,
	})
	if err != nil {
		t.Fatalf("creando plan: %v", err)
	}
	t.Cleanup(func() {
		limpieza := context.Background()
		// Primero se le quita el plan a quien lo tenga: la llave foránea de
		// usuarios.plan_id es RESTRICT, igual que en la app.
		if _, err := e.pool.ExecContext(limpieza, "UPDATE usuarios SET plan_id = NULL WHERE plan_id = $1", plan.ID); err != nil {
			t.Errorf("desasignando el plan: %v", err)
		}
		if _, err := e.pool.ExecContext(limpieza, "DELETE FROM planes WHERE id = $1", plan.ID); err != nil {
			t.Errorf("limpiando el plan: %v", err)
		}
	})

	// Ana es la clienta que no ha pagado este mes.
	if _, err := admin.NewStore(e.pool).AsignarPlan(ctx, e.ana, &plan.ID, "mensual"); err != nil {
		t.Fatalf("asignando plan: %v", err)
	}

	primeroDelMes := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	e.correr(t, primeroDelMes)
	if lista := e.avisosDe(t, jefe); len(lista) != 0 {
		t.Errorf("el día 1 ya avisó: %+v", lista)
	}

	e.correr(t, miercoles)

	lista := e.avisosDe(t, jefe)
	if len(lista) != 1 || lista[0].Tipo != avisos.TipoCobrosDelMes {
		t.Fatalf("avisos del admin = %+v", lista)
	}
	if !strings.Contains(lista[0].Cuerpo, "Ana") {
		t.Errorf("el aviso no dice a quién falta cobrarle: %s", lista[0].Cuerpo)
	}

	// Y a la clienta no le llega el aviso del negocio: ese no es su asunto.
	for _, a := range e.avisosDe(t, e.ana) {
		if a.Tipo == avisos.TipoCobrosDelMes {
			t.Error("a un cliente le llegó el aviso de cobros del dueño")
		}
	}
}

// --------------------------------------------------------------------------
// El endpoint
// --------------------------------------------------------------------------

func (e *entorno) pedirAvisos(t *testing.T, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	res := httptest.NewRecorder()
	avisos.NewHandler(e.store).Rutas().ServeHTTP(res, req)
	return res
}

func TestElEndpointDevuelveLosAvisosDelUsuario(t *testing.T) {
	e := nuevoEntorno(t)
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-10", "almuerzo")
	e.correr(t, miercoles)

	res := e.pedirAvisos(t, httpx.ConUsuarioID(context.Background(), e.ana))
	if res.Code != http.StatusOK {
		t.Fatalf("código = %d — %s", res.Code, res.Body.String())
	}

	var cuerpo struct {
		Avisos  []avisos.Aviso `json:"avisos"`
		SinLeer int            `json:"sin_leer"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &cuerpo); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(cuerpo.Avisos) != 1 || cuerpo.SinLeer != 1 {
		t.Errorf("avisos = %d, sin leer = %d", len(cuerpo.Avisos), cuerpo.SinLeer)
	}
}

// Un admin revisando la cuenta de un cliente ve sus movimientos —es parte de
// llevar el negocio— pero no los avisos que le llegaron a esa persona.
func TestLosAvisosNoSeLeenEnModoVerComo(t *testing.T) {
	e := nuevoEntorno(t)
	beto := crearUsuario(t, e.pool, "Beto", auth.RolAdmin)

	e.crearMovimiento(t, movimientos.TipoPague, "45000", "2026-09-10", "almuerzo")
	e.correr(t, miercoles)

	// Así queda el context después del middleware VerComo: el id del
	// observado, más la marca de quién está mirando.
	ctx := httpx.ConObservador(httpx.ConUsuarioID(context.Background(), e.ana), beto)

	if res := e.pedirAvisos(t, ctx); res.Code != http.StatusForbidden {
		t.Errorf("código = %d, se esperaba 403 — %s", res.Code, res.Body.String())
	}
}
