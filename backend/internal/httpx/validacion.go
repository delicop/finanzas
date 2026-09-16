package httpx

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Validador acumula errores de validacion campo por campo.
//
// Decision: validacion a mano en vez de una libreria con tags (validator/v10).
// Para una API de este tamano las reglas son pocas, los mensajes quedan en
// espanol y en el formato exacto que pinta el frontend, y no hay una capa de
// "magia por reflexion" que tengas que aprender mientras aprendes Go.
type Validador struct {
	Campos map[string]string
}

func NuevoValidador() *Validador {
	return &Validador{Campos: map[string]string{}}
}

func (v *Validador) Valido() bool { return len(v.Campos) == 0 }

// Check registra el error solo si aun no hay uno para ese campo,
// asi el usuario ve el primer problema (el mas relevante) de cada campo.
func (v *Validador) Check(ok bool, campo, mensaje string) {
	if !ok {
		if _, existe := v.Campos[campo]; !existe {
			v.Campos[campo] = mensaje
		}
	}
}

func (v *Validador) Requerido(campo, valor string) {
	v.Check(strings.TrimSpace(valor) != "", campo, "Este campo es obligatorio")
}

func (v *Validador) MaxLargo(campo, valor string, max int) {
	v.Check(utf8.RuneCountInString(valor) <= max, campo,
		fmt.Sprintf("No puede superar %d caracteres", max))
}

func (v *Validador) MinLargo(campo, valor string, min int) {
	v.Check(utf8.RuneCountInString(valor) >= min, campo,
		fmt.Sprintf("Debe tener al menos %d caracteres", min))
}

func (v *Validador) Email(campo, valor string) {
	_, err := mail.ParseAddress(valor)
	v.Check(err == nil, campo, "El correo no tiene un formato válido")
}
