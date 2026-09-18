package avisos

import (
	"context"
	"fmt"
)

// CuotaVencida es una cuota de un acuerdo de pago que llego a su fecha sin que
// los abonos alcancen a cubrirla.
type CuotaVencida struct {
	CuotaID      int64
	MovimientoID int64
	// Tipo dice quien tiene que pagar: 'preste' (te pagan) o 'me_prestaron'
	// (pagas tu).
	Tipo      string
	AQuien    string
	Numero    int
	DeCuantas int
	Monto     string
	VenceEl   string
	// Falta es lo que hay que poner para dejar esa cuota cubierta. Puede ser
	// menos que el monto de la cuota si ya se abono una parte.
	Falta string
}

// CuotasVencidas son las cuotas cuyo dia ya llego (entre `desde` y `hoy`) y que
// los abonos todavia no cubren.
//
// COMO SE SABE QUE UNA CUOTA ESTA CUBIERTA: los abonos cubren las cuotas EN
// ORDEN. Se compara el ACUMULADO de cuotas hasta la numero N contra el total
// abonado de esa deuda. Si el acumulado es mayor, esa cuota no esta cubierta y
// falta la diferencia.
//
// Es exactamente la misma regla que usa la pantalla del acuerdo
// (movimientos.ListarCuotas). Tiene que serlo: si el aviso contara de otra
// forma, diria que falta una cuota que la app muestra como pagada.
func (s *Store) CuotasVencidas(ctx context.Context, usuarioID int64, desde, hoy string) ([]CuotaVencida, error) {
	const q = `
		WITH abonado AS (
			SELECT movimiento_id, sum(monto) AS total
			FROM abonos
			WHERE usuario_id = $1
			GROUP BY movimiento_id
		), acumulado AS (
			SELECT q.id, q.movimiento_id, q.numero, q.vence_el, q.monto,
			       sum(q.monto) OVER (PARTITION BY q.movimiento_id ORDER BY q.numero) AS hasta_aqui,
			       count(*)     OVER (PARTITION BY q.movimiento_id) AS de_cuantas,
			       coalesce(ab.total, 0) AS abonado
			FROM cuotas q
			LEFT JOIN abonado ab ON ab.movimiento_id = q.movimiento_id
			WHERE q.usuario_id = $1
		)
		SELECT a.id, a.movimiento_id, m.tipo, m.a_quien, a.numero, a.de_cuantas,
		       a.monto::text, to_char(a.vence_el, 'YYYY-MM-DD'),
		       least(a.monto, a.hasta_aqui - a.abonado)::numeric(14,2)::text
		FROM acumulado a
		JOIN movimientos m ON m.id = a.movimiento_id
		WHERE a.hasta_aqui > a.abonado
		  AND a.vence_el BETWEEN $2::date AND $3::date
		ORDER BY a.vence_el, a.movimiento_id, a.numero`

	filas, err := s.db.QueryContext(ctx, q, usuarioID, desde, hoy)
	if err != nil {
		return nil, fmt.Errorf("cuotas vencidas: %w", err)
	}
	defer filas.Close()

	var lista []CuotaVencida
	for filas.Next() {
		var c CuotaVencida
		if err := filas.Scan(&c.CuotaID, &c.MovimientoID, &c.Tipo, &c.AQuien, &c.Numero,
			&c.DeCuantas, &c.Monto, &c.VenceEl, &c.Falta); err != nil {
			return nil, fmt.Errorf("leyendo cuota vencida: %w", err)
		}
		lista = append(lista, c)
	}
	return lista, filas.Err()
}
