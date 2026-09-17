// Package avisos son las notificaciones que la app le deja al usuario sin que
// las pida: su resumen de la semana, los prestamos que lleva meses sin cobrar
// y, al dueno del servidor, a quien le falta cobrarle este mes.
//
// La regla que ordena el paquete: LAS CIFRAS LAS CALCULA POSTGRES. El modelo
// de lenguaje, cuando esta configurado, solo redacta el parrafo con los
// numeros que ya vienen hechos — y si se inventa uno, su version se descarta
// (ver redactor.go). Los avisos funcionan igual sin modelo: el texto base
// siempre existe.
package avisos

import (
	"errors"
	"time"
)

// Los avisos que sabe generar. Los limita tambien un CHECK en la base.
const (
	TipoResumenSemanal      = "resumen_semanal"
	TipoPrestamosPendientes = "prestamos_pendientes"
	TipoCobrosDelMes        = "cobros_del_mes"
	// TipoCobroDelDia: hoy es el dia en que quedaron de devolver un prestamo.
	TipoCobroDelDia = "cobro_del_dia"
)

const (
	// DiasPrestamoViejo es a partir de cuando un prestamo pendiente merece un
	// recordatorio. Un mes: menos que eso es meterse en una plata que todavia
	// es reciente, y mas seria dejarla enfriar.
	DiasPrestamoViejo = 30

	// DiasGraciaCobro: si el servidor estuvo apagado justo el dia del cobro,
	// el aviso sale al volver, siempre que no hayan pasado mas de estos dias.
	// Despues ya no es "hoy te pagan" sino un olvido, y de eso se encarga el
	// recordatorio de prestamos pendientes.
	DiasGraciaCobro = 3

	// MaxNotificaciones es cuantas devuelve la app. Nadie baja mas alla.
	MaxNotificaciones = 30

	// RetencionDias es cuanto se guardan. Un aviso viejo no le sirve a nadie
	// y la tabla crece sola.
	RetencionDias = 90
)

// Aviso es una notificacion tal como la pinta la app.
type Aviso struct {
	ID       int64      `json:"id"`
	Tipo     string     `json:"tipo"`
	Titulo   string     `json:"titulo"`
	Cuerpo   string     `json:"cuerpo"`
	LeidaEn  *time.Time `json:"leida_en"`
	CreadaEn time.Time  `json:"creada_en"`
}

// Nuevo es un aviso por guardar. La clave es lo que impide repetirlo.
type Nuevo struct {
	UsuarioID int64
	Tipo      string
	Clave     string
	Titulo    string
	Cuerpo    string
}

// ErrYaExiste: ya habia un aviso de ese tipo para ese periodo. No es un fallo:
// es la tarea corriendo otra vez, que es justo lo que se espera que pase.
var ErrYaExiste = errors.New("ese aviso ya existía")
