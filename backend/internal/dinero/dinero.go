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
	"strconv"
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

// ErrReparto: se pidieron mas cuotas que centavos. Con $500 no se pueden hacer
// 600 cuotas sin que alguna quede en cero, y una cuota de cero no es una cuota.
var ErrReparto = errors.New("El monto no alcanza para tantas cuotas")

// Repartir divide un monto en n partes que suman EXACTAMENTE el total.
//
// Sobre la regla del paquete ("Go nunca hace aritmetica con plata"): aqui Go si
// calcula, pero con ENTEROS de centavos, no con float. Esa es la diferencia que
// importa. 100.000 / 3 en centavos es 3.333.333 y sobran 1: las tres cuotas
// salen 33.333,34 / 33.333,33 / 33.333,33 y suman 100.000 al peso. Con float
// saldrian tres veces 33333.333333... y el acuerdo no cuadraria con la deuda.
//
// Los centavos que sobran se reparten entre las PRIMERAS cuotas, no entre las
// ultimas: si alguien deja de pagar a la mitad, conviene que lo ya pagado sea
// el pedazo mas grande.
//
// No se hace en Postgres como el resto de las sumas porque esto no es sumar
// filas de una tabla: es partir un numero que ya tenemos, y en una consulta
// quedaria un generate_series bastante menos legible que estas diez lineas.
func Repartir(total string, n int) ([]string, error) {
	if n <= 0 {
		return nil, ErrReparto
	}

	normalizado, err := Normalizar(total)
	if err != nil {
		return nil, err
	}

	centavos, err := aCentavos(normalizado)
	if err != nil {
		return nil, err
	}
	if centavos < int64(n) {
		return nil, ErrReparto
	}

	base := centavos / int64(n)
	sobran := centavos % int64(n)

	partes := make([]string, 0, n)
	for i := range n {
		monto := base
		if int64(i) < sobran {
			monto++
		}
		partes = append(partes, deCentavos(monto))
	}
	return partes, nil
}

// aCentavos pasa "1500.50" a 150050. Sin float: mueve digitos.
//
// El monto ya viene por Normalizar, que garantiza el formato y el techo de 12
// enteros + 2 decimales — 14 digitos, que caben de sobra en un int64.
func aCentavos(normalizado string) (int64, error) {
	entero, decimales, _ := strings.Cut(normalizado, ".")

	// "1500.5" son 50 centavos, no 5: se rellena a la derecha.
	switch len(decimales) {
	case 0:
		decimales = "00"
	case 1:
		decimales += "0"
	}

	var total int64
	for _, c := range entero + decimales {
		total = total*10 + int64(c-'0')
	}
	return total, nil
}

// deCentavos hace el camino de vuelta: 150050 -> "1500.50".
func deCentavos(centavos int64) string {
	digitos := strconv.FormatInt(centavos, 10)
	for len(digitos) < 3 {
		digitos = "0" + digitos
	}
	return digitos[:len(digitos)-2] + "." + digitos[len(digitos)-2:]
}
