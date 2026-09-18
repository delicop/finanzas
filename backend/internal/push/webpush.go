// Package push manda las notificaciones que llegan al celular con la app
// CERRADA.
//
// QUE ES ESTO, EN CRISTIANO
//
// Un aviso dentro de la app solo sirve si la app está abierta. Web Push
// resuelve eso: el navegador mantiene un canal abierto con el servicio de su
// fabricante (Google para Chrome, Apple para Safari, Mozilla para Firefox), y
// nosotros le entregamos el mensaje a ESE servicio, que hace de cartero.
//
// QUE ES VAPID
//
// El cartero no acepta paquetes de cualquiera: si no, cualquier servidor que
// consiga un endpoint podria bombardear al usuario. VAPID (RFC 8292) es como
// el servidor se identifica: firma un token corto con una llave privada suya,
// y manda al lado la llave publica correspondiente. El cartero verifica la
// firma y sabe que los mensajes vienen siempre del mismo remitente. No es una
// cuenta ni una contraseña con nadie: es un par de llaves que se genera una
// vez (ver cmd/vapid) y se guarda en el .env.
//
// POR QUE ADEMAS SE CIFRA
//
// El cartero entrega, pero no tiene por qué leer. El contenido va cifrado
// (RFC 8291) con una llave que solo conocen el navegador del usuario y este
// servidor, derivada de las credenciales que el propio navegador genero al
// suscribirse. Google puede ver que le mandamos algo a alguien; no puede ver
// que dice "Hoy te paga Carlos $200.000".
//
// SIN LLAVES CONFIGURADAS TODO ESTO NO EXISTE: la app funciona igual y los
// avisos siguen llegando a la campana. Mismo criterio que el chat y que el
// token de mantenimiento — lo opcional se apaga solo, no arranca a medias.
package push

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Los limites que fija el estandar.
const (
	// MaxPayload es lo que cabe cifrado en un mensaje. El estandar garantiza
	// 4096 bytes de registro; descontando cabecera y relleno quedan 3993 utiles.
	// De todos modos aqui se mandan titulos y una frase: no se acerca ni de
	// lejos, y el recorte es una red de seguridad.
	MaxPayload = 3800

	// TTL: cuanto guarda el mensaje el servicio de push si el celular esta
	// apagado. Un dia. Mas que eso y el aviso llega cuando ya no sirve
	// ("hoy te paga Carlos" el jueves).
	TTL = 24 * 60 * 60

	// vigenciaVAPID es cuanto vale el token firmado. El estandar permite 24
	// horas; 12 deja margen para relojes desfasados sin abrir mucho la ventana.
	vigenciaVAPID = 12 * time.Hour

	// tamRegistro es el "rs" de la cabecera: el tamaño maximo de registro que
	// declaramos. Un solo registro por mensaje, que es lo normal.
	tamRegistro = 4096
)

// ErrSuscripcionMuerta: el servicio de push dice que ese endpoint ya no
// existe (404) o caduco (410). El usuario desinstalo la app o limpio el
// navegador. La fila se borra; no es un fallo que haya que reintentar.
var ErrSuscripcionMuerta = errors.New("la suscripcion de push ya no existe")

// Suscripcion son las credenciales que genero el navegador del usuario.
type Suscripcion struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"` // llave publica del navegador, base64url
	Auth     string `json:"auth"`   // secreto compartido, base64url
}

// Mensaje es lo que se le muestra al usuario.
type Mensaje struct {
	Titulo string `json:"titulo"`
	Cuerpo string `json:"cuerpo"`
	// URL a la que lleva el clic, dentro de la app.
	URL string `json:"url,omitempty"`
	// Etiqueta agrupa avisos del mismo tipo: uno nuevo reemplaza al anterior
	// en la bandeja en vez de apilarse. Sin esto, tres dias sin abrir la app
	// dejan quince notificaciones iguales.
	Etiqueta string `json:"etiqueta,omitempty"`
}

// Enviador manda mensajes. Es nil cuando no hay llaves configuradas.
type Enviador struct {
	publica  []byte // 65 bytes, punto sin comprimir
	privada  *ecdsa.PrivateKey
	contacto string // mailto:... o https://...
	cliente  *http.Client
}

