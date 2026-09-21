package movimientos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Los pagos parciales de una deuda, y el acuerdo de cuotas en que se quedo.
//
// LA IDEA DE FONDO, que explica todo lo demas de este archivo:
//
//	lo que se debe = monto - suma de abonos
//
// El campo `estado` NO es la verdad, es un resumen para poder filtrar e
// indexar rapido. La verdad son los abonos, y `estado` se recalcula a partir
// de ellos DENTRO de la misma transaccion, cada vez que uno entra o sale.
// Asi no hay forma de que digan cosas distintas.
//
// Las cuotas son otra cosa: son el CALENDARIO, no la plata. Dicen para cuando
// se habia quedado de pagar cada pedazo. Sirven para avisar; no suman ni restan.

// Abono es cada plata que entra (si te estaban debiendo) o sale (si tu debes)
// a cuenta de una deuda.
type Abono struct {
	ID           int64  `json:"id"`
	MovimientoID int64  `json:"movimiento_id"`
	Monto        string `json:"monto"`
	Fecha        string `json:"fecha"`

	MedioID     *int64  `json:"medio_id"`
	MedioNombre *string `json:"medio_nombre"`

	Nota     string    `json:"nota"`
	CreadoEn time.Time `json:"creado_en"`
}

// AbonoDatos es lo que manda el cliente para registrar un abono.
type AbonoDatos struct {
	Monto   string
	Fecha   string
	MedioID *int64
	Nota    string
}

// Cuota es un pedazo del acuerdo de pago: cuanto y para cuando.
type Cuota struct {
	ID      int64  `json:"id"`
	Numero  int    `json:"numero"`
	VenceEl string `json:"vence_el"`
	Monto   string `json:"monto"`

	// Cubierta dice si los abonos hechos hasta hoy ya alcanzan para esta
	// cuota. Se calcula comparando el acumulado de cuotas contra el total
	// abonado: los abonos cubren las cuotas EN ORDEN, que es como se entiende
	// un acuerdo ("ya voy por la tercera").
	Cubierta bool `json:"cubierta"`
}

// CuotaDatos es una cuota tal como la propone el cliente.
type CuotaDatos struct {
	VenceEl string
	Monto   string
}

var (
	ErrNoEsDeuda      = errors.New("solo las deudas tienen abonos")
	ErrAbonoNoExiste  = errors.New("ese abono no existe")
	ErrAcuerdoDeMas   = errors.New("las cuotas suman mas que la deuda")
	ErrCuotaAntes     = errors.New("una cuota no puede vencer antes de la deuda")
	ErrMaximoDeCuotas = errors.New("demasiadas cuotas")
)

// MaxCuotas es el techo de un acuerdo. 120 = diez anios de cuotas mensuales:
// mas que eso no es un acuerdo de pago, es un dedazo en el formulario.
const MaxCuotas = 120

// --------------------------------------------------------------------------
// Abonos
// --------------------------------------------------------------------------

