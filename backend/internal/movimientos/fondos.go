package movimientos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"finanzas/internal/dinero"
)

// ------------------------------------------------------------------
// NINGUN MEDIO QUEDA EN NEGATIVO
// ------------------------------------------------------------------
// Si en Nequi hay $400.000, no se puede pagar $600.000 por Nequi: esos
// $200.000 de diferencia salieron de ALGUN lado, y si no se anota de cual, el
// saldo del medio queda en rojo y el balance deja de parecerse a la plata que
// la persona tiene de verdad.
//
// Por eso un movimiento que SACA plata de un medio (pague, preste y el origen
// de un traslado) se rechaza si lo deja por debajo de cero, con un FaltaPlata
// que dice cuanto hay y cuanto falta. El cliente pregunta entonces de donde
// salio el resto y vuelve a mandar el movimiento con un Cubrir:
//
//	me_prestaron -> alguien te presto lo que faltaba (queda como deuda tuya)
//	recibi       -> fue un ingreso, en la categoria que se elija
//	traslado     -> lo pasaste de otro medio que si tenia
//
// El movimiento que cubre y el que gasta se guardan en la MISMA transaccion:
// o quedan los dos o ninguno. Y el monto que cubre lo calcula el servidor en
// ese momento, no el cliente, para que el medio quede exactamente en cero y
// no en lo que el navegador creia que habia hace un minuto.

// flujosSQL descompone cada movimiento en entradas y salidas de un medio. Es
// la base del "¿donde esta la plata?" del dashboard y de esta verificacion:
// si cada una tuviera su propia version, el dashboard podria decir que hay
// $400.000 y el aviso que hay $350.000.
//
// Un traslado genera DOS filas (sale de uno, entra al otro) y por eso no
// cambia el total. Cada abono genera la suya, con SU medio: asi "presto en
// efectivo, me pagaron la mitad por transferencia" queda bien en los dos
// saldos. `abono` distingue esas filas de las del movimiento en si.
const flujosSQL = `
	-- Ingresos
	SELECT medio_pago_id AS medio_id, monto AS entro, 0::numeric AS salio, id, false AS abono
	FROM movimientos WHERE usuario_id = $1 AND tipo = 'recibi'

	UNION ALL

	-- Gastos
	SELECT medio_pago_id, 0::numeric, monto, id, false
	FROM movimientos WHERE usuario_id = $1 AND tipo = 'pague'

	UNION ALL

	-- Prestar saca la plata del medio con el que se presto, la
	-- devuelvan despues o no.
	SELECT medio_pago_id, 0::numeric, monto, id, false
	FROM movimientos WHERE usuario_id = $1 AND tipo = 'preste'

	UNION ALL

	-- Que te presten la mete por el medio con el que te la dieron.
	SELECT medio_pago_id, monto, 0::numeric, id, false
	FROM movimientos WHERE usuario_id = $1 AND tipo = 'me_prestaron'

	UNION ALL

	-- Un traslado sale del origen...
	SELECT medio_pago_id, 0::numeric, monto, id, false
	FROM movimientos WHERE usuario_id = $1 AND tipo = 'traslado'

	UNION ALL

	-- ...y entra al destino, por el mismo monto. Se cancelan.
	SELECT medio_cobro_id, monto, 0::numeric, id, false
	FROM movimientos WHERE usuario_id = $1 AND tipo = 'traslado'

	UNION ALL

	-- Cada abono de un prestamo ENTRA por su propio medio.
	SELECT a.medio_id, a.monto, 0::numeric, m.id, true
	FROM abonos a
	JOIN movimientos m ON m.id = a.movimiento_id
	WHERE a.usuario_id = $1 AND m.tipo = 'preste'

	UNION ALL

	-- Y cada abono de una deuda propia SALE por el suyo.
	SELECT a.medio_id, 0::numeric, a.monto, m.id, true
	FROM abonos a
	JOIN movimientos m ON m.id = a.movimiento_id
	WHERE a.usuario_id = $1 AND m.tipo = 'me_prestaron'
`

