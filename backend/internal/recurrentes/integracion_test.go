package recurrentes_test

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

	"finanzas/internal/auth"
	"finanzas/internal/categorias"
	"finanzas/internal/db"
	"finanzas/internal/httpx"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
	"finanzas/internal/recurrentes"
)

// Contra un Postgres de verdad: lo que se prueba es el corte por fecha entre
// "esto ya te tocaba" y "esto se viene", que vive en el SQL.
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

type entorno struct {
	pool      *sql.DB
	store     *recurrentes.Store
	handler   *recurrentes.Handler
	ana       int64
	categoria int64
	efectivo  int64
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := abrirBasePrueba(t)
	ctx := context.Background()

	ana := crearUsuario(t, pool, "Ana")

	categoria, err := categorias.NewStore(pool).Crear(ctx, ana, "Personal")
	if err != nil {
		t.Fatalf("creando categoría: %v", err)
	}
	efectivo, err := medios.NewStore(pool).Crear(ctx, ana, "Efectivo")
	if err != nil {
		t.Fatalf("creando medio: %v", err)
	}

	store := recurrentes.NewStore(pool)

	return &entorno{
		pool:      pool,
		store:     store,
		handler:   recurrentes.NewHandler(store, movimientos.NewStore(pool)),
		ana:       ana,
		categoria: categoria.ID,
		efectivo:  efectivo.ID,
	}
}

// fondear mete plata en el efectivo para poder confirmar gastos: ningun medio
// puede quedar en negativo.
func (e *entorno) fondear(t *testing.T) {
	t.Helper()
	_, err := movimientos.NewStore(e.pool).Crear(context.Background(), e.ana, movimientos.Datos{
		CategoriaID: e.categoria, Tipo: movimientos.TipoRecibi, Monto: "10000000",
		Fecha: "2026-01-01", MedioPagoID: &e.efectivo,
	})
	if err != nil {
		t.Fatalf("fondeando el efectivo: %v", err)
	}
}

var contador atomic.Int64

