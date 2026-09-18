package agente_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"finanzas/internal/agente"
	"finanzas/internal/auth"
	"finanzas/internal/categorias"
	"finanzas/internal/db"
	"finanzas/internal/httpx"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
	"finanzas/internal/recurrentes"
)

// Estas pruebas corren contra un Postgres DE VERDAD, porque lo que se está
// probando es el filtro por usuario_id: que el chat de cada quien sea suyo y
// de nadie más. Con una base falsa no se probaría nada real.
//
// Se saltan solas si no hay base de prueba configurada:
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

// proveedorFalso reemplaza al modelo: responde el guion que le demos y guarda
// lo que recibió. Es toda la razón por la que Proveedor es una interfaz.
//
// El guion se consume en orden; cuando se acaba, se repite `repetir`. Así se
// escribe "primero pide la herramienta X, después contesta esto".
type proveedorFalso struct {
	guion   []agente.Respuesta
	repetir agente.Respuesta
	err     error

	llamadas     int
	sistema      string
	pasos        []agente.Paso
	herramientas []agente.Herramienta
}

func (p *proveedorFalso) Completar(ctx context.Context, sistema string, pasos []agente.Paso, herramientas []agente.Herramienta) (agente.Respuesta, error) {
	p.llamadas++
	p.sistema = sistema
	p.pasos = append([]agente.Paso(nil), pasos...)
	p.herramientas = herramientas

	if p.err != nil {
		return agente.Respuesta{}, p.err
	}
	if len(p.guion) > 0 {
		siguiente := p.guion[0]
		p.guion = p.guion[1:]
		return siguiente, nil
	}
	return p.repetir, nil
}

// pideHerramienta arma la respuesta con la que el modelo pide datos.
func pideHerramienta(nombre, argumentos string) agente.Respuesta {
	return agente.Respuesta{
		Llamadas: []agente.Llamada{{
			ID:         "llamada-1",
			Nombre:     nombre,
			Argumentos: json.RawMessage(argumentos),
		}},
		TokensEntrada: 100,
		TokensSalida:  20,
	}
}

type entorno struct {
	pool        *sql.DB
	store       *agente.Store
	recurrentes *recurrentes.Store
	proveedor   *proveedorFalso
	handler     *agente.Handler
	movimientos *movimientos.Store
	categoria   int64 // una categoría de Ana, para poder crearle movimientos
	efectivo    int64 // y dos medios de pago suyos
	banco       int64
	ana         int64 // la dueña del chat
	beto        int64 // el vecino que no tiene por qué verlo
}

func nuevoEntorno(t *testing.T, limiteDiario int) *entorno {
	t.Helper()
	pool := abrirBasePrueba(t)

	// Cada prueba trabaja con usuarios recién creados y al terminar borra solo
	// los suyos. NO se vacían las tablas: `go test ./...` corre los paquetes en
	// paralelo contra la misma base, y una limpieza global le arrancaría los
	// datos al paquete de al lado a mitad de prueba. Como todas las consultas
	// filtran por usuario_id, un usuario nuevo ya es un compartimento limpio.
	ana := crearUsuario(t, pool, "Ana")
	beto := crearUsuario(t, pool, "Beto")

	// El chat solo existe para quien tiene IA en su plan: los dos la tienen,
	// y las pruebas de permisos crean sus propios usuarios sin ella.
	planIA := crearPlan(t, pool, true)
	asignarPlan(t, pool, ana, planIA)
	asignarPlan(t, pool, beto, planIA)

	movimientosStore := movimientos.NewStore(pool)
	categoriasStore := categorias.NewStore(pool)
	mediosStore := medios.NewStore(pool)

	categoria, err := categoriasStore.Crear(context.Background(), ana, "Negocio")
	if err != nil {
		t.Fatalf("creando categoría: %v", err)
	}
	efectivo, err := mediosStore.Crear(context.Background(), ana, "Efectivo")
	if err != nil {
		t.Fatalf("creando medio: %v", err)
	}
	banco, err := mediosStore.Crear(context.Background(), ana, "Transferencia")
	if err != nil {
		t.Fatalf("creando medio: %v", err)
	}

	store := agente.NewStore(pool)
	proveedor := &proveedorFalso{repetir: agente.Respuesta{
		Contenido:     "Listo, te explico.",
		TokensEntrada: 10,
		TokensSalida:  3,
	}}
	recurrentesStore := recurrentes.NewStore(pool)
	catalogo := agente.NuevoCatalogo(movimientosStore, categoriasStore, mediosStore).
		ConRecurrentes(recurrentesStore, recurrentes.NuevoConfirmador(recurrentesStore, movimientosStore))

	return &entorno{
		pool:        pool,
		store:       store,
		recurrentes: recurrentesStore,
		proveedor:   proveedor,
		handler:     agente.NewHandler(store, proveedor, catalogo, limiteDiario, auth.NewStore(pool).TieneIA),
		movimientos: movimientosStore,
		categoria:   categoria.ID,
		efectivo:    efectivo.ID,
		banco:       banco.ID,
		ana:         ana,
		beto:        beto,
	}
}

// crearMovimiento le pone datos a Ana para que las herramientas tengan qué
// leer.
func (e *entorno) crearMovimiento(t *testing.T, tipo, monto, descripcion string) {
	t.Helper()

	_, err := e.movimientos.Crear(context.Background(), e.ana, movimientos.Datos{
		CategoriaID: e.categoria,
		MedioPagoID: &e.efectivo,
		Tipo:        tipo,
		Monto:       monto,
		Fecha:       "2026-09-15",
		Descripcion: descripcion,
	})
	if err != nil {
		t.Fatalf("creando movimiento: %v", err)
	}
}

// crearPrestamo deja un préstamo pendiente, que es lo que el agente propone
// marcar como cobrado.
func (e *entorno) crearPrestamo(t *testing.T, monto, aQuien string) *movimientos.Movimiento {
	t.Helper()

	pendiente := movimientos.EstadoPendiente
	m, err := e.movimientos.Crear(context.Background(), e.ana, movimientos.Datos{
		CategoriaID: e.categoria,
		MedioPagoID: &e.efectivo,
		Tipo:        movimientos.TipoPreste,
		Monto:       monto,
		Fecha:       "2026-09-10",
		Descripcion: "préstamo",
		AQuien:      &aQuien,
		Estado:      &pendiente,
	})
	if err != nil {
		t.Fatalf("creando préstamo: %v", err)
	}
	return m
}