// Abonar registra un pago parcial y devuelve la deuda ya actualizada.
//
// Va en una transaccion con la fila bloqueada (FOR UPDATE) por una razon muy
// concreta: entre "leer cuanto falta" y "escribir el abono" no puede colarse
// otro abono. Sin el bloqueo, dos abonos simultaneos de 60.000 sobre un saldo
// de 100.000 pasarian los dos y la deuda quedaria pagada de mas.
func (s *Store) Abonar(ctx context.Context, usuarioID, movimientoID int64, d AbonoDatos) (*Movimiento, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion del abono: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // tras un Commit exitoso no hace nada

	// Pagar una deuda tuya saca plata de un medio, y borrar lo que te
	// devolvieron le quita: ninguno puede dejarlo en rojo (fondos.go).
	revisar, err := guardia(ctx, tx, usuarioID)
	if err != nil {
		return nil, err
	}

	// Bloquea la deuda y trae lo que falta. El saldo lo calcula Postgres
	// restando la suma de abonos, nunca Go.
	const saldoSQL = `
		SELECT m.tipo,
		       (m.monto - coalesce((SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id), 0))::text
		FROM movimientos m
		WHERE m.id = $1 AND m.usuario_id = $2
		FOR UPDATE`

	var tipo, saldo string
	err = tx.QueryRowContext(ctx, saldoSQL, movimientoID, usuarioID).Scan(&tipo, &saldo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando el saldo: %w", err)
	}
	if !EsDeuda(tipo) {
		return nil, ErrNoEsDeuda
	}

	// La comparacion tambien la hace Postgres: son dos NUMERIC, y compararlos
	// en Go obligaria a convertirlos a float, que es justo lo que esta app no
	// hace con el dinero en ningun lado.
	var sobra, saldado bool
	const compara = `SELECT $1::numeric > $2::numeric, $2::numeric <= 0`
	if err := tx.QueryRowContext(ctx, compara, d.Monto, saldo).Scan(&sobra, &saldado); err != nil {
		return nil, fmt.Errorf("comparando el abono con el saldo: %w", err)
	}
	if saldado {
		return nil, ErrSinSaldo
	}
	if sobra {
		return nil, ErrAbonoDeMas
	}

	const insertar = `
		WITH medio AS (
			SELECT id FROM medios_pago WHERE id = $4 AND usuario_id = $1
		)
		INSERT INTO abonos (usuario_id, movimiento_id, monto, fecha, medio_id, nota)
		SELECT $1, $2, $3::numeric, $5::date, (SELECT id FROM medio), $6
		-- Mismo criterio que en movimientos: si viene un medio, TIENE que ser
		-- del usuario. Sin este WHERE, un id ajeno se guardaria como NULL en
		-- silencio y el usuario creeria que quedo registrado por donde dijo.
		WHERE $4::bigint IS NULL OR EXISTS (SELECT 1 FROM medio)
		RETURNING id`

	var abonoID int64
	err = tx.QueryRowContext(ctx, insertar, usuarioID, movimientoID, d.Monto, d.MedioID, d.Fecha, d.Nota).Scan(&abonoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMedioInvalido
	}
	if err != nil {
		return nil, fmt.Errorf("guardando el abono: %w", err)
	}

	if err := recalcularEstado(ctx, tx, movimientoID); err != nil {
		return nil, err
	}
	if err := revisar(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando el abono: %w", err)
	}

	return s.PorID(ctx, usuarioID, movimientoID)
}

// BorrarAbono deshace un abono mal registrado y recalcula el estado.
func (s *Store) BorrarAbono(ctx context.Context, usuarioID, abonoID int64) (*Movimiento, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Pagar una deuda tuya saca plata de un medio, y borrar lo que te
	// devolvieron le quita: ninguno puede dejarlo en rojo (fondos.go).
	revisar, err := guardia(ctx, tx, usuarioID)
	if err != nil {
		return nil, err
	}

	const borrar = `DELETE FROM abonos WHERE id = $1 AND usuario_id = $2 RETURNING movimiento_id`

	var movimientoID int64
	err = tx.QueryRowContext(ctx, borrar, abonoID, usuarioID).Scan(&movimientoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAbonoNoExiste
	}
	if err != nil {
		return nil, fmt.Errorf("borrando el abono: %w", err)
	}

	if err := recalcularEstado(ctx, tx, movimientoID); err != nil {
		return nil, err
	}
	if err := revisar(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando el borrado del abono: %w", err)
	}

	return s.PorID(ctx, usuarioID, movimientoID)
}

