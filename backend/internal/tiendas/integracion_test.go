package tiendas_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"finanzas/internal/admin"
	"finanzas/internal/auth"
	"finanzas/internal/db"
	"finanzas/internal/httpx"
	"finanzas/internal/suscripciones"
	"finanzas/internal/tiendas"
)

// Contra un Postgres de verdad, porque lo que se prueba es lo que decide la
// base: el plan que abre o cierra la seccion y el filtro por usuario que
// impide ver las tiendas de otro. Se saltan sin base de prueba.
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
	handler *tiendas.Handler
	rutas   http.Handler
	admin   *admin.Store
	plan    *suscripciones.Store
	// almacen es el mismo que usa el handler: las pruebas de fotos miran su
	// carpeta para saber si un archivo sigue en el disco.
	almacen almacenTemporal
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := abrirBasePrueba(t)
	plan := suscripciones.NewStore(pool)
	// El mismo permiso que monta el router de verdad: el plan del cliente. El
	// almacen de las fotos es una carpeta temporal que Go borra al terminar:
	// la prueba escribe archivos de verdad sin ensuciar nada.
	almacen := almacenTemporal{t.TempDir()}
	handler := tiendas.NewHandler(tiendas.NewStore(pool), func(ctx context.Context, id int64) (bool, error) {
		c, err := plan.DelUsuario(ctx, id)
		return c.Tiendas, err
	}, almacen)
	return &entorno{
		pool:    pool,
		handler: handler,
		rutas:   handler.Rutas(),
		admin:   admin.NewStore(pool),
		plan:    plan,
		almacen: almacen,
	}
}

func (e *entorno) crearCliente(t *testing.T) int64 {
	t.Helper()
	hash, err := auth.HashPassword("ClaveDePrueba123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := auth.NewStore(e.pool).Crear(context.Background(), unico("tiendas")+"@prueba.local",
		"Cliente", auth.RolUsuario, hash)
	if err != nil {
		t.Fatalf("creando cliente: %v", err)
	}
	t.Cleanup(func() {
		// Las tiendas se van solas con el usuario (CASCADE).
		_, _ = e.pool.ExecContext(context.Background(), "DELETE FROM usuarios WHERE id = $1", u.ID)
	})
	return u.ID
}

// conPlan le asigna al cliente un plan nuevo que incluye (o no) las tiendas.
func (e *entorno) conPlan(t *testing.T, usuarioID int64, conTiendas bool) {
	t.Helper()
	p, err := e.plan.CrearPlan(context.Background(), suscripciones.DatosPlan{
		Nombre: unico("plan"), PrecioMensual: "10000", IncluyeTiendas: conTiendas, Activo: true,
	})
	if err != nil {
		t.Fatalf("creando plan: %v", err)
	}
	t.Cleanup(func() {
		limpieza := context.Background()
		_, _ = e.pool.ExecContext(limpieza, "UPDATE usuarios SET plan_id = NULL WHERE plan_id = $1", p.ID)
		_, _ = e.pool.ExecContext(limpieza, "DELETE FROM planes WHERE id = $1", p.ID)
	})
	if _, err := e.admin.AsignarPlan(context.Background(), usuarioID, &p.ID, "mensual"); err != nil {
		t.Fatalf("asignando plan: %v", err)
	}
}

// pedir hace una peticion como la haria el usuario indicado: el id va en el
// context, que es donde lo deja el middleware de auth en la app real.
func (e *entorno) pedir(t *testing.T, usuarioID int64, metodo, ruta, cuerpo string) *httptest.ResponseRecorder {
	return e.pedirEn(t, e.rutas, usuarioID, metodo, ruta, cuerpo)
}

// pedirEn es lo mismo contra otro router: /api/cierres se monta aparte del de
// tiendas, y las pruebas de esa seccion lo necesitan.
func (e *entorno) pedirEn(t *testing.T, rutas http.Handler, usuarioID int64, metodo, ruta, cuerpo string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(metodo, ruta, bytes.NewBufferString(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), usuarioID))

	w := httptest.NewRecorder()
	rutas.ServeHTTP(w, req)
	return w
}

