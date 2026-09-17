package registro

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finanzas/internal/httpx"
)

// registradorFalso captura lo que se intentó guardar, sin tocar la base.
type registradorFalso struct {
	llamadas []llamada
}

type llamada struct {
	contexto string
	mensaje  string
	ruta     string
	metodo   string
	panico   bool
	traza    string
}

func (r *registradorFalso) GuardarError(req *http.Request, err error, contexto string, esPanico bool, traza string) {
	r.llamadas = append(r.llamadas, llamada{
		contexto: contexto,
		mensaje:  err.Error(),
		ruta:     req.URL.Path,
		metodo:   req.Method,
		panico:   esPanico,
		traza:    traza,
	})
}

// El servidor NO se puede caer por un bug en un handler: el pánico se atrapa,
// se guarda con su traza y el usuario recibe un 500 normal.
func TestRecuperadorAtrapaYRegistraElPanico(t *testing.T) {
	falso := &registradorFalso{}
	httpx.UsarRegistrador(falso)
	t.Cleanup(func() { httpx.UsarRegistrador(nil) })

	handler := Recuperador(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		m["explota"] = "sí" // escribir en un mapa nil entra en pánico
	}))

	r := httptest.NewRequest("POST", "/api/movimientos", nil)
	w := httptest.NewRecorder()

	// Si el pánico se escapara, este test reventaría aquí mismo.
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", w.Code)
	}
	// Al usuario no se le filtra el detalle técnico.
	if strings.Contains(w.Body.String(), "nil map") {
		t.Errorf("la respuesta al usuario expone el detalle interno: %s", w.Body.String())
	}

	if len(falso.llamadas) != 1 {
		t.Fatalf("se registraron %d errores, se esperaba 1", len(falso.llamadas))
	}

	l := falso.llamadas[0]
	if !l.panico {
		t.Error("no quedó marcado como pánico")
	}
	if l.ruta != "/api/movimientos" || l.metodo != "POST" {
		t.Errorf("ruta/método mal registrados: %s %s", l.metodo, l.ruta)
	}
	if !strings.Contains(l.traza, "TestRecuperadorAtrapaYRegistraElPanico") {
		t.Error("la traza no apunta al código que falló; así no se puede depurar")
	}
}

func TestRecuperadorNoEstorbaCuandoTodoVaBien(t *testing.T) {
	falso := &registradorFalso{}
	httpx.UsarRegistrador(falso)
	t.Cleanup(func() { httpx.UsarRegistrador(nil) })

	handler := Recuperador(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/dashboard", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, se esperaba 200", w.Code)
	}
	if len(falso.llamadas) != 0 {
		t.Errorf("se registró un error en una petición exitosa: %+v", falso.llamadas)
	}
}

// ErrorInterno debe registrar el detalle pero responderle al usuario un
// mensaje genérico: un error de base de datos revela nombres de tablas.
func TestErrorInternoNoFiltraDetallesAlUsuario(t *testing.T) {
	falso := &registradorFalso{}
	httpx.UsarRegistrador(falso)
	t.Cleanup(func() { httpx.UsarRegistrador(nil) })

	r := httptest.NewRequest("GET", "/api/movimientos", nil)
	w := httptest.NewRecorder()

	secreto := "ERROR: column usuarios.password_hash does not exist"
	httpx.ErrorInterno(w, r, errSimulado(secreto), "movimientos: listando")

	cuerpo := w.Body.String()
	if strings.Contains(cuerpo, "password_hash") || strings.Contains(cuerpo, "column") {
		t.Errorf("la respuesta filtró detalles internos: %s", cuerpo)
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", w.Code)
	}

	if len(falso.llamadas) != 1 {
		t.Fatalf("se registraron %d errores, se esperaba 1", len(falso.llamadas))
	}
	// Pero en la bitácora sí queda el detalle completo: es lo que sirve
	// para arreglar el problema.
	if falso.llamadas[0].mensaje != secreto {
		t.Errorf("la bitácora guardó %q, se esperaba el error completo", falso.llamadas[0].mensaje)
	}
	if falso.llamadas[0].panico {
		t.Error("un error normal no debería marcarse como pánico")
	}
}

