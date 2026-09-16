package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestHashYVerificacion(t *testing.T) {
	const clave = "MiClaveSegura123"

	hash, err := HashPassword(clave)
	if err != nil {
		t.Fatalf("HashPassword falló: %v", err)
	}

	// El hash nunca debe parecerse a la clave.
	if hash == clave {
		t.Fatal("el hash es igual a la contraseña en texto plano")
	}

	if !VerificarPassword(hash, clave) {
		t.Error("la contraseña correcta fue rechazada")
	}
	if VerificarPassword(hash, "OtraClave123") {
		t.Error("se aceptó una contraseña incorrecta")
	}
	if VerificarPassword(hash, "") {
		t.Error("se aceptó una contraseña vacía")
	}
}

// bcrypt mete una "sal" aleatoria en cada hash. Por eso dos personas con la
// misma contraseña tienen hashes distintos, y una tabla precalculada de
// hashes (rainbow table) no sirve de nada.
func TestHashesDistintosParaLaMismaClave(t *testing.T) {
	const clave = "MiClaveSegura123"

	h1, _ := HashPassword(clave)
	h2, _ := HashPassword(clave)

	if h1 == h2 {
		t.Fatal("dos hashes de la misma clave salieron idénticos: la sal no está funcionando")
	}
	if !VerificarPassword(h1, clave) || !VerificarPassword(h2, clave) {
		t.Error("ambos hashes deberían validar la misma contraseña")
	}
}

func TestVerificarPasswordConHashInvalido(t *testing.T) {
	// No debe entrar en pánico con basura guardada en la base de datos.
	for _, hash := range []string{"", "no-es-un-hash", "$2a$12$corto"} {
		if VerificarPassword(hash, "loquesea") {
			t.Errorf("se validó contra un hash inválido: %q", hash)
		}
	}
}

// ---------------------------------------------------------------- limitador

func TestLimitadorBloqueaTrasElTope(t *testing.T) {
	l := nuevoLimitador(3, time.Minute)

	for i := 1; i <= 3; i++ {
		if !l.permitir("10.0.0.1") {
			t.Fatalf("el intento %d debería permitirse (el tope es 3)", i)
		}
	}
	if l.permitir("10.0.0.1") {
		t.Error("el cuarto intento debería bloquearse")
	}
}

func TestLimitadorAislaPorIP(t *testing.T) {
	l := nuevoLimitador(1, time.Minute)

	l.permitir("10.0.0.1")
	if l.permitir("10.0.0.1") {
		t.Error("la primera IP debería estar bloqueada")
	}
	if !l.permitir("10.0.0.2") {
		t.Error("bloquear una IP no debe afectar a las demás")
	}
}

func TestLimitadorSeLimpiaTrasUnLoginBueno(t *testing.T) {
	l := nuevoLimitador(2, time.Minute)

	l.permitir("10.0.0.1")
	l.permitir("10.0.0.1")
	l.exito("10.0.0.1") // entró bien: se le perdona el historial

	if !l.permitir("10.0.0.1") {
		t.Error("tras un login correcto la IP debería quedar libre")
	}
}

func TestLimitadorReiniciaAlPasarLaVentana(t *testing.T) {
	// Ventana mínima para no tener que esperar en el test.
	l := nuevoLimitador(1, time.Millisecond)

	l.permitir("10.0.0.1")
	time.Sleep(3 * time.Millisecond)

	if !l.permitir("10.0.0.1") {
		t.Error("pasada la ventana, la IP debería poder intentar de nuevo")
	}
}

func TestIPDelRequest(t *testing.T) {
	casos := []struct {
		nombre     string
		remoteAddr string
		xff        string
		esperado   string
	}{
		{"sin proxy", "192.168.1.50:54321", "", "192.168.1.50"},
		{"detrás de un proxy", "10.0.0.1:8080", "201.45.9.2", "201.45.9.2"},
		{"cadena de proxies", "10.0.0.1:8080", "201.45.9.2, 10.0.0.5", "201.45.9.2"},
		{"con espacios", "10.0.0.1:8080", "  201.45.9.2  ", "201.45.9.2"},
		{"header vacío", "192.168.1.50:54321", "   ", "192.168.1.50"},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/auth/login", nil)
			r.RemoteAddr = c.remoteAddr
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}

			if obtenido := ipDelRequest(r); obtenido != c.esperado {
				t.Errorf("ipDelRequest() = %q, se esperaba %q", obtenido, c.esperado)
			}
		})
	}
}
