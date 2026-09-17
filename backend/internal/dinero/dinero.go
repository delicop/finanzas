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

	// Quitamos los ceros a la izquierda ("0100" -> "100") dejando siempre al
	// menos un digito antes del punto ("0.50" sigue siendo "0.50").
	// Postgres los descartaria igual al convertir a NUMERIC, pero devolver
	// el valor ya limpio evita que dos montos iguales viajen escritos
	// distinto por la API.
	entero, decimales, tieneDecimales := strings.Cut(s, ".")
	entero = strings.TrimLeft(entero, "0")
	if entero == "" {
		entero = "0"
	}
	if tieneDecimales {
		return entero + "." + decimales, nil
	}
	return entero, nil
}

// Formatear escribe un monto como se lee en Colombia: "$ 1.234.567" o
// "$ 1.234,50". Recibe el texto que devuelve Postgres ("1234567.00") y lo
// arma a mano, caracter por caracter: tampoco aqui se pasa por float.
// Los centavos en cero no se muestran. Si el texto no es un numero, se
// devuelve tal cual.
func Formatear(monto string) string {
	s := strings.TrimSpace(monto)
	signo := ""
	if strings.HasPrefix(s, "-") {
		signo, s = "-", s[1:]
	}

	entero, decimales, _ := strings.Cut(s, ".")
	if entero == "" || strings.Trim(entero, "0123456789") != "" || strings.Trim(decimales, "0123456789") != "" {
		return monto
	}
	entero = strings.TrimLeft(entero, "0")
	if entero == "" {
		entero = "0"
	}

	var b strings.Builder
	for i, d := range entero {
		if i > 0 && (len(entero)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(d)
	}

	if strings.Trim(decimales, "0") != "" {
		if len(decimales) == 1 {
			decimales += "0"
		}
		b.WriteString("," + decimales[:2])
	}
	if b.String() == "0" {
		signo = ""
	}
	return signo + "$ " + b.String()
}