// FaltaPlata es el error de "en ese medio no alcanza". Lleva las cifras para
// que el cliente pueda preguntar de donde sale el resto sin hacer cuentas.
type FaltaPlata struct {
	MedioID int64  `json:"medio_id"`
	Medio   string `json:"medio"`
	// Disponible es lo que hay en el medio SIN contar este movimiento (al
	// editar, sin su version anterior). Puede ser negativo si el medio ya
	// venia en rojo de antes.
	Disponible string `json:"disponible"`
	Monto      string `json:"monto"`
	// Falta es lo que hay que cubrir para que el medio quede en cero.
	Falta string `json:"falta"`

	// EnOtros es la plata que SI hay en los demas medios, del que mas tiene
	// al que menos. Sin esto, pagar por Nequi con Nequi en cero pedia cubrir
	// el gasto entero aunque hubiera plata de sobra en el efectivo, y la
	// persona terminaba debiendo lo que no debia.
	EnOtros    []Disponible `json:"en_otros"`
	OtrosTotal string       `json:"otros_total"`
}

// Disponible es un medio con saldo a favor.
type Disponible struct {
	MedioID int64  `json:"medio_id"`
	Medio   string `json:"medio"`
	Saldo   string `json:"saldo"`
}

func (f *FaltaPlata) Error() string {
	return fmt.Sprintf("en %s hay %s y el movimiento es de %s: faltan %s",
		f.Medio, dinero.Formatear(f.Disponible), dinero.Formatear(f.Monto), dinero.Formatear(f.Falta))
}

// Mensaje es la frase que ve el usuario (y el asistente).
func (f *FaltaPlata) Mensaje() string {
	m := fmt.Sprintf("En %s solo tienes %s y esto es de %s. Faltan %s: ¿de dónde salieron?",
		f.Medio, dinero.Formatear(f.Disponible), dinero.Formatear(f.Monto), dinero.Formatear(f.Falta))
	for i, o := range f.EnOtros {
		if i == 0 {
			m += " Tienes"
		} else {
			m += ","
		}
		m += fmt.Sprintf(" %s en %s", dinero.Formatear(o.Saldo), o.Medio)
		if i == len(f.EnOtros)-1 {
			m += "."
		}
	}
	return m
}

// Los tipos con los que se puede cubrir un faltante.
const (
	CubrirPrestamo = TipoMePrestaron
	CubrirIngreso  = TipoRecibi
	CubrirTraslado = TipoTraslado
)

// Cubrir dice de donde salio lo que faltaba. El monto no va aqui: lo pone el
// servidor (ver el comentario de arriba).
type Cubrir struct {
	// UsarOtros: primero se pasa a este medio lo que haya en los demas (un
	// traslado por cada uno), y solo lo que siga faltando se cubre con Tipo.
	// Si con eso alcanza, Tipo puede venir vacio.
	UsarOtros bool
	Tipo      string
	// CategoriaID: obligatoria en un ingreso ("entro por Ventas"). En los
	// otros dos es opcional y, si no viene, se usa la del movimiento.
	CategoriaID int64
	AQuien      string  // quien te presto (solo me_prestaron)
	CobrarEl    *string // cuando le pagas (solo me_prestaron, opcional)
	OrigenID    int64   // de que medio lo pasaste (solo traslado)
}

// sacaPlata dice si el movimiento resta de medio_pago_id.
func sacaPlata(tipo string) bool {
	return tipo == TipoPague || tipo == TipoPreste || tipo == TipoTraslado
}

// consultor es lo que tienen en comun *sql.DB y *sql.Tx.
type consultor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// verificarFondos devuelve un *FaltaPlata si el movimiento deja su medio en
// negativo, o nil si alcanza (o si el movimiento no saca plata).
//
// excluirID es el movimiento que se esta editando: su version vieja no cuenta
// (0 al crear). Sus abonos si cuentan, porque siguen ahi despues de editar.
//
// Al editar solo se rechaza si el cambio EMPEORA el medio. Si ya estaba en
// rojo por datos de antes de esta regla, corregir la descripcion de un gasto
// no tiene por que exigir cuadrar primero todo lo demas.
func verificarFondos(ctx context.Context, q consultor, usuarioID, excluirID int64, d Datos) error {
	if !sacaPlata(d.Tipo) || d.MedioPagoID == nil {
		return nil
	}

	const consulta = `
		WITH flujos AS (` + flujosSQL + `),
		t AS (
			SELECT coalesce(sum(entro - salio), 0) AS antes,
			       coalesce(sum(entro - salio) FILTER (WHERE NOT (id = $3 AND NOT abono)), 0) AS sin_este
			FROM flujos WHERE medio_id = $2
		)
		SELECT mp.nombre,
		       t.sin_este::numeric(14,2)::text,
		       greatest($4::numeric - t.sin_este, 0)::numeric(14,2)::text,
		       (t.sin_este - $4::numeric < 0 AND t.sin_este - $4::numeric < t.antes)
		FROM medios_pago mp, t
		WHERE mp.id = $2 AND mp.usuario_id = $1`

	f := FaltaPlata{MedioID: *d.MedioPagoID, Monto: d.Monto}
	var rechazar bool
	err := q.QueryRowContext(ctx, consulta, usuarioID, *d.MedioPagoID, excluirID, d.Monto).
		Scan(&f.Medio, &f.Disponible, &f.Falta, &rechazar)
	if errors.Is(err, sql.ErrNoRows) {
		// El medio no es del usuario: eso lo reporta el INSERT con su error.
		return nil
	}
	if err != nil {
		return fmt.Errorf("verificando fondos del medio: %w", err)
	}
	if !rechazar {
		return nil
	}
	if err := otrosMedios(ctx, q, usuarioID, &f); err != nil {
		return err
	}
	return &f
}