// Cada prueba crea sus usuarios y borra solo los suyos: los paquetes corren en
// paralelo contra la misma base y una limpieza global le arrancaría los datos
// al de al lado.
func crearUsuario(t *testing.T, pool *sql.DB, nombre string) int64 {
	t.Helper()
	ctx := context.Background()

	hash, err := auth.HashPassword("ClaveDePrueba123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	email := fmt.Sprintf("%s-%d-%d@prueba.local", strings.ToLower(nombre), time.Now().UnixNano(), contador.Add(1))
	usuario, err := auth.NewStore(pool).Crear(ctx, email, nombre, auth.RolUsuario, hash)
	if err != nil {
		t.Fatalf("creando usuario: %v", err)
	}

	t.Cleanup(func() {
		limpieza := context.Background()
		for _, tabla := range []string{"recurrentes_ocurrencias", "recurrentes", "movimientos", "categorias", "medios_pago"} {
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

// crear pasa por el handler, que es donde vive la preparación de ocurrencias:
// crear el recurrente y dejar listas sus fechas son la misma acción.
func (e *entorno) crear(t *testing.T, cuerpo map[string]any) recurrentes.Recurrente {
	t.Helper()
	return e.peticion(t, http.MethodPost, "/", cuerpo, http.StatusCreated)
}

func (e *entorno) actualizar(t *testing.T, id int64, cuerpo map[string]any) recurrentes.Recurrente {
	t.Helper()
	return e.peticion(t, http.MethodPut, fmt.Sprintf("/%d", id), cuerpo, http.StatusOK)
}

func (e *entorno) peticion(t *testing.T, metodo, ruta string, cuerpo map[string]any, espera int) recurrentes.Recurrente {
	t.Helper()

	datos, err := json.Marshal(cuerpo)
	if err != nil {
		t.Fatalf("armando el cuerpo: %v", err)
	}

	req := httptest.NewRequest(metodo, ruta, strings.NewReader(string(datos)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), e.ana))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	if res.Code != espera {
		t.Fatalf("%s %s: %d — %s", metodo, ruta, res.Code, res.Body.String())
	}

	var rec recurrentes.Recurrente
	if err := json.Unmarshal(res.Body.Bytes(), &rec); err != nil {
		t.Fatalf("respuesta ilegible: %v — %s", err, res.Body.String())
	}
	return rec
}

// listas lee lo que ve el resumen: lo vencido y lo que viene.
func (e *entorno) listas(t *testing.T) (pendientes, proximas []recurrentes.Ocurrencia) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/pendientes", nil).
		WithContext(httpx.ConUsuarioID(context.Background(), e.ana))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /pendientes: %d — %s", res.Code, res.Body.String())
	}

	var cuerpo struct {
		Pendientes []recurrentes.Ocurrencia `json:"pendientes"`
		Proximas   []recurrentes.Ocurrencia `json:"proximas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &cuerpo); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	return cuerpo.Pendientes, cuerpo.Proximas
}

// arriendoElDia arma el cuerpo de un mensual que empezó hace un año, para que
// la fecha de inicio nunca tape lo que se está probando.
func arriendoElDia(dia int) map[string]any {
	return map[string]any{
		"categoria_id":  0, // lo llena quien lo usa
		"medio_pago_id": 0,
		"tipo":          "pague",
		"monto":         "1200000",
		"descripcion":   "arriendo",
		"frecuencia":    "mensual",
		"dia":           dia,
		"desde":         time.Now().AddDate(-1, 0, 0).Format("2006-01-02"),
	}
}

func (e *entorno) arriendo(t *testing.T, dia int) recurrentes.Recurrente {
	t.Helper()
	cuerpo := arriendoElDia(dia)
	cuerpo["categoria_id"] = e.categoria
	cuerpo["medio_pago_id"] = e.efectivo
	return e.crear(t, cuerpo)
}

// --------------------------------------------------------------------------

// LO QUE PEDÍA EL USUARIO: crear el arriendo y verlo de una vez en el resumen.
// Antes las ocurrencias solo se generaban hasta HOY, así que un recurrente
// recién creado no existía en ninguna parte hasta el día que le tocaba: creabas
// el arriendo del 5 un 18 y el resumen callaba diecisiete días.
func TestUnRecurrenteReciénCreadoYaSeVeEnLoQueViene(t *testing.T) {
	e := nuevoEntorno(t)

	// Dentro de la ventana futura pase lo que pase: dentro de 10 días.
	dentroDe10 := time.Now().AddDate(0, 0, 10)
	e.arriendo(t, dentroDe10.Day())

	_, proximas := e.listas(t)
	if len(proximas) == 0 {
		t.Fatal("el recurrente recién creado no aparece en lo que viene")
	}

	hoy := time.Now().Format("2006-01-02")
	for _, o := range proximas {
		if o.Fecha <= hoy {
			t.Errorf("una ocurrencia de %s salió como futura, y ya venció", o.Fecha)
		}
		if o.Descripcion != "arriendo" || o.Monto != "1200000.00" {
			t.Errorf("la ocurrencia no trae los datos de la plantilla: %+v", o)
		}
	}
}

// Y prepararlas antes NO las cobra: mientras no se confirmen, en la base del
// dinero no hay nada. Es la regla que sostiene todo el paquete.
func TestLoQueVieneNoCreaNingunMovimiento(t *testing.T) {
	e := nuevoEntorno(t)
	e.arriendo(t, time.Now().AddDate(0, 0, 10).Day())

	var n int
	err := e.pool.QueryRowContext(context.Background(),
		"SELECT count(*) FROM movimientos WHERE usuario_id = $1", e.ana).Scan(&n)
	if err != nil {
		t.Fatalf("contando movimientos: %v", err)
	}
	if n != 0 {
		t.Fatalf("lo que viene creó %d movimientos: nada se registra sin confirmar", n)
	}
}

// Un gasto que todavía no vence se puede confirmar antes — el arriendo del 5 a
// veces se paga el 2 — y ahí sí nace el movimiento.
func TestSePuedePagarAntesDeQueVenza(t *testing.T) {
	e := nuevoEntorno(t)
	e.fondear(t)
	e.arriendo(t, time.Now().AddDate(0, 0, 10).Day())

	_, proximas := e.listas(t)
	if len(proximas) == 0 {
		t.Fatal("no hay nada que adelantar")
	}
	futura := proximas[0]

	ruta := fmt.Sprintf("/pendientes/%d/confirmar", futura.ID)
	req := httptest.NewRequest(http.MethodPost, ruta, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), e.ana))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("confirmar por adelantado: %d — %s", res.Code, res.Body.String())
	}

	var creado movimientos.Movimiento
	if err := json.Unmarshal(res.Body.Bytes(), &creado); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if creado.Monto != "1200000.00" {
		t.Errorf("monto = %s", creado.Monto)
	}

	// Y sale de la lista: ya no está esperando nada.
	_, proximas = e.listas(t)
	for _, o := range proximas {
		if o.ID == futura.ID {
			t.Error("la ocurrencia confirmada sigue en lo que viene")
		}
	}
}

// El recibo nunca llega por el mismo valor: la plantilla dice $100.000 y este
// mes vinieron $118.000. Se corrige AL CONFIRMAR, y lo que se guarda es lo que
// de verdad se pagó — sin tocar la plantilla, que el mes entrante vuelve a
// proponer su valor de siempre.
func TestSePuedeCorregirElMontoAlConfirmar(t *testing.T) {
	e := nuevoEntorno(t)
	e.fondear(t)
	rec := e.arriendo(t, time.Now().AddDate(0, 0, 10).Day())

	_, proximas := e.listas(t)
	if len(proximas) == 0 {
		t.Fatal("no hay nada por confirmar")
	}
	o := proximas[0]

	// Lo mismo que manda la app cuando usas "Otro monto".
	cuerpo := map[string]any{
		"categoria_id":  o.CategoriaID,
		"medio_pago_id": o.MedioPagoID,
		"tipo":          o.Tipo,
		"monto":         "1350000",
		"fecha":         o.Fecha,
		"descripcion":   o.Descripcion,
	}
	datos, err := json.Marshal(cuerpo)
	if err != nil {
		t.Fatalf("armando el cuerpo: %v", err)
	}

	ruta := fmt.Sprintf("/pendientes/%d/confirmar", o.ID)
	req := httptest.NewRequest(http.MethodPost, ruta, strings.NewReader(string(datos)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), e.ana))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("confirmar con otro monto: %d — %s", res.Code, res.Body.String())
	}

	var creado movimientos.Movimiento
	if err := json.Unmarshal(res.Body.Bytes(), &creado); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if creado.Monto != "1350000.00" {
		t.Errorf("se guardó %s: manda lo que se corrigió, no la plantilla", creado.Monto)
	}

	// Y la plantilla no se movió: el mes entrante vuelve a proponer $1.200.000.
	var plantilla string
	err = e.pool.QueryRowContext(context.Background(),
		"SELECT monto::text FROM recurrentes WHERE id = $1", rec.ID).Scan(&plantilla)
	if err != nil {
		t.Fatalf("consultando la plantilla: %v", err)
	}
	if plantilla != "1200000.00" {
		t.Errorf("la plantilla quedó en %s: corregir un mes no cambia los demás", plantilla)
	}
}

// El corte entre las dos listas es la fecha de hoy, y de él dependen también
// los avisos al celular: sin el corte llegaría un "¿ya pagaste el arriendo?"
// treinta días antes.
func TestLoVencidoYLoQueVieneNoSeMezclan(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	rec := e.arriendo(t, time.Now().AddDate(0, 0, 10).Day())

	// Una ocurrencia de ayer, puesta a mano: es lo que la tarea habría
	// generado si el recurrente venía de antes.
	ayer := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	_, err := e.pool.ExecContext(ctx,
		`INSERT INTO recurrentes_ocurrencias (usuario_id, recurrente_id, fecha) VALUES ($1, $2, $3::date)`,
		e.ana, rec.ID, ayer)
	if err != nil {
		t.Fatalf("insertando la ocurrencia vencida: %v", err)
	}

	pendientes, proximas := e.listas(t)
	hoy := time.Now().Format("2006-01-02")

	// La de ayer tiene que estar entre lo vencido (junto con las que el
	// recurrente venía arrastrando de sus 60 días hacia atrás), y ninguna de
	// esa lista puede ser del futuro.
	if !contiene(pendientes, ayer) {
		t.Errorf("la ocurrencia de ayer no salió como vencida: %+v", fechasDe(pendientes))
	}
	for _, o := range pendientes {
		if o.Fecha > hoy {
			t.Errorf("una ocurrencia de %s salió como vencida, y todavía no llega", o.Fecha)
		}
	}

	// Y al revés: lo que viene es todo futuro.
	if len(proximas) == 0 {
		t.Fatal("no quedó nada en lo que viene")
	}
	for _, o := range proximas {
		if o.Fecha <= hoy {
			t.Errorf("una ocurrencia de %s salió como futura, y ya venció", o.Fecha)
		}
	}

	// Los avisos miran lo mismo que la primera lista: nunca lo de adelante.
	soloVencidas, err := e.store.Pendientes(ctx, e.ana, time.Now())
	if err != nil {
		t.Fatalf("consultando pendientes: %v", err)
	}
	if len(soloVencidas) != len(pendientes) {
		t.Errorf("los avisos verían %d ocurrencias y el resumen %d",
			len(soloVencidas), len(pendientes))
	}
}

func contiene(lista []recurrentes.Ocurrencia, fecha string) bool {
	for _, o := range lista {
		if o.Fecha == fecha {
			return true
		}
	}
	return false
}

func fechasDe(lista []recurrentes.Ocurrencia) []string {
	salida := make([]string, 0, len(lista))
	for _, o := range lista {
		salida = append(salida, o.Fecha)
	}
	return salida
}

// Editar la plantilla rehace lo que se había anunciado hacia adelante: si le
// cambias el día, lo que decía el resumen ya no es cierto.
func TestEditarLaPlantillaRehaceLoQueViene(t *testing.T) {
	e := nuevoEntorno(t)

	viejoDia := time.Now().AddDate(0, 0, 10).Day()
	rec := e.arriendo(t, viejoDia)

	_, antes := e.listas(t)
	if len(antes) == 0 {
		t.Fatal("no se preparó nada al crear")
	}

	nuevoDia := time.Now().AddDate(0, 0, 20).Day()
	cuerpo := arriendoElDia(nuevoDia)
	cuerpo["categoria_id"] = e.categoria
	cuerpo["medio_pago_id"] = e.efectivo
	cuerpo["monto"] = "1300000"
	e.actualizar(t, rec.ID, cuerpo)

	_, despues := e.listas(t)
	if len(despues) == 0 {
		t.Fatal("después de editar no quedó ninguna ocurrencia futura")
	}
	for _, o := range despues {
		f, err := time.Parse("2006-01-02", o.Fecha)
		if err != nil {
			t.Fatalf("fecha ilegible: %v", err)
		}
		if f.Day() != nuevoDia {
			t.Errorf("quedó anunciada una fecha del día %d: el recurrente ahora es el %d", f.Day(), nuevoDia)
		}
		if o.Monto != "1300000.00" {
			t.Errorf("monto = %s, se esperaba el nuevo", o.Monto)
		}
	}
}

// Pausar un recurrente borra lo que venía: un gasto pausado no está encima.
func TestPausarQuitaLoQueVenia(t *testing.T) {
	e := nuevoEntorno(t)

	rec := e.arriendo(t, time.Now().AddDate(0, 0, 10).Day())
	if _, antes := e.listas(t); len(antes) == 0 {
		t.Fatal("no se preparó nada al crear")
	}

	cuerpo := arriendoElDia(rec.Dia)
	cuerpo["categoria_id"] = e.categoria
	cuerpo["medio_pago_id"] = e.efectivo
	cuerpo["activo"] = false
	e.actualizar(t, rec.ID, cuerpo)

	if _, despues := e.listas(t); len(despues) != 0 {
		t.Errorf("un recurrente pausado sigue anunciando %d ocurrencias", len(despues))
	}
}