// contador da correos únicos: el índice de usuarios es único y las pruebas se
// corren muchas veces contra la misma base.
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

	// El orden importa: los movimientos antes que las categorías, porque esa
	// llave foránea es RESTRICT. Las conversaciones, mensajes y marcas de
	// consumo se van solas por CASCADE.
	t.Cleanup(func() {
		limpieza := context.Background()
		// Los recurrentes antes que las categorías y los medios: esas dos
		// llaves foráneas son RESTRICT, igual que las de movimientos.
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

// crearPlan crea un plan con o sin IA, y al terminar lo borra. Antes de
// borrarlo se lo quita a quien lo tenga: usuarios.plan_id es RESTRICT.
func crearPlan(t *testing.T, pool *sql.DB, incluyeIA bool) int64 {
	t.Helper()

	nombre := fmt.Sprintf("Plan prueba %d-%d", time.Now().UnixNano(), contador.Add(1))
	var id int64
	err := pool.QueryRowContext(context.Background(),
		`INSERT INTO planes (nombre, precio_mensual, incluye_ia) VALUES ($1, 10000, $2) RETURNING id`,
		nombre, incluyeIA).Scan(&id)
	if err != nil {
		t.Fatalf("creando plan: %v", err)
	}

	t.Cleanup(func() {
		limpieza := context.Background()
		if _, err := pool.ExecContext(limpieza, "UPDATE usuarios SET plan_id = NULL WHERE plan_id = $1", id); err != nil {
			t.Errorf("desasignando el plan: %v", err)
		}
		if _, err := pool.ExecContext(limpieza, "DELETE FROM planes WHERE id = $1", id); err != nil {
			t.Errorf("limpiando el plan: %v", err)
		}
	})
	return id
}

func asignarPlan(t *testing.T, pool *sql.DB, usuarioID, planID int64) {
	t.Helper()
	if _, err := pool.ExecContext(context.Background(),
		"UPDATE usuarios SET plan_id = $1 WHERE id = $2", planID, usuarioID); err != nil {
		t.Fatalf("asignando plan: %v", err)
	}
}

// enviar hace la petición como la haría la app: con el id del usuario en el
// context, que es donde lo deja el middleware de auth.
func (e *entorno) enviar(t *testing.T, usuarioID int64, texto string) *httptest.ResponseRecorder {
	t.Helper()

	cuerpo, err := json.Marshal(map[string]string{"texto": texto})
	if err != nil {
		t.Fatalf("armando el cuerpo: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mensajes", strings.NewReader(string(cuerpo)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), usuarioID))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	return res
}

func (e *entorno) leerHilo(t *testing.T, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	return res
}

// --------------------------------------------------------------------------
// La regla del paquete: el agente solo ve el hilo del usuario de la sesión.
// --------------------------------------------------------------------------

func TestElHiloDeCadaQuienEsSuyo(t *testing.T) {
	e := nuevoEntorno(t, 10)
	ctx := context.Background()

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	conversacionDeAna, err := e.store.Activa(ctx, e.ana)
	if err != nil {
		t.Fatalf("conversación de Ana: %v", err)
	}

	// Beto no tiene hilo, aunque en la base exista el de Ana.
	if _, err := e.store.Activa(ctx, e.beto); !errors.Is(err, agente.ErrNoEncontrada) {
		t.Errorf("Beto ve una conversación que no es suya (err = %v)", err)
	}

	// Y conociendo el id del hilo de Ana tampoco puede leerlo...
	mensajes, err := e.store.Historial(ctx, e.beto, conversacionDeAna.ID, 50)
	if err != nil {
		t.Fatalf("historial: %v", err)
	}
	if len(mensajes) != 0 {
		t.Errorf("Beto leyó %d mensajes del hilo de Ana", len(mensajes))
	}

	// ...ni escribir en él.
	_, err = e.store.GuardarMensaje(ctx, e.beto, conversacionDeAna.ID,
		agente.MensajeNuevo{Rol: agente.RolUsuario, Contenido: "cuélame esto"})
	if !errors.Is(err, agente.ErrNoEncontrada) {
		t.Errorf("Beto pudo escribir en el hilo de Ana (err = %v)", err)
	}
}

// Un admin en modo "ver como" puede revisar los movimientos de un cliente,
// pero no su conversación: no es un registro de dinero, es lo que esa persona
// escribió creyendo que era privado.
func TestElChatNoSeLeeEnModoVerComo(t *testing.T) {
	e := nuevoEntorno(t, 10)

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	// Así queda el context después de VerComo: el id del observado, más la
	// marca de quién está mirando.
	ctx := httpx.ConObservador(httpx.ConUsuarioID(context.Background(), e.ana), e.beto)

	if res := e.leerHilo(t, ctx); res.Code != http.StatusForbidden {
		t.Errorf("leer el hilo observando devolvió %d, se esperaba 403 — %s", res.Code, res.Body.String())
	}
}

// --------------------------------------------------------------------------
// El resto del ciclo
// --------------------------------------------------------------------------

func TestGuardaLaPreguntaYLaRespuesta(t *testing.T) {
	e := nuevoEntorno(t, 10)

	res := e.enviar(t, e.ana, "¿cómo registro un gasto?")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Mensaje   agente.Mensaje `json:"mensaje"`
		Restantes int            `json:"restantes"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if envio.Mensaje.Rol != agente.RolAgente || envio.Mensaje.Contenido != "Listo, te explico." {
		t.Errorf("mensaje devuelto = %+v", envio.Mensaje)
	}
	if envio.Restantes != 9 {
		t.Errorf("restantes = %d, se esperaba 9", envio.Restantes)
	}

	// El modelo recibió el nombre de la usuaria y su pregunta, en ese orden.
	if !strings.Contains(e.proveedor.sistema, "Ana") {
		t.Error("las instrucciones no llevan el nombre del usuario")
	}
	if len(e.proveedor.pasos) != 1 || e.proveedor.pasos[0].Rol != agente.RolUsuario {
		t.Errorf("pasos enviados = %+v", e.proveedor.pasos)
	}

	// Y el hilo queda con las dos líneas, en orden.
	hilo := e.leerHilo(t, httpx.ConUsuarioID(context.Background(), e.ana))
	var conv struct {
		Mensajes []agente.Mensaje `json:"mensajes"`
	}
	if err := json.Unmarshal(hilo.Body.Bytes(), &conv); err != nil {
		t.Fatalf("hilo ilegible: %v", err)
	}
	if len(conv.Mensajes) != 2 ||
		conv.Mensajes[0].Rol != agente.RolUsuario ||
		conv.Mensajes[1].Rol != agente.RolAgente {
		t.Errorf("hilo = %+v", conv.Mensajes)
	}
}

func TestLimiteDiario(t *testing.T) {
	e := nuevoEntorno(t, 2)

	for i := range 2 {
		if res := e.enviar(t, e.ana, "otra pregunta"); res.Code != http.StatusOK {
			t.Fatalf("mensaje %d: %d — %s", i+1, res.Code, res.Body.String())
		}
	}

	res := e.enviar(t, e.ana, "una más")
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("el tercer mensaje devolvió %d, se esperaba 429", res.Code)
	}
	if e.proveedor.llamadas != 2 {
		t.Errorf("se llamó al modelo %d veces: el límite tiene que cortar ANTES de gastar", e.proveedor.llamadas)
	}

	// El límite es por persona, no del servidor entero.
	if res := e.enviar(t, e.beto, "hola"); res.Code != http.StatusOK {
		t.Errorf("a Beto lo frenó el límite de Ana: %d", res.Code)
	}
}

// Si el modelo se cae, el usuario ve un 503 con un mensaje legible — y su
// pregunta no se pierde, que es lo que más molesta de un chat.
func TestProveedorCaidoConservaLaPregunta(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.proveedor.err = agente.ErrProveedorNoDisponible

	res := e.enviar(t, e.ana, "¿me quedó guardada?")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("código = %d, se esperaba 503 — %s", res.Code, res.Body.String())
	}

	conv, err := e.store.Activa(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("conversación: %v", err)
	}
	if len(conv.Mensajes) != 1 || conv.Mensajes[0].Contenido != "¿me quedó guardada?" {
		t.Errorf("la pregunta no sobrevivió a la caída: %+v", conv.Mensajes)
	}
}

func TestBorrarDejaElHiloVacio(t *testing.T) {
	e := nuevoEntorno(t, 10)

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	req := httptest.NewRequest(http.MethodDelete, "/", nil).
		WithContext(httpx.ConUsuarioID(context.Background(), e.ana))
	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("borrar: %d", res.Code)
	}
	if _, err := e.store.Activa(context.Background(), e.ana); !errors.Is(err, agente.ErrNoEncontrada) {
		t.Errorf("quedó conversación después de borrar (err = %v)", err)
	}
}

// Borrar el hilo no puede devolver los mensajes del día: si no, el techo de
// costo se salta con un clic.
func TestBorrarNoReiniciaElLimite(t *testing.T) {
	e := nuevoEntorno(t, 1)

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	req := httptest.NewRequest(http.MethodDelete, "/", nil).
		WithContext(httpx.ConUsuarioID(context.Background(), e.ana))
	e.handler.Rutas().ServeHTTP(httptest.NewRecorder(), req)

	if res := e.enviar(t, e.ana, "otra"); res.Code != http.StatusTooManyRequests {
		t.Errorf("después de borrar devolvió %d: el límite se reinició", res.Code)
	}
}

func TestMensajeVacioODemasiadoLargo(t *testing.T) {
	e := nuevoEntorno(t, 10)

	if res := e.enviar(t, e.ana, "   "); res.Code != http.StatusUnprocessableEntity {
		t.Errorf("mensaje vacío devolvió %d, se esperaba 422", res.Code)
	}
	largo := strings.Repeat("a", agente.MaxCaracteresMensaje+1)
	if res := e.enviar(t, e.ana, largo); res.Code != http.StatusUnprocessableEntity {
		t.Errorf("mensaje larguísimo devolvió %d, se esperaba 422", res.Code)
	}
	if e.proveedor.llamadas != 0 {
		t.Errorf("se llamó al modelo %d veces con entradas inválidas", e.proveedor.llamadas)
	}
}

// --------------------------------------------------------------------------
// Fase 1: las herramientas de lectura
// --------------------------------------------------------------------------

// El ciclo completo: el modelo pide datos, se ejecuta la herramienta, y con
// ese resultado redacta. Lo que se guarda en el hilo es solo el texto final,
// más el rastro de qué consultó.
func TestElAgenteConsultaYLuegoResponde(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "almuerzo")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaResumen, `{}`),
		{Contenido: "Tienes $45.000 menos este mes.", TokensEntrada: 400, TokensSalida: 15},
	}

	res := e.enviar(t, e.ana, "¿cómo voy este mes?")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Mensaje agente.Mensaje `json:"mensaje"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}

	if envio.Mensaje.Contenido != "Tienes $45.000 menos este mes." {
		t.Errorf("contenido = %q", envio.Mensaje.Contenido)
	}
	// El rastro de lo consultado viaja con el mensaje: quien lee una cifra
	// tiene derecho a saber de dónde salió.
	if !slices.Equal(envio.Mensaje.Herramientas, []string{agente.HerramientaResumen}) {
		t.Errorf("herramientas = %v", envio.Mensaje.Herramientas)
	}
	if e.proveedor.llamadas != 2 {
		t.Errorf("llamadas al modelo = %d, se esperaban 2 (pedir datos + redactar)", e.proveedor.llamadas)
	}

	// En la segunda vuelta el modelo recibió la pregunta, su propia petición
	// de datos y el resultado de la herramienta.
	if len(e.proveedor.pasos) != 3 {
		t.Fatalf("pasos de la última llamada = %+v", e.proveedor.pasos)
	}
	resultado := e.proveedor.pasos[2]
	if resultado.Rol != agente.RolHerramienta || !strings.Contains(resultado.Contenido, "totales") {
		t.Errorf("el resultado de la herramienta no llegó bien: %+v", resultado)
	}

	// Y del ir y venir no queda nada en el hilo: solo pregunta y respuesta.
	conv, err := e.store.Activa(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("conversación: %v", err)
	}
	if len(conv.Mensajes) != 2 {
		t.Errorf("el hilo quedó con %d mensajes, se esperaban 2", len(conv.Mensajes))
	}
	if !slices.Equal(conv.Mensajes[1].Herramientas, []string{agente.HerramientaResumen}) {
		t.Errorf("las herramientas no se guardaron: %v", conv.Mensajes[1].Herramientas)
	}
}

// LA prueba del paquete. El modelo pide los movimientos "del usuario 123"
// —inventándose el id de Ana— mientras quien pregunta es Beto. La herramienta
// tiene que responder con los datos de Beto (ninguno), no con los de Ana.
func TestUnUsuarioIdInventadoNoTieneEfecto(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.crearMovimiento(t, movimientos.TipoPague, "45000", "almuerzo secreto de Ana")

	argumentos := fmt.Sprintf(`{"usuario_id": %d, "usuarioId": %d, "texto": "almuerzo"}`, e.ana, e.ana)
	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaMovimientos, argumentos),
		{Contenido: "No encontré nada."},
	}

	// Pregunta BETO.
	if res := e.enviar(t, e.beto, "¿qué compré?"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	// Lo que la herramienta le devolvió al modelo no puede traer nada de Ana.
	resultado := e.proveedor.pasos[len(e.proveedor.pasos)-1]
	if resultado.Rol != agente.RolHerramienta {
		t.Fatalf("el último paso no es el resultado: %+v", resultado)
	}
	if strings.Contains(resultado.Contenido, "almuerzo secreto") {
		t.Fatalf("FUGA: la herramienta devolvió datos de otro usuario: %s", resultado.Contenido)
	}
	if !strings.Contains(resultado.Contenido, `"movimientos":[]`) {
		t.Errorf("resultado = %s (se esperaba la lista vacía de Beto)", resultado.Contenido)
	}
}