// otrosMedios llena EnOtros y OtrosTotal: los demas medios con saldo a favor.
func otrosMedios(ctx context.Context, q consultor, usuarioID int64, f *FaltaPlata) error {
	const consulta = `
		WITH s AS (` + saldosSQL + `)
		SELECT coalesce(json_agg(json_build_object(
		           'medio_id', mp.id, 'medio', mp.nombre, 'saldo', s.saldo::numeric(14,2)::text
		       ) ORDER BY s.saldo DESC, mp.id), '[]')::text,
		       coalesce(sum(s.saldo), 0)::numeric(14,2)::text
		FROM s JOIN medios_pago mp ON mp.id = s.medio_id
		WHERE s.medio_id <> $2 AND s.saldo > 0`

	var crudo string
	if err := q.QueryRowContext(ctx, consulta, usuarioID, f.MedioID).Scan(&crudo, &f.OtrosTotal); err != nil {
		return fmt.Errorf("buscando plata en los otros medios: %w", err)
	}
	f.EnOtros = []Disponible{}
	if err := json.Unmarshal([]byte(crudo), &f.EnOtros); err != nil {
		return fmt.Errorf("leyendo los otros medios: %w", err)
	}
	return nil
}

// datosCubrir arma el movimiento que cubre `falta` en el medio de `d`.
func datosCubrir(d Datos, c *Cubrir, falta string) Datos {
	categoria := c.CategoriaID
	if categoria <= 0 {
		categoria = d.CategoriaID
	}
	descripcion := "Para completar un pago"
	if d.Descripcion != "" {
		descripcion += ": " + d.Descripcion
	}
	if len([]rune(descripcion)) > 500 {
		descripcion = string([]rune(descripcion)[:500])
	}

	nuevo := Datos{
		CategoriaID: categoria,
		Tipo:        c.Tipo,
		Monto:       falta,
		Fecha:       d.Fecha,
		Descripcion: descripcion,
	}
	medio := *d.MedioPagoID

	switch c.Tipo {
	case CubrirPrestamo:
		aQuien, estado := c.AQuien, EstadoPendiente
		nuevo.AQuien = &aQuien
		nuevo.Estado = &estado
		nuevo.CobrarEl = c.CobrarEl
		nuevo.MedioPagoID = &medio
	case CubrirIngreso:
		nuevo.MedioPagoID = &medio
	case CubrirTraslado:
		origen := c.OrigenID
		nuevo.MedioPagoID = &origen
		nuevo.MedioCobroID = &medio
	}
	return nuevo
}

// cubrirSiFalta verifica los fondos y, si faltan y el cliente dijo de donde
// salieron, inserta el movimiento que los cubre. Corre dentro de la
// transaccion de Crear/Actualizar.
func (s *Store) cubrirSiFalta(ctx context.Context, tx *sql.Tx, usuarioID, excluirID int64, d Datos) error {
	err := verificarFondos(ctx, tx, usuarioID, excluirID, d)
	var falta *FaltaPlata
	if !errors.As(err, &falta) {
		return err
	}
	if d.Cubrir == nil {
		return falta
	}

	restante := falta.Falta
	if d.Cubrir.UsarOtros {
		for _, o := range falta.EnOtros {
			var usar string
			var queda bool
			err := tx.QueryRowContext(ctx,
				`SELECT least($1::numeric, $2::numeric)::numeric(14,2)::text,
				        ($1::numeric - least($1::numeric, $2::numeric))::numeric(14,2)::text,
				        $1::numeric > $2::numeric`,
				restante, o.Saldo).Scan(&usar, &restante, &queda)
			if err != nil {
				return fmt.Errorf("repartiendo el faltante: %w", err)
			}
			traslado := datosCubrir(d, &Cubrir{Tipo: CubrirTraslado, OrigenID: o.MedioID}, usar)
			if _, err := s.insertar(ctx, tx, usuarioID, traslado); err != nil {
				return traducirCheck(err, "pasando plata de otro medio")
			}
			if !queda {
				return nil
			}
		}
		if d.Cubrir.Tipo == "" {
			// Ni con todo lo de los otros medios alcanza, y no dijo de donde
			// sale el resto: se pregunta de nuevo, ya con las cifras nuevas.
			return verificarFondos(ctx, tx, usuarioID, excluirID, d)
		}
	}

	cubre := datosCubrir(d, d.Cubrir, restante)
	// Un traslado para cubrir tambien saca plata, esta vez del origen: si
	// ahi tampoco alcanza, el error habla de ESE medio y el usuario elige
	// otra cosa.
	if err := verificarFondos(ctx, tx, usuarioID, 0, cubre); err != nil {
		return err
	}
	if _, err := s.insertar(ctx, tx, usuarioID, cubre); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.porQueNoEntro(ctx, usuarioID, cubre)
		}
		return traducirCheck(err, "cubriendo el faltante")
	}
	return nil
}

