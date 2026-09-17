package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var secreto = []byte("un-secreto-de-pruebas-suficientemente-largo-123456")

func TestGenerarYLeerToken(t *testing.T) {
	tm := NewTokenManager(secreto, time.Hour)
	usuario := &Usuario{ID: 42, Email: "demo@finanzas.local"}

	token, expira, err := tm.Generar(usuario)
	if err != nil {
		t.Fatalf("Generar falló: %v", err)
	}
	if token == "" {
		t.Fatal("el token vino vacío")
	}
	if time.Until(expira) > time.Hour+time.Minute || time.Until(expira) < 59*time.Minute {
		t.Errorf("la expiración no cuadra con el TTL: %v", expira)
	}

	id, err := tm.UsuarioIDDesdeToken(token)
	if err != nil {
		t.Fatalf("UsuarioIDDesdeToken falló: %v", err)
	}
	if id != usuario.ID {
		t.Errorf("id = %d, se esperaba %d", id, usuario.ID)
	}
}

func TestTokenExpirado(t *testing.T) {
	// TTL negativo: nace vencido.
	tm := NewTokenManager(secreto, -time.Minute)

	token, _, err := tm.Generar(&Usuario{ID: 1})
	if err != nil {
		t.Fatalf("Generar falló: %v", err)
	}

	if _, err := tm.UsuarioIDDesdeToken(token); !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("un token vencido debería rechazarse, se obtuvo: %v", err)
	}
}

func TestTokenConOtroSecreto(t *testing.T) {
	emisor := NewTokenManager(secreto, time.Hour)
	token, _, _ := emisor.Generar(&Usuario{ID: 1})

	// Otro servidor, otra llave: la firma no debe validar.
	otro := NewTokenManager([]byte("una-llave-completamente-distinta-123456789"), time.Hour)

	if _, err := otro.UsuarioIDDesdeToken(token); !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("un token firmado con otra llave debería rechazarse, se obtuvo: %v", err)
	}
}

func TestTokenManipulado(t *testing.T) {
	tm := NewTokenManager(secreto, time.Hour)
	token, _, _ := tm.Generar(&Usuario{ID: 1})

	// Cambiamos un carácter de la MITAD de la firma. No el último: en base64 el
	// último carácter lleva bits de relleno, y cambiarlo a veces deja la firma
	// decodificada igual. Eso hacía que esta prueba fallara de vez en cuando.
	partes := strings.Split(token, ".")
	firma := []byte(partes[2])
	medio := len(firma) / 2
	if firma[medio] == 'A' {
		firma[medio] = 'B'
	} else {
		firma[medio] = 'A'
	}
	manipulado := partes[0] + "." + partes[1] + "." + string(firma)

	if _, err := tm.UsuarioIDDesdeToken(manipulado); !errors.Is(err, ErrTokenInvalido) {
		t.Errorf("un token manipulado debería rechazarse, se obtuvo: %v", err)
	}
}

// Esta es LA prueba de seguridad del JWT.
//
// El ataque "alg confusion": el atacante arma un token con alg=none (sin
// firma) y lo manda. Una librería mal configurada se lo cree y deja entrar a
// cualquiera como cualquier usuario. Lo que lo impide es jwt.WithValidMethods
// en UsuarioIDDesdeToken.
func TestRechazaAlgoritmoNone(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Subject:   "1",
		Issuer:    "finanzas-api",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	sinFirma, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("no se pudo armar el token de ataque: %v", err)
	}
	// Confirmamos que el token de ataque realmente dice alg=none, decodificando
	// su cabecera. Comparar contra un base64 escrito a mano es frágil.
	partes := strings.Split(sinFirma, ".")
	if len(partes) != 3 {
		t.Fatalf("el token de ataque no tiene tres partes: %s", sinFirma)
	}
	cabecera, err := base64.RawURLEncoding.DecodeString(partes[0])
	if err != nil {
		t.Fatalf("no se pudo decodificar la cabecera: %v", err)
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(cabecera, &h); err != nil {
		t.Fatalf("cabecera ilegible: %v", err)
	}
	if h.Alg != "none" {
		t.Fatalf("el token de ataque quedó con alg=%q, se esperaba none", h.Alg)
	}

	tm := NewTokenManager(secreto, time.Hour)
	if _, err := tm.UsuarioIDDesdeToken(sinFirma); !errors.Is(err, ErrTokenInvalido) {
		t.Fatalf("SE ACEPTÓ UN TOKEN SIN FIRMA (alg=none). Esto es un hueco de seguridad. err=%v", err)
	}
}

func TestRechazaBasura(t *testing.T) {
	tm := NewTokenManager(secreto, time.Hour)

	for _, entrada := range []string{"", "  ", "no-es-un-token", "a.b.c", "Bearer xyz"} {
		if _, err := tm.UsuarioIDDesdeToken(entrada); !errors.Is(err, ErrTokenInvalido) {
			t.Errorf("UsuarioIDDesdeToken(%q) debería fallar, se obtuvo: %v", entrada, err)
		}
	}
}