// Cuando el modelo se equivoca, el error vuelve COMO RESULTADO para que se
// corrija en la siguiente ronda. No es un fallo del servidor.
func TestLosErroresDelModeloVuelvenComoResultado(t *testing.T) {
	casos := map[string]struct {
		herramienta string
		argumentos  string
		esperado    string
	}{
		"herramienta inventada":  {"consultar_bolsa", `{}`, "no existe una herramienta"},
		"categoría inexistente":  {agente.HerramientaMovimientos, `{"categoria":"Criptomonedas"}`, "no existe la categoría"},
		"tipo inválido":          {agente.HerramientaMovimientos, `{"tipo":"regale"}`, "tipo inválido"},
		"fecha con otro formato": {agente.HerramientaMovimientos, `{"desde":"15/09/2026"}`, "AAAA-MM-DD"},
	}

	for nombre, caso := range casos {
		t.Run(nombre, func(t *testing.T) {
			e := nuevoEntorno(t, 10)
			e.proveedor.guion = []agente.Respuesta{
				pideHerramienta(caso.herramienta, caso.argumentos),
				{Contenido: "Perdón, me equivoqué."},
			}

			res := e.enviar(t, e.ana, "algo")
			if res.Code != http.StatusOK {
				t.Fatalf("un error del modelo tumbó la respuesta: %d — %s", res.Code, res.Body.String())
			}

			resultado := e.proveedor.pasos[len(e.proveedor.pasos)-1]
			if !strings.Contains(resultado.Contenido, caso.esperado) {
				t.Errorf("resultado = %s, se esperaba que mencionara %q", resultado.Contenido, caso.esperado)
			}
		})
	}
}

// La categoría que no existe vuelve con la lista de las que sí: es lo que deja
// que el modelo se corrija solo en vez de insistir.
func TestLaCategoriaInexistenteDevuelveLasQueHay(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaMovimientos, `{"categoria":"Mercado"}`),
		{Contenido: "No tienes esa categoría."},
	}

	if res := e.enviar(t, e.ana, "¿cuánto gasté en mercado?"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	resultado := e.proveedor.pasos[len(e.proveedor.pasos)-1].Contenido
	if !strings.Contains(resultado, "Negocio") {
		t.Errorf("resultado = %s (se esperaba la lista de categorías del usuario)", resultado)
	}
}

// Un modelo que se queda pidiendo datos en bucle costaría una llamada tras
// otra hasta que el router corte la petición.
func TestSeCortaElBucleDeHerramientas(t *testing.T) {
	e := nuevoEntorno(t, 10)
	// Nunca redacta: siempre pide más datos.
	e.proveedor.repetir = pideHerramienta(agente.HerramientaResumen, `{}`)

	res := e.enviar(t, e.ana, "dame vueltas")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("código = %d, se esperaba 503 — %s", res.Code, res.Body.String())
	}
	if e.proveedor.llamadas != agente.MaxRondas {
		t.Errorf("llamadas al modelo = %d, el techo es %d", e.proveedor.llamadas, agente.MaxRondas)
	}
}