// Sin registrador configurado (por ejemplo en un test), nada debe reventar.
func TestFuncionaSinRegistrador(t *testing.T) {
	httpx.UsarRegistrador(nil)

	r := httptest.NewRequest("GET", "/api/x", nil)
	w := httptest.NewRecorder()

	httpx.ErrorInterno(w, r, errSimulado("algo falló"), "prueba")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", w.Code)
	}
}

func TestRecortar(t *testing.T) {
	if r := recortar("corto", 100); r != "corto" {
		t.Errorf("no debería recortar: %q", r)
	}

	largo := strings.Repeat("a", 500)
	r := recortar(largo, 100)
	if len(r) <= 100 || !strings.HasSuffix(r, "(recortado)") {
		t.Errorf("no se recortó como se esperaba: %d bytes", len(r))
	}
}

type errSimulado string

func (e errSimulado) Error() string { return string(e) }

// ---------------------------------------------------------------------------
// Peticiones que el cliente abandona
// ---------------------------------------------------------------------------

// La app cancela peticiones a propósito (cerrar el chat, cambiar de pantalla).
// La consulta que queda a medias falla con "context canceled", pero eso no es
// una falla del servidor y no debe llenar la bitácora.
func TestUnaPeticionAbandonadaNoVaALaBitacora(t *testing.T) {
	falso := &registradorFalso{}
	httpx.UsarRegistrador(falso)
	t.Cleanup(func() { httpx.UsarRegistrador(nil) })

	ctx, cancelar := context.WithCancel(context.Background())
	cancelar() // el navegador ya cortó
	r := httptest.NewRequest("GET", "/api/agente", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	httpx.ErrorInterno(w, r, fmt.Errorf("leyendo el hilo: %w", context.Canceled), "agente: leyendo")
	httpx.RegistrarFallo(r, errSimulado("otra cosa"), "agente: otra")

	if len(falso.llamadas) != 0 {
		t.Errorf("se registraron %d fallas de una petición abandonada: %+v", len(falso.llamadas), falso.llamadas)
	}
	if w.Code != httpx.StatusClienteSeFue {
		t.Errorf("status = %d, se esperaba %d", w.Code, httpx.StatusClienteSeFue)
	}
}

// Un tiempo vencido NO es lo mismo: la consulta fue lenta y eso sí se revisa.
func TestUnTiempoVencidoSiVaALaBitacora(t *testing.T) {
	falso := &registradorFalso{}
	httpx.UsarRegistrador(falso)
	t.Cleanup(func() { httpx.UsarRegistrador(nil) })

	ctx, cancelar := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelar()
	<-ctx.Done()
	r := httptest.NewRequest("GET", "/api/dashboard", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	httpx.ErrorInterno(w, r, context.DeadlineExceeded, "dashboard: calculando")

	if len(falso.llamadas) != 1 {
		t.Errorf("un tiempo vencido debería registrarse; se registraron %d", len(falso.llamadas))
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", w.Code)
	}
}

// Las tareas de cierre (deshacer una reserva) corren con su propio context:
// si fallan, dejan datos a medias aunque el cliente se haya ido.
func TestRegistrarFalloSiempreNoMiraAlCliente(t *testing.T) {
	falso := &registradorFalso{}
	httpx.UsarRegistrador(falso)
	t.Cleanup(func() { httpx.UsarRegistrador(nil) })

	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	r := httptest.NewRequest("POST", "/api/agente/propuestas/1/confirmar", nil).WithContext(ctx)

	httpx.RegistrarFalloSiempre(r, errSimulado("no se pudo deshacer"), "agente: devolviendo la propuesta")

	if len(falso.llamadas) != 1 {
		t.Errorf("una falla de cierre debe registrarse siempre; se registraron %d", len(falso.llamadas))
	}
}
