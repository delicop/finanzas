// Package dinero valida y normaliza montos.
//
// Decision central del proyecto: el dinero NUNCA es float64.
// En binario 0.1 no se puede representar exacto, asi que 0.1+0.2 != 0.3 y
// despues de unos cientos de movimientos los totales dejan de cuadrar.
//
// Aqui el monto viaja como STRING de punta a punta (JSON -> Go -> Postgres),
// se guarda en una columna NUMERIC(14,2) y TODAS las sumas las hace Postgres,
// que si maneja decimal exacto. Go nunca hace aritmetica con plata.
package dinero

import (
	"errors"
	"regexp"
	"strings"
)

// Hasta 12 enteros y maximo 2 decimales: cabe en NUMERIC(14,2).
// 12 digitos son 999 mil millones de pesos; suficiente.
var formatoValido = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)

var (
	ErrFormato = errors.New("El monto debe ser un número como 1500 o 1500.50")
	ErrCero    = errors.New("El monto debe ser mayor que cero")
)

// Normalizar limpia y valida el monto que llega del cliente.
//
// Acepta separadores de miles con coma ("1,500.50") porque es facil que se
// cuelen desde un input formateado, pero NO acepta la coma como decimal:
// seria ambiguo y prefiero rechazar antes que guardar un monto equivocado.
func Normalizar(entrada string) (string, error) {
	s := strings.TrimSpace(entrada)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", "")

	if s == "" {
		return "", ErrFormato
	}
	if !formatoValido.MatchString(s) {
		return "", ErrFormato
	}

	// "0", "0.0" y "0.00" son cero: los rechazamos sin convertir a float.
	soloDigitos := strings.ReplaceAll(strings.TrimLeft(s, "0"), ".", "")
	if strings.Trim(soloDigitos, "0") == "" {
		return "", ErrCero
	}

	return s, nil
}