// Los tokens que se guardan son los de TODA la pregunta, no los de la última
// vuelta: si no, el costo real del agente quedaría subestimado.
func TestSeSumanLosTokensDeTodasLasRondas(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaResumen, `{}`), // 100 / 20
		{Contenido: "Listo.", TokensEntrada: 400, TokensSalida: 15},
	}

	if res := e.enviar(t, e.ana, "¿cómo voy?"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	var entrada, salida int
	err := e.pool.QueryRowContext(context.Background(),
		"SELECT tokens_entrada, tokens_salida FROM agente_mensajes WHERE usuario_id = $1 AND rol = $2 ORDER BY id DESC LIMIT 1",
		e.ana, agente.RolAgente).Scan(&entrada, &salida)
	if err != nil {
		t.Fatalf("consultando los tokens: %v", err)
	}
	if entrada != 500 || salida != 35 {
		t.Errorf("tokens guardados = %d/%d, se esperaban 500/35", entrada, salida)
	}
}

// El catálogo que se le ofrece al modelo no puede incluir el id del usuario en
// ningún argumento: ese dato lo pone el servidor y no se negocia.
func TestNingunaHerramientaPideElUsuario(t *testing.T) {
	e := nuevoEntorno(t, 10)

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}
	if len(e.proveedor.herramientas) == 0 {
		t.Fatal("no se le ofreció ninguna herramienta al modelo")
	}

	for _, h := range e.proveedor.herramientas {
		propiedades, _ := h.Parametros["properties"].(map[string]any)
		for campo := range propiedades {
			if strings.Contains(strings.ToLower(campo), "usuario") || strings.Contains(strings.ToLower(campo), "user") {
				t.Errorf("la herramienta %s acepta un argumento %q: el usuario no puede ser un dato del modelo", h.Nombre, campo)
			}
		}
	}
}

// --------------------------------------------------------------------------
// Fase 2: proponer, confirmar y escribir
// --------------------------------------------------------------------------

// confirmar y descartar hacen la petición como la haría la tarjeta.
func (e *entorno) confirmar(t *testing.T, usuarioID, propuestaID int64, cuerpo any) *httptest.ResponseRecorder {
	t.Helper()

	datos, err := json.Marshal(cuerpo)
	if err != nil {
		t.Fatalf("armando el cuerpo: %v", err)
	}

	ruta := fmt.Sprintf("/propuestas/%d/confirmar", propuestaID)
	req := httptest.NewRequest(http.MethodPost, ruta, strings.NewReader(string(datos)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), usuarioID))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	return res
}

func (e *entorno) descartar(t *testing.T, usuarioID, propuestaID int64) *httptest.ResponseRecorder {
	t.Helper()

	ruta := fmt.Sprintf("/propuestas/%d", propuestaID)
	req := httptest.NewRequest(http.MethodDelete, ruta, nil).
		WithContext(httpx.ConUsuarioID(context.Background(), usuarioID))

	res := httptest.NewRecorder()
	e.handler.Rutas().ServeHTTP(res, req)
	return res
}

// proponerAlmuerzo deja al modelo pidiendo un movimiento y devuelve la
// propuesta que quedó pendiente.
func (e *entorno) proponerAlmuerzo(t *testing.T) agente.Propuesta {
	t.Helper()

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"pague","monto":"45000","fecha":"2026-09-16","descripcion":"almuerzo","categoria":"Negocio","medio_pago":"Efectivo"}`),
		{Contenido: "Te preparé el gasto de $45.000. Confírmalo ahí abajo."},
	}

	res := e.enviar(t, e.ana, "pagué 45 mil de almuerzo en efectivo")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 1 {
		t.Fatalf("propuestas = %d, se esperaba 1", len(envio.Propuestas))
	}
	return envio.Propuestas[0]
}

func (e *entorno) contarMovimientos(t *testing.T, usuarioID int64) int {
	t.Helper()

	var n int
	if err := e.pool.QueryRowContext(context.Background(),
		"SELECT count(*) FROM movimientos WHERE usuario_id = $1", usuarioID).Scan(&n); err != nil {
		t.Fatalf("contando movimientos: %v", err)
	}
	return n
}

// LA prueba de la fase: el agente propone y en la base del dinero NO pasa
// nada. Si esto se rompe, se rompió lo único que hace que valga la pena tener
// una tabla de propuestas.
func TestProponerNoEscribeNada(t *testing.T) {
	e := nuevoEntorno(t, 10)

	propuesta := e.proponerAlmuerzo(t)

	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Fatalf("proponer creó %d movimientos: el modelo no puede escribir", n)
	}
	if propuesta.Tipo != agente.TipoPropuestaMovimiento {
		t.Errorf("tipo = %q", propuesta.Tipo)
	}

	// La tarjeta llega con los nombres ya resueltos y el monto normalizado.
	var datos struct {
		Monto       string `json:"monto"`
		Categoria   string `json:"categoria"`
		CategoriaID int64  `json:"categoria_id"`
		MedioPago   string `json:"medio_pago"`
		Tipo        string `json:"tipo"`
	}
	if err := json.Unmarshal(propuesta.Datos, &datos); err != nil {
		t.Fatalf("datos ilegibles: %v", err)
	}
	// El monto va normalizado por el paquete dinero, igual que si viniera del
	// formulario: sin puntos de miles y sin decimales de relleno.
	if datos.Monto != "45000" || datos.Categoria != "Negocio" || datos.MedioPago != "Efectivo" {
		t.Errorf("datos = %+v", datos)
	}
	if datos.CategoriaID != e.categoria {
		t.Errorf("categoria_id = %d, se esperaba %d", datos.CategoriaID, e.categoria)
	}
}

func TestConfirmarCreaElMovimiento(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	// El usuario corrige el monto en la tarjeta antes de guardar: eso es
	// justamente para lo que sirve.
	res := e.confirmar(t, e.ana, propuesta.ID, map[string]any{
		"categoria_id":  e.categoria,
		"medio_pago_id": e.efectivo,
		"tipo":          "pague",
		"monto":         "48000",
		"fecha":         "2026-09-16",
		"descripcion":   "almuerzo",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("confirmar: %d — %s", res.Code, res.Body.String())
	}

	var creado movimientos.Movimiento
	if err := json.Unmarshal(res.Body.Bytes(), &creado); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if creado.Monto != "48000.00" {
		t.Errorf("se guardó %s: la tarjeta manda sobre lo que propuso el modelo", creado.Monto)
	}

	// Y queda el rastro de qué propuesta lo creó.
	var movimientoID int64
	var estado string
	err := e.pool.QueryRowContext(context.Background(),
		"SELECT estado, coalesce(movimiento_id, 0) FROM agente_propuestas WHERE id = $1", propuesta.ID).
		Scan(&estado, &movimientoID)
	if err != nil {
		t.Fatalf("consultando la propuesta: %v", err)
	}
	if estado != agente.EstadoPropuestaConfirmada || movimientoID != creado.ID {
		t.Errorf("propuesta = %s / movimiento %d", estado, movimientoID)
	}
}

// Dos clics en "Guardar" no pueden dejar el gasto dos veces.
// EL BUG: el usuario anota algo, guarda la tarjeta y en el mensaje siguiente
// el agente le dice "revisa la tarjeta y dale aceptar" — pero en pantalla no
// hay ninguna.
//
// Pasaba porque el hilo NO cuenta nada de las tarjetas: del ir y venir con las
// herramientas solo queda el texto final, así que lo único que el modelo
// volvía a leer era su propia frase del turno anterior ("te la dejé
// preparada"), y la daba por cierta para siempre.
func TestElModeloSabeQueLaTarjetaAnteriorYaSeConfirmo(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	res := e.confirmar(t, e.ana, propuesta.ID, map[string]any{
		"categoria_id":  e.categoria,
		"medio_pago_id": e.efectivo,
		"tipo":          "pague",
		"monto":         "45000",
		"fecha":         "2026-09-16",
		"descripcion":   "almuerzo",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("confirmar: %d — %s", res.Code, res.Body.String())
	}

	// Segundo mensaje: al modelo tiene que llegarle que esa tarjeta ya no está
	// en pantalla.
	e.proveedor.guion = []agente.Respuesta{{Contenido: "Listo, ya quedó guardado ese almuerzo."}}
	if res := e.enviar(t, e.ana, "gracias"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	sistema := e.proveedor.sistema
	if !strings.Contains(sistema, "ESTADO DE LAS TARJETAS") {
		t.Fatal("el mensaje de sistema no le cuenta al modelo el estado de las tarjetas")
	}
	if !strings.Contains(sistema, "CONFIRMÓ") {
		t.Errorf("no dice que el usuario ya confirmó la tarjeta:\n%s", sistema)
	}
	if !strings.Contains(sistema, "NO hay ninguna tarjeta en pantalla") {
		t.Errorf("no avisa que la pantalla quedó sin tarjetas:\n%s", sistema)
	}
}

// Y si aun así manda a confirmar una tarjeta que no existe, no sale a
// pantalla: se le devuelve el aviso y se le deja rehacer la respuesta.
func TestNoSaleUnaRespuestaQueMandaAUnaTarjetaInexistente(t *testing.T) {
	e := nuevoEntorno(t, 10)

	// El modelo propone con una categoría que no existe —la herramienta se la
	// rechaza— y aun así contesta como si la tarjeta hubiera quedado.
	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"pague","monto":"45000","fecha":"2026-09-16","descripcion":"almuerzo","categoria":"Comida","medio_pago":"Efectivo"}`),
		{Contenido: "Listo, te lo dejé preparado: revisa la tarjeta y dale a guardar."},
		{Contenido: "¿En qué categoría lo pongo? Las tuyas son Negocio."},
	}

	res := e.enviar(t, e.ana, "pagué 45 mil de almuerzo")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Mensaje    agente.Mensaje     `json:"mensaje"`
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}

	if len(envio.Propuestas) != 0 {
		t.Fatalf("propuestas = %d: la herramienta rechazó la categoría, no debería haber ninguna", len(envio.Propuestas))
	}
	if strings.Contains(strings.ToLower(envio.Mensaje.Contenido), "revisa la tarjeta") {
		t.Errorf("salió a pantalla una respuesta que manda a una tarjeta que no existe: %q", envio.Mensaje.Contenido)
	}

	// El aviso es interno: se le da al modelo, no se guarda en el hilo.
	if strings.Contains(envio.Mensaje.Contenido, "aviso del sistema") {
		t.Errorf("el aviso interno se le mostró al usuario: %q", envio.Mensaje.Contenido)
	}
}