// NuevoEnviador arma el enviador con las llaves del .env.
//
// Falla al arrancar si las llaves no sirven, en vez de fallar en el primer
// aviso: una llave mal copiada se descubre en el log de arranque, no tres dias
// despues preguntandose por que no llegan las notificaciones.
func NuevoEnviador(publicaB64, privadaB64, contacto string) (*Enviador, error) {
	publica, err := decodificar(publicaB64)
	if err != nil {
		return nil, fmt.Errorf("PUSH_VAPID_PUBLIC no es base64url valido: %w", err)
	}
	if len(publica) != 65 || publica[0] != 4 {
		return nil, fmt.Errorf("PUSH_VAPID_PUBLIC debe ser un punto P-256 sin comprimir de 65 bytes (tiene %d)", len(publica))
	}

	privada, err := decodificar(privadaB64)
	if err != nil {
		return nil, fmt.Errorf("PUSH_VAPID_PRIVATE no es base64url valido: %w", err)
	}
	if len(privada) != 32 {
		return nil, fmt.Errorf("PUSH_VAPID_PRIVATE debe tener 32 bytes (tiene %d)", len(privada))
	}

	llave, err := clavePrivadaECDSA(privada)
	if err != nil {
		return nil, err
	}

	// El contacto es a quien escribirle si nuestros mensajes dan problemas.
	// Los servicios de push lo exigen; es parte del trato de que nos dejen
	// mandar cosas al celular de alguien.
	contacto = strings.TrimSpace(contacto)
	if !strings.HasPrefix(contacto, "mailto:") && !strings.HasPrefix(contacto, "https://") {
		return nil, errors.New("PUSH_CONTACTO debe ser un mailto:correo o una URL https")
	}

	return &Enviador{
		publica:  publica,
		privada:  llave,
		contacto: contacto,
		// Timeout corto: el envio corre dentro de la tarea de avisos, y un
		// servicio de push colgado no puede dejar sin avisos a los demas.
		cliente: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ClavePublica es lo que el navegador necesita para suscribirse. Se expone por
// la API; es publica por definicion.
func (e *Enviador) ClavePublica() string { return codificar(e.publica) }

// Enviar cifra el mensaje y lo entrega al servicio de push del navegador.
func (e *Enviador) Enviar(s Suscripcion, cuerpo []byte) error {
	if len(cuerpo) > MaxPayload {
		return fmt.Errorf("el mensaje de push pesa %d bytes, el maximo es %d", len(cuerpo), MaxPayload)
	}

	cifrado, err := e.cifrar(s, cuerpo)
	if err != nil {
		return err
	}

	token, err := e.autorizacion(s.Endpoint)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, s.Endpoint, bytes.NewReader(cifrado))
	if err != nil {
		return fmt.Errorf("armando la peticion de push: %w", err)
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", fmt.Sprint(TTL))
	req.Header.Set("Authorization", token)
	// "normal" en vez de "high": estos avisos no son una llamada entrante.
	// Con urgencia normal el celular puede agruparlos y no despertar la
	// pantalla a las 3 de la manana.
	req.Header.Set("Urgency", "normal")

	resp, err := e.cliente.Do(req)
	if err != nil {
		return fmt.Errorf("mandando el push: %w", err)
	}
	defer resp.Body.Close()
	// Se lee y se descarta para que la conexion se pueda reutilizar; sin esto
	// cada envio abre una conexion nueva.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrSuscripcionMuerta
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	default:
		return fmt.Errorf("el servicio de push respondio %d", resp.StatusCode)
	}
}

// --------------------------------------------------------------------------
// El cifrado (RFC 8291 + RFC 8188, aes128gcm)
// --------------------------------------------------------------------------

// cifrar arma el cuerpo del mensaje.
//
// El baile es este, y cada paso esta en el estandar:
//
//  1. Se genera un par de llaves EFIMERO, solo para este mensaje.
//  2. Se hace ECDH entre nuestra llave efimera y la publica del navegador:
//     los dos lados llegan al mismo secreto sin haberlo mandado nunca.
//  3. Ese secreto se mezcla con el `auth` de la suscripcion y con las dos
//     llaves publicas para sacar la llave real de cifrado y el nonce.
//  4. Se cifra con AES-128-GCM y se arma la cabecera, que lleva la sal y
//     nuestra llave publica efimera para que el navegador pueda repetir el
//     mismo calculo del otro lado.
func (e *Enviador) cifrar(s Suscripcion, texto []byte) ([]byte, error) {
	navegador, err := decodificar(s.P256dh)
	if err != nil {
		return nil, fmt.Errorf("la llave p256dh no es base64url valido: %w", err)
	}
	secretoAuth, err := decodificar(s.Auth)
	if err != nil {
		return nil, fmt.Errorf("el secreto auth no es base64url valido: %w", err)
	}

	suPublica, err := ecdh.P256().NewPublicKey(navegador)
	if err != nil {
		return nil, fmt.Errorf("la llave p256dh no es un punto P-256 valido: %w", err)
	}

	efimera, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generando la llave efimera: %w", err)
	}

	sal := make([]byte, 16)
	if _, err := rand.Read(sal); err != nil {
		return nil, fmt.Errorf("generando la sal: %w", err)
	}

	return cifrarCon(navegador, secretoAuth, suPublica, efimera, sal, texto)
}

