package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"finanzas/internal/admin"
	"finanzas/internal/auth"
	"finanzas/internal/categorias"
	"finanzas/internal/db"
	"finanzas/internal/medios"
)

// Contra un Postgres de verdad: lo que se prueba son las cifras de uso, y
// viven enteras en el SQL.
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
	store     *admin.Store
	auth      *auth.Store
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

	return &entorno{
		pool:      pool,
		store:     admin.NewStore(pool),
		auth:      auth.NewStore(pool),
		ana:       ana,
		categoria: categoria.ID,
		efectivo:  efectivo.ID,
	}
}

var contador atomic.Int64

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

// movimientoHaceDias anota un gasto con fecha de creación en el pasado: es la
// única forma de probar una ventana de 30 días sin esperar 30 días.
func (e *entorno) movimientoHaceDias(t *testing.T, dias int) {
	t.Helper()

	cuando := time.Now().AddDate(0, 0, -dias)
	_, err := e.pool.ExecContext(context.Background(), `
		INSERT INTO movimientos (usuario_id, categoria_id, medio_pago_id, tipo, monto, fecha, descripcion, creado_en)
		VALUES ($1, $2, $3, 'pague', 1000, $4::date, 'prueba', $4)`,
		e.ana, e.categoria, e.efectivo, cuando)
	if err != nil {
		t.Fatalf("insertando movimiento: %v", err)
	}
}

func (e *entorno) ficha(t *testing.T) admin.UsuarioAdmin {
	t.Helper()

	lista, err := e.store.Listar(context.Background())
	if err != nil {
		t.Fatalf("listando usuarios: %v", err)
	}
	for _, u := range lista {
		if u.ID == e.ana {
			return u
		}
	}
	t.Fatal("Ana no salió en la lista")
	return admin.UsuarioAdmin{}
}

// --------------------------------------------------------------------------

// Un conteo de movimientos no dice si todavía usan la app: mil de hace ocho
// meses y mil de esta semana se ven igual. Esto sí.
func TestLaFichaDiceCuandoFueLaUltimaVez(t *testing.T) {
	e := nuevoEntorno(t)

	// Recién creada, sin haber entrado nunca: no hay nada que mostrar, y
	// fingir una fecha sería peor que no tenerla.
	if u := e.ficha(t); u.UltimaActividad != nil {
		t.Errorf("una cuenta sin uso trae fecha de actividad: %v", u.UltimaActividad)
	}

	e.movimientoHaceDias(t, 3)

	u := e.ficha(t)
	if u.UltimaActividad == nil {
		t.Fatal("después de anotar un movimiento no hay última actividad")
	}
	if hace := time.Since(*u.UltimaActividad); hace > 4*24*time.Hour || hace < 2*24*time.Hour {
		t.Errorf("la última actividad quedó en %v, se esperaba hace ~3 días", *u.UltimaActividad)
	}
}

// La frecuencia cuenta DÍAS distintos, no movimientos: alguien que anota diez
// cosas un lunes y no vuelve usa la app un día, no diez.
func TestLaFrecuenciaCuentaDiasYNoMovimientos(t *testing.T) {
	e := nuevoEntorno(t)

	// Tres el mismo día, uno en otro día.
	e.movimientoHaceDias(t, 2)
	e.movimientoHaceDias(t, 2)
	e.movimientoHaceDias(t, 2)
	e.movimientoHaceDias(t, 5)

	if u := e.ficha(t); u.DiasActivos != 2 {
		t.Errorf("dias_activos = %d, se esperaban 2 días distintos", u.DiasActivos)
	}
}

// Y solo cuenta la ventana: lo de hace tres meses ya no dice nada de si lo
// usa HOY, que es la pregunta.
func TestLoViejoNoCuentaComoFrecuencia(t *testing.T) {
	e := nuevoEntorno(t)

	e.movimientoHaceDias(t, 90)
	e.movimientoHaceDias(t, 45)

	u := e.ficha(t)
	if u.DiasActivos != 0 {
		t.Errorf("dias_activos = %d: nada de eso cae en los últimos %d días", u.DiasActivos, admin.DiasDeFrecuencia)
	}
	// Pero la última vez sí se sigue viendo: es justo el dato que dice "este
	// cliente lleva mes y medio sin aparecer".
	if u.UltimaActividad == nil {
		t.Error("se perdió la última actividad de una cuenta vieja")
	}
}

// Abrir la app cuenta como uso aunque no anote nada: hay clientes que solo
// entran a mirar sus cifras, y contarlos como inactivos sería mentir.
func TestEntrarALaAppCuentaComoUso(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.auth.RegistrarAcceso(ctx, e.ana)

	u := e.ficha(t)
	if u.UltimoAcceso == nil {
		t.Fatal("no quedó registrado el acceso")
	}
	if u.UltimaActividad == nil {
		t.Fatal("el acceso no cuenta como actividad")
	}
}

// El registro lleva su propio freno: sin él, cada pantalla que abre el
// frontend sería otra escritura sobre la misma fila.
func TestElAccesoNoSeReescribeACadaRato(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()

	e.auth.RegistrarAcceso(ctx, e.ana)
	primero := *e.ficha(t).UltimoAcceso

	e.auth.RegistrarAcceso(ctx, e.ana)
	segundo := *e.ficha(t).UltimoAcceso

	if !primero.Equal(segundo) {
		t.Errorf("el acceso se reescribió de inmediato: %v -> %v", primero, segundo)
	}
}

// LA REGRESIÓN: Listar y PorID tenían la consulta copiada, y al agregar las
// cifras de uso se actualizó una sola. La lista las mostraba y la respuesta de
// editar un cliente las devolvía en cero, pisando en pantalla lo que se
// acababa de ver.
func TestLaFichaSueltaTraeLasMismasCifrasQueLaLista(t *testing.T) {
	e := nuevoEntorno(t)
	e.auth.RegistrarAcceso(context.Background(), e.ana)
	e.movimientoHaceDias(t, 1)

	deLaLista := e.ficha(t)

	suelta, err := e.store.PorID(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("consultando la ficha: %v", err)
	}

	if suelta.DiasActivos != deLaLista.DiasActivos {
		t.Errorf("dias_activos: la lista dice %d y la ficha %d", deLaLista.DiasActivos, suelta.DiasActivos)
	}
	if suelta.UltimaActividad == nil || !suelta.UltimaActividad.Equal(*deLaLista.UltimaActividad) {
		t.Errorf("ultima_actividad: la lista dice %v y la ficha %v", deLaLista.UltimaActividad, suelta.UltimaActividad)
	}
	if suelta.UltimoAcceso == nil || !suelta.UltimoAcceso.Equal(*deLaLista.UltimoAcceso) {
		t.Errorf("ultimo_acceso: la lista dice %v y la ficha %v", deLaLista.UltimoAcceso, suelta.UltimoAcceso)
	}
}