func TestConfirmarDosVecesNoDuplica(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	cuerpo := map[string]any{
		"categoria_id": e.categoria, "medio_pago_id": e.efectivo,
		"tipo": "pague", "monto": "45000", "fecha": "2026-09-16", "descripcion": "almuerzo",
	}

	if res := e.confirmar(t, e.ana, propuesta.ID, cuerpo); res.Code != http.StatusCreated {
		t.Fatalf("primera confirmación: %d", res.Code)
	}
	if res := e.confirmar(t, e.ana, propuesta.ID, cuerpo); res.Code != http.StatusConflict {
		t.Errorf("segunda confirmación = %d, se esperaba 409", res.Code)
	}
	if n := e.contarMovimientos(t, e.ana); n != 1 {
		t.Errorf("quedaron %d movimientos, se esperaba 1", n)
	}
}

// La tarjeta es editable, así que lo que llega al confirmar puede ser
// cualquier cosa: tiene que pasar por las mismas reglas del formulario.
func TestConfirmarValidaComoElFormulario(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	res := e.confirmar(t, e.ana, propuesta.ID, map[string]any{
		"categoria_id": e.categoria, "medio_pago_id": e.efectivo,
		"tipo": "pague", "monto": "-5", "fecha": "2026-09-16", "descripcion": "almuerzo",
	})
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("código = %d, se esperaba 422 — %s", res.Code, res.Body.String())
	}
	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Errorf("se creó un movimiento con monto inválido")
	}

	// Y la tarjeta sigue viva para que el usuario corrija.
	var estado string
	if err := e.pool.QueryRowContext(context.Background(),
		"SELECT estado FROM agente_propuestas WHERE id = $1", propuesta.ID).Scan(&estado); err != nil {
		t.Fatalf("consultando la propuesta: %v", err)
	}
	if estado != agente.EstadoPropuestaPendiente {
		t.Errorf("la propuesta quedó en %q tras fallar: el usuario no podría reintentar", estado)
	}
}

func TestNoSePuedeConfirmarLaPropuestaDeOtro(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	res := e.confirmar(t, e.beto, propuesta.ID, map[string]any{
		"categoria_id": e.categoria, "medio_pago_id": e.efectivo,
		"tipo": "pague", "monto": "45000", "fecha": "2026-09-16", "descripcion": "robado",
	})
	if res.Code != http.StatusNotFound {
		t.Fatalf("código = %d, se esperaba 404", res.Code)
	}
	if n := e.contarMovimientos(t, e.beto); n != 0 {
		t.Errorf("Beto creó %d movimientos con una propuesta ajena", n)
	}
	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Errorf("se escribió en la cuenta de Ana desde otra sesión")
	}
}

func TestDescartarNoEscribeNada(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	if res := e.descartar(t, e.ana, propuesta.ID); res.Code != http.StatusNoContent {
		t.Fatalf("descartar: %d", res.Code)
	}
	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Errorf("descartar creó %d movimientos", n)
	}

	// Descartada ya no se puede confirmar.
	res := e.confirmar(t, e.ana, propuesta.ID, map[string]any{
		"categoria_id": e.categoria, "medio_pago_id": e.efectivo,
		"tipo": "pague", "monto": "45000", "fecha": "2026-09-16", "descripcion": "almuerzo",
	})
	if res.Code != http.StatusConflict {
		t.Errorf("confirmar una descartada = %d, se esperaba 409", res.Code)
	}
}

// Confirmar hoy un "pagué 45 mil" de anteayer entraría con la fecha de la
// tarjeta y descuadraría el mes sin que nadie se entere.
func TestLaPropuestaCaduca(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	_, err := e.pool.ExecContext(context.Background(),
		"UPDATE agente_propuestas SET creada_en = now() - interval '25 hours' WHERE id = $1", propuesta.ID)
	if err != nil {
		t.Fatalf("envejeciendo la propuesta: %v", err)
	}

	res := e.confirmar(t, e.ana, propuesta.ID, map[string]any{
		"categoria_id": e.categoria, "medio_pago_id": e.efectivo,
		"tipo": "pague", "monto": "45000", "fecha": "2026-09-14", "descripcion": "almuerzo",
	})
	if res.Code != http.StatusConflict {
		t.Fatalf("código = %d, se esperaba 409", res.Code)
	}
	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Errorf("se creó un movimiento desde una propuesta caducada")
	}

	// Y tampoco aparece ya en el chat.
	pendientes, err := e.store.PropuestasPendientes(context.Background(), e.ana)
	if err != nil {
		t.Fatalf("pendientes: %v", err)
	}
	if len(pendientes) != 0 {
		t.Errorf("la propuesta caducada sigue apareciendo: %+v", pendientes)
	}
}

// Las tarjetas sin confirmar tienen que sobrevivir a recargar la página. Si
// desaparecieran, el usuario creería que se guardó algo.
func TestLasPropuestasPendientesSobrevivenLaRecarga(t *testing.T) {
	e := nuevoEntorno(t, 10)
	propuesta := e.proponerAlmuerzo(t)

	res := e.leerHilo(t, httpx.ConUsuarioID(context.Background(), e.ana))
	var conv struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &conv); err != nil {
		t.Fatalf("hilo ilegible: %v", err)
	}
	if len(conv.Propuestas) != 1 || conv.Propuestas[0].ID != propuesta.ID {
		t.Errorf("propuestas al recargar = %+v", conv.Propuestas)
	}
}

