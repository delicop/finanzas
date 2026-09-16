package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidadorAcumulaErrores(t *testing.T) {
	v := NuevoValidador()

	if !v.Valido() {
		t.Fatal("un validador nuevo debería estar válido")
	}

	v.Requerido("nombre", "")
	v.Requerido("email", "  ") // solo espacios también cuenta como vacío

	if v.Valido() {
		t.Fatal("con dos campos vacíos no debería estar válido")
	}
	if len(v.Campos) != 2 {
		t.Errorf("se esperaban 2 errores, hay %d: %v", len(v.Campos), v.Campos)
	}
}

// Solo se guarda el PRIMER error de cada campo: es el más relevante y evita
// que el formulario muestre tres mensajes encima del mismo input.
func TestValidadorGuardaSoloElPrimerError(t *testing.T) {
	v := NuevoValidador()

	v.Check(false, "monto", "primero")
	v.Check(false, "monto", "segundo")

	if v.Campos["monto"] != "primero" {
		t.Errorf("se guardó %q, se esperaba el primer mensaje", v.Campos["monto"])
	}
}

func TestValidadorEmail(t *testing.T) {
	validos := []string{"a@b.co", "demo@finanzas.local", "nombre.apellido@empresa.com.co"}
	for _, e := range validos {
		v := NuevoValidador()
		v.Email("email", e)
		if !v.Valido() {
			t.Errorf("%q debería ser un correo válido", e)
		}
	}

	invalidos := []string{"", "sinarroba", "@sinusuario.com", "espacio @b.com", "a@"}
	for _, e := range invalidos {
		v := NuevoValidador()
		v.Email("email", e)
		if v.Valido() {
			t.Errorf("%q debería rechazarse como correo", e)
		}
	}
}

// El largo se mide en CARACTERES, no en bytes: "ñ" y "é" ocupan dos bytes
// pero son una sola letra para quien escribe.
func TestValidadorLargoCuentaCaracteresNoBytes(t *testing.T) {
	v := NuevoValidador()
	v.MaxLargo("nombre", "ñññññ", 5) // 5 letras, 10 bytes

	if !v.Valido() {
		t.Errorf("5 caracteres con tope 5 debería pasar: %v", v.Campos)
	}

	v2 := NuevoValidador()
	v2.MaxLargo("nombre", "ñññññх", 5) // 6 letras
	if v2.Valido() {
		t.Error("6 caracteres con tope 5 debería fallar")
	}
}

func TestValidadorMinLargo(t *testing.T) {
	v := NuevoValidador()
	v.MinLargo("clave", "1234567", 8)
	if v.Valido() {
		t.Error("7 caracteres con mínimo 8 debería fallar")
	}

	v2 := NuevoValidador()
	v2.MinLargo("clave", "12345678", 8)
	if !v2.Valido() {
		t.Error("8 caracteres con mínimo 8 debería pasar")
	}
}

// ---------------------------------------------------------------- DecodeJSON

type cuerpoPrueba struct {
	Nombre string `json:"nombre"`
	Monto  int    `json:"monto"`
}

func TestDecodeJSONValido(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"nombre":"Negocio 1","monto":500}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var dst cuerpoPrueba
	if err := DecodeJSON(w, r, &dst); err != nil {
		t.Fatalf("DecodeJSON falló con un cuerpo válido: %v", err)
	}
	if dst.Nombre != "Negocio 1" || dst.Monto != 500 {
		t.Errorf("se decodificó %+v", dst)
	}
}

func TestDecodeJSONRechazaEntradasMalas(t *testing.T) {
	casos := []struct {
		nombre string
		cuerpo string
		busca  string
	}{
		{"vacío", "", "vacío"},
		{"JSON roto", `{"nombre":`, "mal formado"},
		{"tipo incorrecto", `{"monto":"texto"}`, "tipo incorrecto"},
		{"campo desconocido", `{"nombre":"x","admin":true}`, "desconocido"},
		{"dos objetos pegados", `{"nombre":"a"}{"nombre":"b"}`, "un solo objeto"},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(c.cuerpo))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			var dst cuerpoPrueba
			err := DecodeJSON(w, r, &dst)
			if err == nil {
				t.Fatalf("se aceptó un cuerpo inválido: %s", c.cuerpo)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(c.busca)) {
				t.Errorf("el mensaje %q no menciona %q", err.Error(), c.busca)
			}
		})
	}
}

// Sin tope de tamaño, un cuerpo gigante puede tumbar el servidor por memoria.
func TestDecodeJSONCortaCuerposGigantes(t *testing.T) {
	gigante := `{"nombre":"` + strings.Repeat("a", 2<<20) + `"}`

	r := httptest.NewRequest("POST", "/", strings.NewReader(gigante))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var dst cuerpoPrueba
	if err := DecodeJSON(w, r, &dst); err == nil {
		t.Error("un cuerpo de 2 MB debería rechazarse (el tope es 1 MB)")
	}
}

func TestDecodeJSONExigeContentType(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"nombre":"x"}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	var dst cuerpoPrueba
	if err := DecodeJSON(w, r, &dst); err == nil {
		t.Error("un Content-Type que no es JSON debería rechazarse")
	}
}

// ---------------------------------------------------------------- contexto

func TestUsuarioIDEnElContexto(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)

	if _, ok := UsuarioID(r.Context()); ok {
		t.Error("un request sin autenticar no debería traer usuario")
	}

	ctx := ConUsuarioID(r.Context(), 42)
	id, ok := UsuarioID(ctx)
	if !ok || id != 42 {
		t.Errorf("UsuarioID = (%d, %v), se esperaba (42, true)", id, ok)
	}
}