// BorrarAbonos quita TODOS los abonos de una deuda y la deja en pendiente.
//
// Es lo que hace "volver a pendiente" desde la lista. Se llama con lo que el
// usuario acaba de confirmar en pantalla: es destructivo y no se deshace.
func (s *Store) BorrarAbonos(ctx context.Context, usuarioID, movimientoID int64) (*Movimiento, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Pagar una deuda tuya saca plata de un medio, y borrar lo que te
	// devolvieron le quita: ninguno puede dejarlo en rojo (fondos.go).
	revisar, err := guardia(ctx, tx, usuarioID)
	if err != nil {
		return nil, err
	}

	const tipoSQL = `SELECT tipo FROM movimientos WHERE id = $1 AND usuario_id = $2 FOR UPDATE`

	var tipo string
	err = tx.QueryRowContext(ctx, tipoSQL, movimientoID, usuarioID).Scan(&tipo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando la deuda: %w", err)
	}
	if !EsDeuda(tipo) {
		return nil, ErrNoEsPrestamo
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM abonos WHERE movimiento_id = $1`, movimientoID); err != nil {
		return nil, fmt.Errorf("borrando los abonos: %w", err)
	}
	if err := recalcularEstado(ctx, tx, movimientoID); err != nil {
		return nil, err
	}
	if err := revisar(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando el borrado de los abonos: %w", err)
	}

	return s.PorID(ctx, usuarioID, movimientoID)
}

// ListarAbonos devuelve los abonos de una deuda, del mas viejo al mas nuevo.
func (s *Store) ListarAbonos(ctx context.Context, usuarioID, movimientoID int64) ([]Abono, error) {
	const q = `
		SELECT a.id, a.movimiento_id, a.monto::text, to_char(a.fecha, 'YYYY-MM-DD'),
		       a.medio_id, mp.nombre, a.nota, a.creado_en
		FROM abonos a
		LEFT JOIN medios_pago mp ON mp.id = a.medio_id
		WHERE a.movimiento_id = $1 AND a.usuario_id = $2
		ORDER BY a.fecha, a.id`

	filas, err := s.db.QueryContext(ctx, q, movimientoID, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando abonos: %w", err)
	}
	defer filas.Close()

	lista := []Abono{}
	for filas.Next() {
		var (
			a      Abono
			medio  sql.NullInt64
			nombre sql.NullString
		)
		if err := filas.Scan(&a.ID, &a.MovimientoID, &a.Monto, &a.Fecha, &medio, &nombre, &a.Nota, &a.CreadoEn); err != nil {
			return nil, fmt.Errorf("leyendo abono: %w", err)
		}
		if medio.Valid {
			a.MedioID = &medio.Int64
			if nombre.Valid {
				a.MedioNombre = &nombre.String
			}
		}
		lista = append(lista, a)
	}
	return lista, filas.Err()
}

// recalcularEstado deja el campo `estado` de acuerdo con los abonos que hay.
//
// Es la unica forma en que ese campo cambia. No hay ningun sitio donde se
// escriba "pagado" a mano: marcar como pagado es registrar un abono por lo que
// falta, y volver a pendiente es borrar los abonos. Con una sola puerta, el
// estado no puede contradecir a la tabla.
func recalcularEstado(ctx context.Context, tx *sql.Tx, movimientoID int64) error {
	const q = `
		UPDATE movimientos m
		SET estado = CASE
		        WHEN ab.abonado >= m.monto THEN 'pagado'
		        WHEN ab.abonado > 0        THEN 'parcial'
		        ELSE 'pendiente'
		    END,
		    actualizado_en = now()
		FROM (
			SELECT coalesce(sum(monto), 0) AS abonado FROM abonos WHERE movimiento_id = $1
		) ab
		WHERE m.id = $1 AND m.tipo IN ('preste', 'me_prestaron')`

	if _, err := tx.ExecContext(ctx, q, movimientoID); err != nil {
		return fmt.Errorf("recalculando el estado de la deuda: %w", err)
	}
	return nil
}

// --------------------------------------------------------------------------
// El acuerdo de pago (las cuotas)
// --------------------------------------------------------------------------

// GuardarAcuerdo reemplaza las cuotas de una deuda por las que llegan.
//
// Reemplaza y no agrega: un acuerdo se renegocia entero ("mejor en cuatro
// cuotas"), y dejar mezcladas las viejas con las nuevas seria cobrar dos veces
// el mismo pedazo. Una lista vacia borra el acuerdo.
func (s *Store) GuardarAcuerdo(ctx context.Context, usuarioID, movimientoID int64, cuotas []CuotaDatos) ([]Cuota, error) {
	if len(cuotas) > MaxCuotas {
		return nil, ErrMaximoDeCuotas
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion del acuerdo: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	const revisar = `
		SELECT tipo, to_char(fecha, 'YYYY-MM-DD')
		FROM movimientos WHERE id = $1 AND usuario_id = $2 FOR UPDATE`

	var tipo, fecha string
	err = tx.QueryRowContext(ctx, revisar, movimientoID, usuarioID).Scan(&tipo, &fecha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando la deuda: %w", err)
	}
	if !EsDeuda(tipo) {
		return nil, ErrNoEsDeuda
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM cuotas WHERE movimiento_id = $1`, movimientoID); err != nil {
		return nil, fmt.Errorf("borrando el acuerdo anterior: %w", err)
	}

	const insertar = `
		INSERT INTO cuotas (usuario_id, movimiento_id, numero, vence_el, monto)
		VALUES ($1, $2, $3, $4::date, $5::numeric)`

	for i, c := range cuotas {
		// Las fechas AAAA-MM-DD se comparan bien como texto.
		if c.VenceEl < fecha {
			return nil, ErrCuotaAntes
		}
		if _, err := tx.ExecContext(ctx, insertar, usuarioID, movimientoID, i+1, c.VenceEl, c.Monto); err != nil {
			return nil, fmt.Errorf("guardando la cuota %d: %w", i+1, err)
		}
	}

	// Que las cuotas no sumen MAS que la deuda lo revisa Postgres, no Go:
	// sumar NUMERIC en Go obligaria a pasar por float. Sumar menos si se
	// permite a proposito: un acuerdo puede cubrir solo una parte ("pagame
	// 300 en tres cuotas y el resto cuando puedas").
	if len(cuotas) > 0 {
		const cuadra = `
			SELECT coalesce(sum(q.monto), 0) > m.monto
			FROM movimientos m
			LEFT JOIN cuotas q ON q.movimiento_id = m.id
			WHERE m.id = $1
			GROUP BY m.monto`

		var sobra bool
		if err := tx.QueryRowContext(ctx, cuadra, movimientoID).Scan(&sobra); err != nil {
			return nil, fmt.Errorf("verificando el acuerdo: %w", err)
		}
		if sobra {
			return nil, ErrAcuerdoDeMas
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando el acuerdo: %w", err)
	}

	return s.ListarCuotas(ctx, usuarioID, movimientoID)
}

// ListarCuotas devuelve el acuerdo con cada cuota marcada como cubierta o no.
//
// "Cubierta" lo decide Postgres comparando el acumulado de cuotas contra el
// total abonado: los abonos cubren las cuotas en orden. Es lo que permite
// decir "vas por la tercera" sin que el usuario tenga que casar cada abono
// con su cuota a mano — nadie lleva esa contabilidad en la cabeza.
func (s *Store) ListarCuotas(ctx context.Context, usuarioID, movimientoID int64) ([]Cuota, error) {
	const q = `
		WITH abonado AS (
			SELECT coalesce(sum(monto), 0) AS total
			FROM abonos WHERE movimiento_id = $1
		)
		SELECT q.id, q.numero, to_char(q.vence_el, 'YYYY-MM-DD'), q.monto::text,
		       sum(q.monto) OVER (ORDER BY q.numero) <= (SELECT total FROM abonado)
		FROM cuotas q
		WHERE q.movimiento_id = $1 AND q.usuario_id = $2
		ORDER BY q.numero`

	filas, err := s.db.QueryContext(ctx, q, movimientoID, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando cuotas: %w", err)
	}
	defer filas.Close()

	lista := []Cuota{}
	for filas.Next() {
		var c Cuota
		if err := filas.Scan(&c.ID, &c.Numero, &c.VenceEl, &c.Monto, &c.Cubierta); err != nil {
			return nil, fmt.Errorf("leyendo cuota: %w", err)
		}
		lista = append(lista, c)
	}
	return lista, filas.Err()
}

// --------------------------------------------------------------------------
// Saldar de una vez
// --------------------------------------------------------------------------

// Saldar da por pagada la deuda registrando un abono por lo que falta.
//
// Es lo que hace el boton "Ya me pagó" de la lista. Por dentro NO escribe
// 'pagado' en ningun lado: crea el abono que faltaba y deja que
// recalcularEstado saque la conclusion. Un solo camino, una sola verdad.
func (s *Store) Saldar(ctx context.Context, usuarioID, movimientoID int64, medioID *int64, fecha string) (*Movimiento, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("abriendo transaccion: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Pagar una deuda tuya saca plata de un medio, y borrar lo que te
	// devolvieron le quita: ninguno puede dejarlo en rojo (fondos.go).
	revisar, err := guardia(ctx, tx, usuarioID)
	if err != nil {
		return nil, err
	}

	const saldoSQL = `
		SELECT m.tipo,
		       (m.monto - coalesce((SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id), 0))::text,
		       (m.monto - coalesce((SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id), 0)) <= 0
		FROM movimientos m
		WHERE m.id = $1 AND m.usuario_id = $2
		FOR UPDATE`

	var (
		tipo    string
		saldo   string
		saldada bool
	)
	err = tx.QueryRowContext(ctx, saldoSQL, movimientoID, usuarioID).Scan(&tipo, &saldo, &saldada)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando el saldo: %w", err)
	}
	if !EsDeuda(tipo) {
		return nil, ErrNoEsPrestamo
	}
	// Ya estaba saldada: no es un error, es un segundo clic. Se devuelve tal
	// como esta en vez de crear un abono de cero (que el CHECK rechazaria).
	if saldada {
		return s.PorID(ctx, usuarioID, movimientoID)
	}

	const insertar = `
		WITH medio AS (
			SELECT id FROM medios_pago WHERE id = $4 AND usuario_id = $1
		)
		INSERT INTO abonos (usuario_id, movimiento_id, monto, fecha, medio_id, nota)
		SELECT $1, $2, $3::numeric, $5::date, (SELECT id FROM medio), ''
		WHERE $4::bigint IS NULL OR EXISTS (SELECT 1 FROM medio)
		RETURNING id`

	var abonoID int64
	err = tx.QueryRowContext(ctx, insertar, usuarioID, movimientoID, saldo, medioID, fecha).Scan(&abonoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMedioInvalido
	}
	if err != nil {
		return nil, fmt.Errorf("saldando la deuda: %w", err)
	}

	if err := recalcularEstado(ctx, tx, movimientoID); err != nil {
		return nil, err
	}
	if err := revisar(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando: %w", err)
	}

	return s.PorID(ctx, usuarioID, movimientoID)
}

// CambiarEstado es la puerta que usan el boton de la lista y el asistente.
//
// Traduce los dos estados que un usuario pide a mano a lo que de verdad pasa
// con los abonos:
//
//	pagado    -> abono por lo que falta
//	pendiente -> se borran todos los abonos
//
// 'parcial' no se puede pedir: sale solo de abonar una parte. Pedirlo seria
// decir "quedate a medias" sin decir de cuanto, que no significa nada.
func (s *Store) CambiarEstado(ctx context.Context, usuarioID, id int64, estado string, medioID *int64) (*Movimiento, error) {
	switch estado {
	case EstadoPagado:
		return s.Saldar(ctx, usuarioID, id, medioID, HoyEnColombia())
	case EstadoPendiente:
		return s.BorrarAbonos(ctx, usuarioID, id)
	default:
		return nil, ErrNoEsPrestamo
	}
}

// HoyEnColombia es la fecha de hoy para el usuario, no para el servidor. A las
// 8 de la noche en Bogota el servidor (que corre en UTC) ya esta en manana, y
// un abono registrado con la fecha de manana descuadra el resumen del dia.
func HoyEnColombia() string {
	return time.Now().In(zonaColombia).Format(FormatoFecha)
}