// cifrarCon es el cifrado de verdad, con la llave efimera y la sal COMO
// PARAMETROS en vez de generadas adentro.
//
// Esa es la unica razon de que exista: asi la prueba puede fijar las dos y
// comparar el resultado, byte por byte, con el vector de ejemplo del RFC 8291.
// Con la generacion aleatoria dentro, el resultado cambia en cada corrida y lo
// unico que se podria probar es que "no revienta" — que es exactamente lo que
// tambien haria una implementacion mal hecha.
func cifrarCon(navegador, secretoAuth []byte, suPublica *ecdh.PublicKey, efimera *ecdh.PrivateKey, sal, texto []byte) ([]byte, error) {
	miPublica := efimera.PublicKey().Bytes()

	compartido, err := efimera.ECDH(suPublica)
	if err != nil {
		return nil, fmt.Errorf("calculando el secreto compartido: %w", err)
	}

	// Paso 3, primera mitad: del secreto ECDH y el `auth` sale el material de
	// partida. El "info" incluye las DOS llaves publicas, que es lo que ata
	// este cifrado a esta suscripcion y a este mensaje.
	prkAuth, err := hkdf.Extract(sha256.New, compartido, secretoAuth)
	if err != nil {
		return nil, fmt.Errorf("derivando la llave (extract): %w", err)
	}

	infoLlave := append([]byte("WebPush: info\x00"), navegador...)
	infoLlave = append(infoLlave, miPublica...)

	ikm, err := hkdf.Expand(sha256.New, prkAuth, string(infoLlave), 32)
	if err != nil {
		return nil, fmt.Errorf("derivando la llave (expand): %w", err)
	}

	// Paso 3, segunda mitad: la llave de contenido y el nonce.
	cek, err := hkdf.Key(sha256.New, ikm, sal, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("derivando la llave de contenido: %w", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, sal, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("derivando el nonce: %w", err)
	}

	bloque, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("preparando AES: %w", err)
	}
	gcm, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, fmt.Errorf("preparando GCM: %w", err)
	}

	// El 0x02 al final marca "este es el ultimo registro". Va DENTRO de lo
	// cifrado a proposito: asi nadie puede cortar el mensaje por la mitad sin
	// que se note.
	registro := append(append([]byte{}, texto...), 0x02)
	cifrado := gcm.Seal(nil, nonce, registro, nil)

	// La cabecera va en claro: sal | tamaño de registro | largo de la llave |
	// nuestra llave publica efimera. El navegador la necesita para derivar lo
	// mismo, y no revela nada (la llave publica es publica).
	var cuerpo bytes.Buffer
	cuerpo.Write(sal)
	if err := binary.Write(&cuerpo, binary.BigEndian, uint32(tamRegistro)); err != nil {
		return nil, fmt.Errorf("escribiendo la cabecera: %w", err)
	}
	cuerpo.WriteByte(byte(len(miPublica)))
	cuerpo.Write(miPublica)
	cuerpo.Write(cifrado)

	return cuerpo.Bytes(), nil
}

// --------------------------------------------------------------------------
// VAPID (RFC 8292)
// --------------------------------------------------------------------------

// autorizacion arma la cabecera con la que el servicio de push nos reconoce.
//
// El `aud` es el ORIGEN del endpoint, no el endpoint completo: el token vale
// para todos los suscriptores del mismo servicio, y asi no hay que firmar uno
// distinto por cada celular.
func (e *Enviador) autorizacion(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("el endpoint de push no es una URL valida: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"aud": u.Scheme + "://" + u.Host,
		"exp": time.Now().Add(vigenciaVAPID).Unix(),
		"sub": e.contacto,
	})

	firmado, err := token.SignedString(e.privada)
	if err != nil {
		return "", fmt.Errorf("firmando el token VAPID: %w", err)
	}
	return "vapid t=" + firmado + ", k=" + codificar(e.publica), nil
}

// clavePrivadaECDSA reconstruye la llave para firmar a partir de los 32 bytes
// crudos que guarda el .env.
//
// El rodeo por ecdh es para no usar elliptic.ScalarBaseMult, que esta
// deprecada: se deja que el paquete moderno calcule el punto publico y de ahi
// se sacan X e Y, que es lo unico que ecdsa necesita ademas del escalar.
func clavePrivadaECDSA(cruda []byte) (*ecdsa.PrivateKey, error) {
	k, err := ecdh.P256().NewPrivateKey(cruda)
	if err != nil {
		return nil, fmt.Errorf("PUSH_VAPID_PRIVATE no es una llave P-256 valida: %w", err)
	}

	punto := k.PublicKey().Bytes() // 65 bytes: 0x04 | X (32) | Y (32)
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(punto[1:33]),
			Y:     new(big.Int).SetBytes(punto[33:65]),
		},
		D: new(big.Int).SetBytes(cruda),
	}, nil
}

// --------------------------------------------------------------------------

// base64url SIN relleno: es lo que usan tanto VAPID como las credenciales que
// entrega el navegador.
func codificar(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// decodificar acepta las dos variantes (con y sin "=" al final) porque en la
// practica llegan de las dos formas segun el navegador.
func decodificar(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// GenerarLlaves crea un par VAPID nuevo, ya codificado para el .env.
// Lo usa cmd/vapid.
func GenerarLlaves() (publica, privada string, err error) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generando el par de llaves: %w", err)
	}
	return codificar(k.PublicKey().Bytes()), codificar(k.Bytes()), nil
}