// El ciclo completo del préstamo cobrado: el agente lo busca, propone, y el
// usuario confirma eligiendo por dónde le pagaron.
func TestCobrarUnPrestamo(t *testing.T) {
	e := nuevoEntorno(t, 10)
	prestamo := e.crearPrestamo(t, "200000", "Juan")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerPagado,
			fmt.Sprintf(`{"movimiento_id": %d}`, prestamo.ID)),
		{Contenido: "Te preparé el cobro del préstamo de Juan."},
	}

	res := e.enviar(t, e.ana, "ya me pagó Juan")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 1 || envio.Propuestas[0].Tipo != agente.TipoPropuestaMarcarPagado {
		t.Fatalf("propuestas = %+v", envio.Propuestas)
	}

	// Sigue pendiente mientras no confirme.
	actual, err := e.movimientos.PorID(context.Background(), e.ana, prestamo.ID)
	if err != nil {
		t.Fatalf("consultando el préstamo: %v", err)
	}
	if *actual.Estado != movimientos.EstadoPendiente {
		t.Fatalf("el préstamo se marcó pagado sin confirmación")
	}

	// El usuario elige por dónde le pagaron y confirma.
	if res := e.confirmar(t, e.ana, envio.Propuestas[0].ID, map[string]any{"medio_cobro_id": e.banco}); res.Code != http.StatusOK {
		t.Fatalf("confirmar: %d — %s", res.Code, res.Body.String())
	}

	actual, err = e.movimientos.PorID(context.Background(), e.ana, prestamo.ID)
	if err != nil {
		t.Fatalf("consultando el préstamo: %v", err)
	}
	if *actual.Estado != movimientos.EstadoPagado {
		t.Errorf("estado = %s", *actual.Estado)
	}
	// Por dónde le pagaron ya no vive en una columna del movimiento: vive en
	// el abono que se creó al saldarlo. Un préstamo se puede devolver en tres
	// pedazos y por tres medios distintos, y una sola columna no podría
	// contarlo.
	abonos, err := e.movimientos.ListarAbonos(context.Background(), e.ana, prestamo.ID)
	if err != nil {
		t.Fatalf("consultando los abonos: %v", err)
	}
	if len(abonos) != 1 || abonos[0].MedioID == nil || *abonos[0].MedioID != e.banco {
		t.Errorf("no quedó registrado por dónde le pagaron: %+v", abonos)
	}
}

// Anotar una DEUDA es el camino más largo del chat, y por ahí se rompía: un
// préstamo necesita listar_contrapartes además de las dos listas de siempre,
// y con eso se pasaba del tope de rondas. El usuario recibía un 503 ("el
// asistente se enredó consultando tus datos"), sin tarjeta y con el mensaje ya
// descontado de su cuota.
func TestUnPrestamoCabeEnElTopeDeRondas(t *testing.T) {
	e := nuevoEntorno(t, 10)

	// El modelo pide las herramientas de a una, que es lo normal, y sigue el
	// orden que le exige el prompt antes de escribir el nombre de alguien. Y
	// se equivoca una vez —la categoría que se imaginó no existe—, que es lo
	// que de verdad hay que aguantar: sin margen para un tropiezo, el camino
	// más largo del chat termina siempre en 503.
	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaContrapartes, `{}`),
		pideHerramienta(agente.HerramientaMovimientos, `{"tipo":"preste","limite":5}`),
		pideHerramienta(agente.HerramientaCategorias, `{}`),
		pideHerramienta(agente.HerramientaMedios, `{}`),
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"preste","monto":"200000","fecha":"2026-09-18","descripcion":"préstamo","categoria":"Préstamos","medio_pago":"Efectivo","a_quien":"Carlos"}`),
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"preste","monto":"200000","fecha":"2026-09-18","descripcion":"préstamo","categoria":"Negocio","medio_pago":"Efectivo","a_quien":"Carlos"}`),
		{Contenido: "Te preparé el préstamo de $200.000 a Carlos; confírmalo ahí."},
	}

	res := e.enviar(t, e.ana, "le presté 200 mil a Carlos")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 1 {
		t.Fatalf("propuestas = %d, se esperaba 1", len(envio.Propuestas))
	}
}

// Y el estado no le cuesta una ronda: una deuda nueva nace pendiente. Antes,
// omitirlo —que es lo que hace un modelo al que nadie le dijo que era
// obligatorio— se llevaba una vuelta entera en un rechazo de la validación.
func TestUnaDeudaNuevaNacePendienteSinGastarUnaRonda(t *testing.T) {
	casos := []struct {
		tipo   string
		texto  string
		aQuien string
	}{
		{tipo: movimientos.TipoPreste, texto: "le presté 200 mil a Carlos", aQuien: "Carlos"},
		{tipo: movimientos.TipoMePrestaron, texto: "el negocio me prestó 500 mil", aQuien: "Negocio 2"},
	}

	for _, caso := range casos {
		t.Run(caso.tipo, func(t *testing.T) {
			e := nuevoEntorno(t, 10)

			// Sin "estado": el modelo no lo mandó.
			e.proveedor.guion = []agente.Respuesta{
				pideHerramienta(agente.HerramientaProponerMovimiento, fmt.Sprintf(
					`{"tipo":%q,"monto":"200000","fecha":"2026-09-18","descripcion":"préstamo","categoria":"Negocio","medio_pago":"Efectivo","a_quien":%q}`,
					caso.tipo, caso.aQuien)),
				{Contenido: "Te lo preparé; confírmalo ahí."},
			}

			res := e.enviar(t, e.ana, caso.texto)
			if res.Code != http.StatusOK {
				t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
			}

			var envio struct {
				Propuestas []agente.Propuesta `json:"propuestas"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
				t.Fatalf("respuesta ilegible: %v", err)
			}
			if len(envio.Propuestas) != 1 {
				t.Fatalf("propuestas = %d: la deuda se rechazó por el estado que falta", len(envio.Propuestas))
			}

			var datos struct {
				Tipo   string `json:"tipo"`
				Estado string `json:"estado"`
				AQuien string `json:"a_quien"`
			}
			if err := json.Unmarshal(envio.Propuestas[0].Datos, &datos); err != nil {
				t.Fatalf("datos ilegibles: %v", err)
			}
			if datos.Estado != movimientos.EstadoPendiente {
				t.Errorf("estado = %q, se esperaba pendiente", datos.Estado)
			}
			if datos.Tipo != caso.tipo || datos.AQuien != caso.aQuien {
				t.Errorf("la tarjeta quedó con %+v", datos)
			}
		})
	}
}

// Lo contrario también: si el usuario dijo que ya se la pagaron, manda el
// modelo y no el valor por defecto.
func TestSiLaDeudaYaEstaSaldadaElModeloMandaElEstado(t *testing.T) {
	e := nuevoEntorno(t, 10)

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"preste","monto":"200000","fecha":"2026-09-18","descripcion":"préstamo","categoria":"Negocio","medio_pago":"Efectivo","a_quien":"Carlos","estado":"pagado"}`),
		{Contenido: "Te lo preparé; confírmalo ahí."},
	}

	res := e.enviar(t, e.ana, "le presté 200 mil a Carlos el lunes y ya me los devolvió")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 1 {
		t.Fatalf("propuestas = %d, se esperaba 1", len(envio.Propuestas))
	}

	var datos struct {
		Estado string `json:"estado"`
	}
	if err := json.Unmarshal(envio.Propuestas[0].Datos, &datos); err != nil {
		t.Fatalf("datos ilegibles: %v", err)
	}
	if datos.Estado != movimientos.EstadoPagado {
		t.Errorf("estado = %q: el valor por defecto le pisó lo que dijo el usuario", datos.Estado)
	}
}

// El préstamo de otro no existe para el agente, aunque el modelo acierte el id.
func TestNoSePuedeProponerSobreElPrestamoDeOtro(t *testing.T) {
	e := nuevoEntorno(t, 10)
	prestamo := e.crearPrestamo(t, "200000", "Juan")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerPagado, fmt.Sprintf(`{"movimiento_id": %d}`, prestamo.ID)),
		{Contenido: "No encontré ese préstamo."},
	}

	// Pregunta BETO, con el id de un préstamo de Ana.
	res := e.enviar(t, e.beto, "ya me pagaron")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 0 {
		t.Fatalf("se preparó una propuesta sobre el préstamo de otro: %+v", envio.Propuestas)
	}

	resultado := e.proveedor.pasos[len(e.proveedor.pasos)-1].Contenido
	if !strings.Contains(resultado, "no tiene ningún movimiento") {
		t.Errorf("resultado = %s", resultado)
	}
}

