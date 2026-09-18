package recurrentes

import (
	"context"
	"errors"
	"time"

	"finanzas/internal/movimientos"
)

// Confirmador es el único camino por el que una ocurrencia se convierte en
// movimiento.
//
// Existe porque ahora son DOS los que confirman: el botón "Lo pagué" del
// resumen y la tarjeta del asistente cuando le dices "ya pagué el internet".
// Con la lógica escrita dos veces, el día que cambie una regla —el orden de la
// reserva, qué se valida, qué pasa si la escritura falla— cambiaría en un
// camino y no en el otro, y el que quede viejo es el que va a duplicar un
// gasto o dejar una ocurrencia resuelta sin movimiento detrás.
type Confirmador struct {
	store       *Store
	movimientos *movimientos.Store
}

func NuevoConfirmador(s *Store, m *movimientos.Store) *Confirmador {
	return &Confirmador{store: s, movimientos: m}
}

// Confirmar resuelve la ocurrencia y crea su movimiento.
//
// `ajustar` recibe la entrada YA LLENA con lo que dice la plantilla, para que
// quien llame le pise encima solo lo que la persona corrigió — el monto, casi
// siempre. Ese "encima" importa: un cuerpo vacío tiene que dejar la plantilla
// intacta, no borrarla campo por campo. Con nil se confirma tal cual, que es
// el caso del clic único.
//
// Corregir el monto NO toca la plantilla: el recibo de la luz vino en 118 mil
// este mes, no cambió de precio para siempre.
//
// Devuelve los errores por campo igual que el formulario (segundo valor), para
// que quien llame los pueda pintar donde el usuario está mirando.
func (c *Confirmador) Confirmar(ctx context.Context, usuarioID, ocurrenciaID int64,
	ajustar func(*movimientos.Entrada) error) (*movimientos.Movimiento, map[string]string, error) {

	o, err := c.store.Ocurrencia(ctx, usuarioID, ocurrenciaID)
	if err != nil {
		return nil, nil, err
	}
	if o.Estado != Pendiente {
		return nil, nil, ErrYaResuelta
	}

	entrada := movimientos.Entrada{
		CategoriaID: o.CategoriaID,
		MedioPagoID: o.MedioPagoID,
		Tipo:        o.Tipo,
		Monto:       o.Monto,
		Fecha:       o.Fecha,
		Descripcion: o.Descripcion,
	}
	if ajustar != nil {
		if err := ajustar(&entrada); err != nil {
			return nil, nil, err
		}
	}

	// Las MISMAS reglas del formulario. Si esto tuviera su propia validación,
	// el día que cambie una regla tendría también su propio bug.
	datos, campos := movimientos.Validar(entrada)
	if len(campos) > 0 {
		return nil, campos, nil
	}

	// Primero se reserva y después se escribe. Al revés, dos clics seguidos
	// (o el botón del resumen y la tarjeta del chat a la vez) crearían dos
	// arriendos: el UPDATE condicional de Resolver es lo único que puede
	// decidir quién gana esa carrera.
	if err := c.store.Resolver(ctx, usuarioID, ocurrenciaID, Confirmada, nil); err != nil {
		return nil, nil, err
	}

	m, err := c.movimientos.Crear(ctx, usuarioID, datos)
	if err != nil {
		// No se creó nada: el pendiente vuelve a estar disponible para que la
		// persona corrija y reintente.
		c.devolverAPendiente(ctx, usuarioID, ocurrenciaID)

		switch {
		case errors.Is(err, movimientos.ErrCategoriaInvalida):
			return nil, map[string]string{"categoria_id": "La categoría no existe"}, nil
		case errors.Is(err, movimientos.ErrMedioInvalido):
			return nil, map[string]string{"medio_pago_id": "El medio de pago no existe"}, nil
		}
		return nil, nil, err
	}

	// Rastro: de qué pendiente salió este movimiento. Si falla, el movimiento
	// ya existe y no se tumba la respuesta por una anotación.
	c.anotar(ctx, usuarioID, ocurrenciaID, m.ID)

	return m, nil, nil
}

// Los dos cierres van con un context propio: si quien pidió esto se fue (cerró
// la pestaña) su context ya está cancelado, y el "deshacer" fallaría también,
// dejando la ocurrencia resuelta sin que exista el movimiento. WithoutCancel
// conserva los valores pero no hereda la cancelación; el tope evita que una
// base colgada lo deje esperando para siempre.
func (c *Confirmador) devolverAPendiente(ctx context.Context, usuarioID, id int64) {
	cierre, cancelar := contextoDeCierre(ctx)
	defer cancelar()

	// Si esto falla, queda una ocurrencia resuelta sin movimiento: la persona
	// puede volver a dictarla, y el error sube por el log de quien llama.
	_ = c.store.DevolverAPendiente(cierre, usuarioID, id)
}

func (c *Confirmador) anotar(ctx context.Context, usuarioID, id, movimientoID int64) {
	cierre, cancelar := contextoDeCierre(ctx)
	defer cancelar()

	_ = c.store.AnotarMovimiento(cierre, usuarioID, id, movimientoID)
}

func contextoDeCierre(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