// bloquearUsuario serializa las escrituras de plata de un mismo usuario hasta
// que termine la transaccion. Sin esto, dos gastos de $300.000 enviados a la
// vez sobre $400.000 verian los dos que alcanza, y los dos entrarian.
func bloquearUsuario(ctx context.Context, tx *sql.Tx, usuarioID int64) error {
	var nada string
	err := tx.QueryRowContext(ctx, `SELECT pg_advisory_xact_lock($1)::text`, usuarioID).Scan(&nada)
	if err != nil {
		return fmt.Errorf("bloqueando al usuario: %w", err)
	}
	return nil
}

// VerificarFondos es la misma verificacion, fuera de una transaccion y sin
// escribir nada. La usa el asistente para avisar del faltante al PROPONER el
// movimiento, antes de que el usuario llegue a confirmar la tarjeta.
func (s *Store) VerificarFondos(ctx context.Context, usuarioID int64, d Datos) error {
	return verificarFondos(ctx, s.db, usuarioID, 0, d)
}

// ------------------------------------------------------------------
// LA GUARDIA: nada deja un medio mas en rojo
// ------------------------------------------------------------------
// verificarFondos mira un movimiento que SACA plata antes de escribirlo. Pero
// un medio tambien se va a negativo por el otro lado: borrando el ingreso que
// lo sostenia, bajandole el monto, cambiandolo de medio, borrando el abono con
// el que te devolvieron un prestamo o pagando una deuda tuya.
//
// En vez de pensar caso por caso, cada escritura de plata toma una foto de los
// saldos al empezar (dentro de su transaccion y con el usuario bloqueado) y
// antes del COMMIT la compara con como quedaron. Si algun medio termina en
// negativo Y peor que antes, se deshace todo. "Peor que antes" es para no
// castigar a quien ya venia en rojo por datos viejos: corregir una fecha no le
// exige cuadrar primero.

// QuedaEnRojo es el error de la guardia: con ese cambio, el medio queda en
// negativo.
type QuedaEnRojo struct {
	MedioID int64  `json:"medio_id"`
	Medio   string `json:"medio"`
	// Saldo es como quedaria el medio (negativo).
	Saldo string `json:"saldo"`
	// Falta es lo que habria que registrar primero para que quede en cero.
	Falta string `json:"falta"`
}

func (q *QuedaEnRojo) Error() string {
	return fmt.Sprintf("%s quedaria en %s", q.Medio, dinero.Formatear(q.Saldo))
}

// Mensaje es la frase que ve el usuario (y el asistente).
func (q *QuedaEnRojo) Mensaje() string {
	return fmt.Sprintf("Así %s quedaría en %s, y ningún medio puede quedar en negativo. "+
		"Si esos %s salieron de otro lado, regístralo primero: un ingreso, un préstamo que te hicieron "+
		"o un traslado desde otro medio.",
		q.Medio, dinero.Formatear(q.Saldo), dinero.Formatear(q.Falta))
}

// saldosSQL es el saldo de cada medio del usuario ($1), ya sumado.
const saldosSQL = `
	SELECT medio_id, sum(entro - salio) AS saldo
	FROM (` + flujosSQL + `) f
	WHERE medio_id IS NOT NULL
	GROUP BY medio_id`