// Una categoría que no existe no se convierte en una propuesta: vuelve como
// aviso para que el modelo pregunte o use una real.
func TestNoProponeConCategoriaInventada(t *testing.T) {
	e := nuevoEntorno(t, 10)

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"pague","monto":"45000","fecha":"2026-09-16","descripcion":"almuerzo","categoria":"Comida","medio_pago":"Efectivo"}`),
		{Contenido: "¿En qué categoría lo pongo?"},
	}

	res := e.enviar(t, e.ana, "pagué 45 mil de almuerzo")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d", res.Code)
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 0 {
		t.Errorf("se propuso con una categoría inventada: %+v", envio.Propuestas)
	}

	resultado := e.proveedor.pasos[len(e.proveedor.pasos)-1].Contenido
	if !strings.Contains(resultado, "no existe la categoría") || !strings.Contains(resultado, "Negocio") {
		t.Errorf("resultado = %s", resultado)
	}
}

// El prompt le pide al modelo que, cuando el usuario no diga la categoria ni
// el medio, los BUSQUE en sus listas antes de proponer nada, en vez de
// escribir el nombre que le suene. Esa conducta no se puede probar aqui —el
// proveedor es de mentiras y hace lo que diga el guion—, pero si se puede
// probar que el camino le alcanza: son cinco vueltas al modelo, y con el techo
// viejo de cuatro la propuesta quedaba creada y el usuario recibia un 503.
func TestConsultarLasListasAntesDeProponerCabeEnElTope(t *testing.T) {
	e := nuevoEntorno(t, 10)

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaCategorias, `{}`),
		pideHerramienta(agente.HerramientaMedios, `{}`),
		pideHerramienta(agente.HerramientaMovimientos, `{"limite":5}`),
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"pague","monto":"45000","fecha":"2026-09-16","descripcion":"almuerzo","categoria":"Negocio","medio_pago":"Efectivo"}`),
		{Contenido: "Lo puse en Negocio y en Efectivo; cámbialo ahí si no es."},
	}

	res := e.enviar(t, e.ana, "pagué 45 mil de almuerzo")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Mensaje    agente.Mensaje     `json:"mensaje"`
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}

	if len(envio.Propuestas) != 1 {
		t.Fatalf("propuestas = %d, se esperaba 1", len(envio.Propuestas))
	}
	var datos struct {
		Categoria string `json:"categoria"`
		MedioPago string `json:"medio_pago"`
	}
	if err := json.Unmarshal(envio.Propuestas[0].Datos, &datos); err != nil {
		t.Fatalf("datos de la propuesta ilegibles: %v", err)
	}
	if datos.Categoria != "Negocio" || datos.MedioPago != "Efectivo" {
		t.Errorf("la propuesta no quedó con lo que eligió de las listas: %+v", datos)
	}

	// El rastro que ve el usuario debajo de la respuesta tiene que mostrar que
	// fue a mirar sus listas: es la diferencia entre elegir y adivinar.
	esperadas := []string{
		agente.HerramientaCategorias,
		agente.HerramientaMedios,
		agente.HerramientaMovimientos,
		agente.HerramientaProponerMovimiento,
	}
	if !slices.Equal(envio.Mensaje.Herramientas, esperadas) {
		t.Errorf("herramientas = %v, se esperaban %v", envio.Mensaje.Herramientas, esperadas)
	}

	if e.proveedor.llamadas != 5 {
		t.Errorf("llamadas al modelo = %d, se esperaban 5", e.proveedor.llamadas)
	}
	if e.proveedor.llamadas > agente.MaxRondas {
		t.Errorf("el camino que pide el prompt no cabe en MaxRondas = %d", agente.MaxRondas)
	}
}

// --------------------------------------------------------------------------
// El asistente es parte del plan: sin IA en el plan, no hay chat.
// --------------------------------------------------------------------------

func TestSinIAEnElPlanNoHayChat(t *testing.T) {
	e := nuevoEntorno(t, 50)

	sinPlan := crearUsuario(t, e.pool, "SinPlan")

	conPlanSinIA := crearUsuario(t, e.pool, "Basico")
	asignarPlan(t, e.pool, conPlanSinIA, crearPlan(t, e.pool, false))

	for nombre, usuario := range map[string]int64{"sin plan": sinPlan, "plan sin IA": conPlanSinIA} {
		if res := e.enviar(t, usuario, "¿cuánto gasté?"); res.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, se esperaba 403", nombre, res.Code)
		}
		ctx := httpx.ConUsuarioID(context.Background(), usuario)
		if res := e.leerHilo(t, ctx); res.Code != http.StatusForbidden {
			t.Errorf("%s: leer el hilo dio %d, se esperaba 403", nombre, res.Code)
		}
	}

	// Lo que importa es la plata: el modelo ni se enteró.
	if e.proveedor.llamadas != 0 {
		t.Errorf("se llamó al modelo %d veces para usuarios sin IA", e.proveedor.llamadas)
	}
}

// Quitarle la IA a un plan corta el chat desde el mensaje siguiente: no espera
// a que venza la sesión.
func TestQuitarLaIAAlPlanCortaElChatDeInmediato(t *testing.T) {
	e := nuevoEntorno(t, 50)

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusOK {
		t.Fatalf("con IA: status = %d, se esperaba 200 (%s)", res.Code, res.Body.String())
	}

	if _, err := e.pool.ExecContext(context.Background(),
		`UPDATE planes SET incluye_ia = false
		 WHERE id = (SELECT plan_id FROM usuarios WHERE id = $1)`, e.ana); err != nil {
		t.Fatalf("quitando la IA: %v", err)
	}

	if res := e.enviar(t, e.ana, "hola otra vez"); res.Code != http.StatusForbidden {
		t.Errorf("sin IA: status = %d, se esperaba 403", res.Code)
	}
}

// Si alguien arma el handler sin decir quién puede usarlo, la puerta queda
// cerrada, no abierta para todos.
func TestSinPermisoConfiguradoNadieEntra(t *testing.T) {
	e := nuevoEntorno(t, 50)
	catalogo := agente.NuevoCatalogo(e.movimientos, categorias.NewStore(e.pool), medios.NewStore(e.pool))
	e.handler = agente.NewHandler(e.store, e.proveedor, catalogo, 50, nil)

	if res := e.enviar(t, e.ana, "hola"); res.Code != http.StatusForbidden {
		t.Errorf("status = %d, se esperaba 403", res.Code)
	}
}

// --------------------------------------------------------------- recurrentes

// crearRecurrente deja un gasto que se repite, con su ocurrencia pendiente:
// es lo que el usuario ve en el resumen esperando un clic.
func (e *entorno) crearRecurrente(t *testing.T, descripcion, monto string) recurrentes.Ocurrencia {
	t.Helper()
	ctx := context.Background()

	rec, err := e.recurrentes.Crear(ctx, e.ana, recurrentes.Datos{
		CategoriaID: e.categoria,
		MedioPagoID: e.efectivo,
		Tipo:        movimientos.TipoPague,
		Monto:       monto,
		Descripcion: descripcion,
		Frecuencia:  recurrentes.Mensual,
		Dia:         time.Now().Day(),
		Desde:       time.Now().AddDate(-1, 0, 0).Format("2006-01-02"),
		Activo:      true,
	})
	if err != nil {
		t.Fatalf("creando el recurrente: %v", err)
	}

	if _, err := e.recurrentes.Generar(ctx, *rec, e.ana, time.Now()); err != nil {
		t.Fatalf("generando ocurrencias: %v", err)
	}

	pendientes, err := e.recurrentes.Pendientes(ctx, e.ana, time.Now())
	if err != nil {
		t.Fatalf("listando pendientes: %v", err)
	}
	if len(pendientes) == 0 {
		t.Fatal("el recurrente no dejó ninguna ocurrencia pendiente")
	}
	return pendientes[len(pendientes)-1]
}

