package push

import (
	"crypto/ecdh"
	"encoding/base64"
	"strings"
	"testing"
)

// El cifrado de Web Push es la parte de toda la app donde un error no se nota:
// no revienta nada, simplemente los avisos nunca llegan al celular — o llegan
// y el navegador los descarta en silencio porque no los puede descifrar.
//
// Por eso esta prueba no comprueba "que no falle", sino que el resultado sea
// EXACTAMENTE el del ejemplo del RFC 8291 (sección 5), byte por byte. Es la
// única forma de saber que cada paso de la derivación está bien: el orden de
// las llaves públicas en el "info", el 0x02 del final, la cabecera.
func TestCifrarCoincideConElEjemploDelRFC8291(t *testing.T) {
	const (
		textoPlano = "When I grow up, I want to be a watermelon"

		// El navegador que recibe (en el RFC, el "user agent").
		suPublicaB64 = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
		authB64      = "BTBZMqHH6r4Tts7J_aSIgg"

		// El servidor que manda: su llave efímera y su sal, fijas para poder
		// comparar.
		miPrivadaB64 = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
		salB64       = "DGv6ra1nlYgDCS1FRnbzlw"

		// El resultado completo del ejemplo: 16 de sal + 4 del tamaño de
		// registro + 1 del largo + 65 de la llave + 58 cifrados = 144 bytes.
		esperadoB64 = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
			"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
			"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	)

	suPublicaBytes := deBase64(t, suPublicaB64)
	auth := deBase64(t, authB64)
	sal := deBase64(t, salB64)

	suPublica, err := ecdh.P256().NewPublicKey(suPublicaBytes)
	if err != nil {
		t.Fatalf("la llave pública del ejemplo no se pudo leer: %v", err)
	}

	efimera, err := ecdh.P256().NewPrivateKey(deBase64(t, miPrivadaB64))
	if err != nil {
		t.Fatalf("la llave privada del ejemplo no se pudo leer: %v", err)
	}

	cifrado, err := cifrarCon(suPublicaBytes, auth, suPublica, efimera, sal, []byte(textoPlano))
	if err != nil {
		t.Fatalf("cifrarCon devolvió error: %v", err)
	}

	if obtenido := codificar(cifrado); obtenido != esperadoB64 {
		t.Errorf("el cifrado no coincide con el RFC 8291\n  esperado: %s\n  obtenido: %s", esperadoB64, obtenido)
	}
}

// La cabecera del mensaje va en claro y tiene un formato fijo. Si se corre un
// byte, el navegador ni siquiera intenta descifrar.
func TestCabeceraDelMensaje(t *testing.T) {
	suPublicaBytes := deBase64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	suPublica, err := ecdh.P256().NewPublicKey(suPublicaBytes)
	if err != nil {
		t.Fatal(err)
	}
	efimera, err := ecdh.P256().NewPrivateKey(deBase64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	sal := deBase64(t, "DGv6ra1nlYgDCS1FRnbzlw")

	cifrado, err := cifrarCon(suPublicaBytes, deBase64(t, "BTBZMqHH6r4Tts7J_aSIgg"), suPublica, efimera, sal, []byte("hola"))
	if err != nil {
		t.Fatal(err)
	}

	if len(cifrado) < 86 {
		t.Fatalf("el mensaje quedó demasiado corto: %d bytes", len(cifrado))
	}
	if string(cifrado[:16]) != string(sal) {
		t.Error("los primeros 16 bytes deberían ser la sal")
	}
	// Bytes 16..20: el tamaño de registro, 4096 en big endian.
	if cifrado[16] != 0 || cifrado[17] != 0 || cifrado[18] != 0x10 || cifrado[19] != 0 {
		t.Errorf("el tamaño de registro no es 4096: %v", cifrado[16:20])
	}
	if cifrado[20] != 65 {
		t.Errorf("el largo de la llave pública debería ser 65, es %d", cifrado[20])
	}
	if cifrado[21] != 4 {
		t.Error("la llave pública debería ir sin comprimir (empieza en 0x04)")
	}
}

// Las llaves generadas tienen que poder usarse de inmediato: el error clásico
// es guardar la privada en un formato que después no se puede volver a leer.
func TestGenerarLlavesSirvenParaArrancar(t *testing.T) {
	publica, privada, err := GenerarLlaves()
	if err != nil {
		t.Fatalf("GenerarLlaves falló: %v", err)
	}

	e, err := NuevoEnviador(publica, privada, "mailto:alguien@ejemplo.com")
	if err != nil {
		t.Fatalf("las llaves recién generadas no se aceptaron: %v", err)
	}
	if e.ClavePublica() != publica {
		t.Error("la clave pública que se expone no es la que se configuró")
	}

	// La cabecera VAPID lleva el token firmado y la llave pública, y el `aud`
	// es el ORIGEN del endpoint, no la URL completa.
	cabecera, err := e.autorizacion("https://fcm.googleapis.com/fcm/send/abc123")
	if err != nil {
		t.Fatalf("no se pudo firmar el token VAPID: %v", err)
	}
	if !strings.HasPrefix(cabecera, "vapid t=") || !strings.Contains(cabecera, ", k="+publica) {
		t.Errorf("la cabecera de autorización no tiene la forma esperada: %s", cabecera)
	}
}

func TestNuevoEnviadorRechazaLoQueNoSirve(t *testing.T) {
	publica, privada, err := GenerarLlaves()
	if err != nil {
		t.Fatal(err)
	}

	casos := []struct {
		nombre   string
		publica  string
		privada  string
		contacto string
	}{
		{"pública corta", "AAAA", privada, "mailto:a@b.com"},
		{"privada corta", publica, "AAAA", "mailto:a@b.com"},
		{"sin contacto", publica, privada, ""},
		{"contacto que no es correo ni https", publica, privada, "tel:3001234567"},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if _, err := NuevoEnviador(c.publica, c.privada, c.contacto); err == nil {
				t.Error("se aceptó una configuración que no sirve")
			}
		})
	}
}

func deBase64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("no se pudo decodificar %q: %v", s, err)
	}
	return b
}