// fotoSaldos devuelve los saldos de todos los medios como un objeto JSON
// {"medio_id": "saldo"}. Va como texto de ida y vuelta a Postgres para que Go
// no tenga que comparar dinero (ni convertirlo a float).
func fotoSaldos(ctx context.Context, tx *sql.Tx, usuarioID int64) (string, error) {
	var foto string
	err := tx.QueryRowContext(ctx,
		`SELECT coalesce(json_object_agg(medio_id, saldo), '{}')::text FROM (`+saldosSQL+`) s`,
		usuarioID).Scan(&foto)
	if err != nil {
		return "", fmt.Errorf("tomando la foto de los saldos: %w", err)
	}
	return foto, nil
}

// comparar devuelve un *QuedaEnRojo si algun medio quedo en negativo y peor
// que en la foto. Si hay varios, el que quedo mas en rojo.
func comparar(ctx context.Context, tx *sql.Tx, usuarioID int64, foto string) error {
	const q = `
		WITH ahora AS (` + saldosSQL + `),
		antes AS (
			SELECT key::bigint AS medio_id, value::numeric AS saldo FROM json_each_text($2::json)
		)
		SELECT mp.id, mp.nombre, a.saldo::numeric(14,2)::text, (-a.saldo)::numeric(14,2)::text
		FROM ahora a
		JOIN medios_pago mp ON mp.id = a.medio_id
		LEFT JOIN antes b ON b.medio_id = a.medio_id
		WHERE a.saldo < 0 AND a.saldo < coalesce(b.saldo, 0)
		ORDER BY a.saldo
		LIMIT 1`

	var r QuedaEnRojo
	err := tx.QueryRowContext(ctx, q, usuarioID, foto).Scan(&r.MedioID, &r.Medio, &r.Saldo, &r.Falta)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("comparando los saldos: %w", err)
	}
	return &r
}

// guardia bloquea al usuario, toma la foto y devuelve la funcion que hay que
// llamar justo antes del COMMIT.
func guardia(ctx context.Context, tx *sql.Tx, usuarioID int64) (func() error, error) {
	if err := bloquearUsuario(ctx, tx, usuarioID); err != nil {
		return nil, err
	}
	foto, err := fotoSaldos(ctx, tx, usuarioID)
	if err != nil {
		return nil, err
	}
	return func() error { return comparar(ctx, tx, usuarioID, foto) }, nil
}

// cubrirPagoDeDeuda aplica a un pago de deuda propia (un abono que SALE del
// medio) la misma verificacion que a un gasto: si el medio no alcanza,
// FaltaPlata; si el usuario dijo de donde salio, se registra en la misma
// transaccion. Los abonos de un prestamo que te hacen a ti ENTRAN, asi que
// con esos no hace nada; tampoco sin medio, que no sale de ningun lado.
func (s *Store) cubrirPagoDeDeuda(ctx context.Context, tx *sql.Tx, usuarioID, movimientoID int64,
	tipo, monto, fecha string, medioID *int64, cubrir *Cubrir) error {
	if tipo != TipoMePrestaron || medioID == nil {
		return nil
	}

	var (
		categoria int64
		aQuien    sql.NullString
	)
	err := tx.QueryRowContext(ctx,
		`SELECT categoria_id, a_quien FROM movimientos WHERE id = $1 AND usuario_id = $2`,
		movimientoID, usuarioID).Scan(&categoria, &aQuien)
	if err != nil {
		return fmt.Errorf("leyendo la deuda: %w", err)
	}

	descripcion := "Pago de una deuda"
	if aQuien.String != "" {
		descripcion = "Pago a " + aQuien.String
	}
	// Se verifica como si fuera un gasto: es plata que sale del medio. Solo
	// se usa para la verificacion y para armar lo que cubre; el abono lo
	// inserta quien llama.
	return s.cubrirSiFalta(ctx, tx, usuarioID, 0, Datos{
		CategoriaID: categoria,
		Tipo:        TipoPague,
		Monto:       monto,
		Fecha:       fecha,
		Descripcion: descripcion,
		MedioPagoID: medioID,
		Cubrir:      cubrir,
	})
}

// VerificarPago dice si sacar `monto` de un medio lo deja en rojo, sin
// escribir nada. La usa el asistente para avisar al PROPONER el pago de una
// deuda propia, antes de que el usuario llegue a la tarjeta.
func (s *Store) VerificarPago(ctx context.Context, usuarioID, medioID int64, monto string) error {
	return verificarFondos(ctx, s.db, usuarioID, 0, Datos{Tipo: TipoPague, Monto: monto, MedioPagoID: &medioID})
}