// rutasTodos monta /api/cierres con el mismo handler, como lo hace el router.
func (e *entorno) rutasTodos() http.Handler { return e.handler.RutasTodosLosCierres() }

// Lo que se vende es lo que manda: sin la casilla del plan, la seccion no
// existe ni escribiendo la URL a mano.
func TestSinTiendasEnElPlanNoSePuedeEntrar(t *testing.T) {
	e := nuevoEntorno(t)

	sinPlan := e.crearCliente(t)
	if w := e.pedir(t, sinPlan, "GET", "/", ""); w.Code != http.StatusForbidden {
		t.Errorf("sin plan: status = %d, se esperaba 403", w.Code)
	}

	conOtroPlan := e.crearCliente(t)
	e.conPlan(t, conOtroPlan, false)
	if w := e.pedir(t, conOtroPlan, "GET", "/", ""); w.Code != http.StatusForbidden {
		t.Errorf("plan sin tiendas: status = %d, se esperaba 403", w.Code)
	}
	// Y tampoco por la puerta de atras: si solo se cuidara el listado, se
	// podrian crear tiendas que despues nadie ve.
	w := e.pedir(t, conOtroPlan, "POST", "/", `{"nombre":"La de contrabando"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("crear sin tiendas en el plan: status = %d, se esperaba 403", w.Code)
	}

	// El permiso vive en DOS routers (Rutas y RutasTodosLosCierres). Si solo se
	// probara uno, quitarle el middleware al otro pasaria en verde y un cliente
	// sin la seccion seguiria leyendo y exportando sus cierres viejos.
	global := e.rutasTodos()
	for _, usuario := range []int64{sinPlan, conOtroPlan} {
		for _, ruta := range []string{"/", "/exportar"} {
			if w := e.pedirEn(t, global, usuario, "GET", ruta, ""); w.Code != http.StatusForbidden {
				t.Errorf("/api/cierres%s sin tiendas en el plan: status = %d, se esperaba 403", ruta, w.Code)
			}
		}
	}

	// Y la foto, que cuelga de un Mount dentro de otro Mount: es justo donde
	// es facil creer que el middleware llega y que no llegue.
	if w := e.subirFoto(t, conOtroPlan, "/1/cierres/1/foto", "hoja.jpg", []byte("x")); w.Code != http.StatusForbidden {
		t.Errorf("subir foto sin tiendas en el plan: status = %d, se esperaba 403", w.Code)
	}
}

// Quitarle las tiendas al plan corta la seccion de una, sin esperar a que
// venza el token: se revisa en cada peticion.
func TestQuitarLasTiendasDelPlanCortaElAcceso(t *testing.T) {
	e := nuevoEntorno(t)
	cliente := e.crearCliente(t)
	e.conPlan(t, cliente, true)

	// Con una tienda, un cierre y su foto de verdad: sin el middleware, todas
	// las rutas de abajo responderian 200, no un 404 que pase por casualidad.
	tienda := e.crearTienda(t, cliente, "centro")
	c := e.crearCierre(t, cliente, tienda, hojaDeUnDia)
	foto := fmt.Sprintf("/%d/cierres/%d/foto", tienda, c.ID)
	if w := e.subirFoto(t, cliente, foto, "hoja.jpg", []byte("la hoja")); w.Code != http.StatusOK {
		t.Fatalf("subiendo la foto con el plan puesto: status = %d, cuerpo %s", w.Code, w.Body)
	}

	_, err := e.pool.ExecContext(context.Background(), `
		UPDATE planes SET incluye_tiendas = false
		WHERE id = (SELECT plan_id FROM usuarios WHERE id = $1)`, cliente)
	if err != nil {
		t.Fatalf("quitando las tiendas del plan: %v", err)
	}

	if w := e.pedir(t, cliente, "GET", "/", ""); w.Code != http.StatusForbidden {
		t.Errorf("después de quitarlas: status = %d, se esperaba 403", w.Code)
	}

	// Quitar la casilla cierra las DOS puertas: tambien /api/cierres.
	global := e.rutasTodos()
	for _, ruta := range []string{"/", "/exportar"} {
		if w := e.pedirEn(t, global, cliente, "GET", ruta, ""); w.Code != http.StatusForbidden {
			t.Errorf("/api/cierres%s después de quitarlas: status = %d, se esperaba 403", ruta, w.Code)
		}
	}

	// Y las de la foto, que llegan por el Mount anidado.
	if w := e.subirFoto(t, cliente, foto, "otra.jpg", []byte("otra hoja")); w.Code != http.StatusForbidden {
		t.Errorf("subir foto después de quitarlas: status = %d, se esperaba 403", w.Code)
	}
	for _, metodo := range []string{"GET", "DELETE"} {
		if w := e.pedir(t, cliente, metodo, foto, ""); w.Code != http.StatusForbidden {
			t.Errorf("%s foto después de quitarlas: status = %d, se esperaba 403", metodo, w.Code)
		}
	}
}

// Cada quien ve las suyas. El id de la URL no alcanza para tocar la tienda de
// otro, aunque los dos tengan la seccion en su plan.
func TestLasTiendasDeOtroNoSeVenNiSeBorran(t *testing.T) {
	e := nuevoEntorno(t)

	ana := e.crearCliente(t)
	beto := e.crearCliente(t)
	e.conPlan(t, ana, true)
	e.conPlan(t, beto, true)

	w := e.pedir(t, ana, "POST", "/", `{"nombre":"La de Ana"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("creando la de Ana: status = %d, cuerpo %s", w.Code, w.Body)
	}
	var deAna tiendas.Tienda
	if err := json.Unmarshal(w.Body.Bytes(), &deAna); err != nil {
		t.Fatalf("leyendo la respuesta: %v", err)
	}

	w = e.pedir(t, beto, "GET", "/", "")
	var lista []tiendas.Tienda
	if err := json.Unmarshal(w.Body.Bytes(), &lista); err != nil {
		t.Fatalf("leyendo la lista de Beto: %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("Beto ve %d tiendas ajenas", len(lista))
	}

	ruta := fmt.Sprintf("/%d", deAna.ID)
	if w := e.pedir(t, beto, "DELETE", ruta, ""); w.Code != http.StatusNotFound {
		t.Errorf("Beto borrando la de Ana: status = %d, se esperaba 404", w.Code)
	}
	if w := e.pedir(t, beto, "PUT", ruta, `{"nombre":"Mía"}`); w.Code != http.StatusNotFound {
		t.Errorf("Beto renombrando la de Ana: status = %d, se esperaba 404", w.Code)
	}

	// Y la de Ana sigue ahí, con su nombre.
	w = e.pedir(t, ana, "GET", "/", "")
	if err := json.Unmarshal(w.Body.Bytes(), &lista); err != nil {
		t.Fatalf("releyendo las de Ana: %v", err)
	}
	if len(lista) != 1 || lista[0].Nombre != "La de Ana" {
		t.Errorf("las tiendas de Ana quedaron en %+v", lista)
	}
}

// El nombre es unico dentro de la cuenta, no en toda la base: dos clientes
// distintos pueden tener los dos su "Principal".
func TestElNombreSeRepiteEntreCuentasPeroNoDentroDeUna(t *testing.T) {
	e := nuevoEntorno(t)

	ana := e.crearCliente(t)
	beto := e.crearCliente(t)
	e.conPlan(t, ana, true)
	e.conPlan(t, beto, true)

	if w := e.pedir(t, ana, "POST", "/", `{"nombre":"Principal"}`); w.Code != http.StatusCreated {
		t.Fatalf("la primera: status = %d, cuerpo %s", w.Code, w.Body)
	}
	if w := e.pedir(t, beto, "POST", "/", `{"nombre":"Principal"}`); w.Code != http.StatusCreated {
		t.Errorf("Beto con el mismo nombre: status = %d, debería poder", w.Code)
	}
	// Repetida en la MISMA cuenta, y con otras mayúsculas: no entra.
	w := e.pedir(t, ana, "POST", "/", `{"nombre":"principal"}`)
	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
		t.Errorf("repetida en la misma cuenta: status = %d, se esperaba un error de campo", w.Code)
	}
}
