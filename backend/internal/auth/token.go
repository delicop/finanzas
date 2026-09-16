package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenManager firma y valida los JWT.
//
// Que es un JWT: un texto con 3 partes (header.payload.firma). El payload NO
// esta cifrado, cualquiera puede leerlo con base64 -> por eso ahi solo va el id
// del usuario y la expiracion, nunca la contrasena ni datos sensibles.
// Lo que garantiza el JWT es INTEGRIDAD: si alguien cambia el payload, la firma
// deja de coincidir y lo rechazamos.
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenManager(secret []byte, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: secret, ttl: ttl}
}

var ErrTokenInvalido = errors.New("token invalido o expirado")

// Generar crea un token para el usuario y devuelve tambien cuando expira,
// para que el frontend sepa en que momento pedir login de nuevo.
func (tm *TokenManager) Generar(u *Usuario) (string, time.Time, error) {
	ahora := time.Now()
	expira := ahora.Add(tm.ttl)

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(u.ID, 10), // "sub" = de quien es el token
		IssuedAt:  jwt.NewNumericDate(ahora),
		ExpiresAt: jwt.NewNumericDate(expira),
		Issuer:    "finanzas-api",
	}

	// HS256: firma simetrica. La misma llave firma y verifica. Es lo correcto
	// aqui porque el unico que firma y el unico que verifica somos nosotros.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	firmado, err := token.SignedString(tm.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("firmando token: %w", err)
	}
	return firmado, expira, nil
}

// UsuarioIDDesdeToken valida la firma y la expiracion, y devuelve el id.
func (tm *TokenManager) UsuarioIDDesdeToken(tokenStr string) (int64, error) {
	token, err := jwt.ParseWithClaims(
		tokenStr,
		&jwt.RegisteredClaims{},
		func(t *jwt.Token) (any, error) { return tm.secret, nil },
		// CRITICO: fijar el algoritmo permitido.
		// Sin esto existe el ataque "alg confusion": un atacante manda un token
		// con alg=none o alg=HS256 firmado con una llave publica y la libreria
		// podria aceptarlo. Con WithValidMethods, cualquier otro alg se rechaza.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("finanzas-api"),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return 0, ErrTokenInvalido
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.Subject == "" {
		return 0, ErrTokenInvalido
	}

	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, ErrTokenInvalido
	}
	return id, nil
}