// EL BUG QUE ESTO EVITA: el chat y el resumen escribían por caminos distintos,
// así que decirle "ya pagué el internet" creaba un gasto suelto Y dejaba la
// ocurrencia esperando. Se confirmaba también y el internet quedaba dos veces.
func TestConfirmarUnRecurrenteDesdeElChatNoDuplicaElGasto(t *testing.T) {
	e := nuevoEntorno(t, 10)
	pendiente := e.crearRecurrente(t, "internet", "100000")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaRecurrentesPendientes, `{}`),
		pideHerramienta(agente.HerramientaProponerRecurrente,
			fmt.Sprintf(`{"ocurrencia_id": %d}`, pendiente.ID)),
		{Contenido: "Te preparé el internet de $100.000, confírmalo ahí."},
	}

	res := e.enviar(t, e.ana, "ya pagué el internet")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 1 || envio.Propuestas[0].Tipo != agente.TipoPropuestaRecurrente {
		t.Fatalf("propuestas = %+v, se esperaba una de tipo recurrente", envio.Propuestas)
	}

	// Todavía no se ha registrado nada: es una tarjeta, no una escritura.
	if n := e.contarMovimientos(t, e.ana); n != 0 {
		t.Fatalf("proponer creó %d movimientos", n)
	}

	res = e.confirmar(t, e.ana, envio.Propuestas[0].ID, map[string]any{
		"categoria_id":  e.categoria,
		"medio_pago_id": e.efectivo,
		"tipo":          "pague",
		"monto":         "100000",
		"fecha":         pendiente.Fecha,
		"descripcion":   "internet",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("confirmar: %d — %s", res.Code, res.Body.String())
	}

	// UN movimiento, no dos.
	if n := e.contarMovimientos(t, e.ana); n != 1 {
		t.Errorf("quedaron %d movimientos: el recurrente se registró más de una vez", n)
	}

	// Y la ocurrencia dejó de estar pendiente: el resumen ya no la pide.
	pendientes, err := e.recurrentes.Pendientes(context.Background(), e.ana, time.Now())
	if err != nil {
		t.Fatalf("listando pendientes: %v", err)
	}
	for _, o := range pendientes {
		if o.ID == pendiente.ID {
			t.Error("el recurrente sigue pendiente en el resumen después de pagarlo por el chat")
		}
	}
}

// El recibo casi nunca llega igual: si dijo un valor distinto, ese es el que
// va en la tarjeta — y la plantilla no se mueve.
func TestElMontoQueDijoLaPersonaMandaSobreElDeLaPlantilla(t *testing.T) {
	e := nuevoEntorno(t, 10)
	pendiente := e.crearRecurrente(t, "luz", "100000")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerRecurrente,
			fmt.Sprintf(`{"ocurrencia_id": %d, "monto": "118000"}`, pendiente.ID)),
		{Contenido: "Lo dejé en $118.000 y no en los $100.000 de siempre; confírmalo ahí."},
	}

	res := e.enviar(t, e.ana, "pagué la luz, vinieron 118 mil este mes")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 1 {
		t.Fatalf("propuestas = %d, se esperaba 1", len(envio.Propuestas))
	}

	var datos struct {
		Monto          string `json:"monto"`
		MontoDeSiempre string `json:"monto_de_siempre"`
	}
	if err := json.Unmarshal(envio.Propuestas[0].Datos, &datos); err != nil {
		t.Fatalf("datos ilegibles: %v", err)
	}
	if datos.Monto != "118000" {
		t.Errorf("monto = %q, se esperaba el que dijo la persona", datos.Monto)
	}
	// El de siempre viaja también: es lo que deja ver la diferencia en la
	// tarjeta, para que no se confirme sin mirar.
	if datos.MontoDeSiempre != "100000.00" {
		t.Errorf("monto_de_siempre = %q", datos.MontoDeSiempre)
	}
}

// Sin monto, la tarjeta sale con el de siempre: no se inventa ninguno.
func TestSinMontoSeUsaElDeLaPlantilla(t *testing.T) {
	e := nuevoEntorno(t, 10)
	pendiente := e.crearRecurrente(t, "arriendo", "1200000")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerRecurrente,
			fmt.Sprintf(`{"ocurrencia_id": %d}`, pendiente.ID)),
		{Contenido: "Te lo preparé; confírmalo ahí."},
	}

	res := e.enviar(t, e.ana, "ya pagué el arriendo")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}

	var datos struct {
		Monto string `json:"monto"`
	}
	if err := json.Unmarshal(envio.Propuestas[0].Datos, &datos); err != nil {
		t.Fatalf("datos ilegibles: %v", err)
	}
	if datos.Monto != "1200000.00" {
		t.Errorf("monto = %q, se esperaba el de la plantilla", datos.Monto)
	}
}

// El recurrente de otro no existe para el chat, aunque el modelo acierte el id.
func TestNoSePuedePagarElRecurrenteDeOtro(t *testing.T) {
	e := nuevoEntorno(t, 10)
	pendiente := e.crearRecurrente(t, "internet", "100000")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerRecurrente,
			fmt.Sprintf(`{"ocurrencia_id": %d}`, pendiente.ID)),
		{Contenido: "No encontré ese pendiente."},
	}

	// Pregunta BETO, con el id de un recurrente de Ana.
	res := e.enviar(t, e.beto, "ya pagué el internet")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}
	if len(envio.Propuestas) != 0 {
		t.Fatalf("se preparó el pago del recurrente de otro: %+v", envio.Propuestas)
	}

	// Y la de Ana sigue intacta, esperándola a ella.
	pendientes, err := e.recurrentes.Pendientes(context.Background(), e.ana, time.Now())
	if err != nil {
		t.Fatalf("listando pendientes: %v", err)
	}
	if len(pendientes) == 0 {
		t.Error("el recurrente de Ana desapareció")
	}
}

// Si lo confirmó desde el resumen con la tarjeta del chat abierta, la tarjeta
// no puede volver a registrarlo.
func TestUnRecurrenteYaConfirmadoNoSeRegistraDosVeces(t *testing.T) {
	e := nuevoEntorno(t, 10)
	pendiente := e.crearRecurrente(t, "internet", "100000")

	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerRecurrente,
			fmt.Sprintf(`{"ocurrencia_id": %d}`, pendiente.ID)),
		{Contenido: "Te lo preparé; confírmalo ahí."},
	}
	res := e.enviar(t, e.ana, "ya pagué el internet")
	if res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	var envio struct {
		Propuestas []agente.Propuesta `json:"propuestas"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envio); err != nil {
		t.Fatalf("respuesta ilegible: %v", err)
	}

	// Mientras tanto, lo confirma desde el resumen.
	confirmador := recurrentes.NuevoConfirmador(e.recurrentes, e.movimientos)
	if _, campos, err := confirmador.Confirmar(context.Background(), e.ana, pendiente.ID, nil); err != nil || campos != nil {
		t.Fatalf("confirmando desde el resumen: %v — %v", err, campos)
	}

	// Ahora la tarjeta del chat: tiene que chocar, no crear un segundo gasto.
	res = e.confirmar(t, e.ana, envio.Propuestas[0].ID, map[string]any{
		"categoria_id":  e.categoria,
		"medio_pago_id": e.efectivo,
		"tipo":          "pague",
		"monto":         "100000",
		"fecha":         pendiente.Fecha,
		"descripcion":   "internet",
	})
	if res.Code != http.StatusConflict {
		t.Fatalf("confirmar la segunda vez: %d — %s", res.Code, res.Body.String())
	}
	if n := e.contarMovimientos(t, e.ana); n != 1 {
		t.Errorf("quedaron %d movimientos: el internet se registró más de una vez", n)
	}
}
