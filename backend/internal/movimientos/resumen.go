package movimientos

import (
	"context"
	"database/sql"
	"fmt"
)

// Todas las sumas de este archivo las hace POSTGRES sobre columnas NUMERIC,
// y llegan a Go ya convertidas a texto (::text). Go no suma un solo peso:
// asi los totales son exactos hasta el ultimo centavo.
//
// ------------------------------------------------------------------
// COMO CUENTA UNA DEUDA
// ------------------------------------------------------------------
// Prestar y que te devuelvan son DOS movimientos de plata que se anulan:
// salieron $200.000 y volvieron $200.000. Neto: cero. Con pagos parciales la
// idea es la misma, solo que a pedazos, y por eso todo se calcula sobre el
// SALDO y no sobre un estado:
//
//	saldo = monto - suma de sus abonos
//
//	preste       -> el saldo es plata TUYA que esta afuera. RESTA del balance.
//	me_prestaron -> el saldo es plata AJENA que tienes. SUMA al balance,
//	                porque de hecho la tienes en el bolsillo (y la debes).
//
// Una deuda saldada tiene saldo 0 y por lo tanto no mueve nada, sin necesidad
// de mirar el campo `estado`. Un abono que entra baja el saldo y sube el
// balance en ese mismo monto: es plata que volvio.
//
//	balance = recibido - pagado - por_cobrar + por_pagar
//
// "Recuperado" (lo que ya te devolvieron) y "abonado" (lo que ya pagaste de lo
// tuyo) se reportan aparte, solo como informacion: no entran en la formula.
//
// LOS TRASLADOS NO APARECEN EN NINGUNA DE ESTAS CIFRAS. Pasar plata del
// efectivo a Nequi no es ingreso ni gasto: la misma plata cambia de bolsillo.
// Solo mueven los saldos por medio, y ahi se cancelan entre si.

type ResumenCategoria struct {
	CategoriaID int64  `json:"categoria_id"`
	Nombre      string `json:"nombre"`
	Recibido    string `json:"recibido"`
	Pagado      string `json:"pagado"`
	// PorCobrar es el saldo de lo que te deben en esta categoria.
	PorCobrar string `json:"por_cobrar"`
	// PorPagar es el saldo de lo que tu debes en esta categoria.
	PorPagar string `json:"por_pagar"`
	// Recuperado es lo que ya te devolvieron (informativo).
	Recuperado string `json:"recuperado"`
	// Abonado es lo que ya pagaste de tus deudas (informativo).
	Abonado string `json:"abonado"`
	// Balance = recibido - pagado - por_cobrar + por_pagar
	Balance     string `json:"balance"`
	Movimientos int    `json:"movimientos"`
}

// ResumenMedio responde "¿dónde está la plata?": cuánto hay en cada medio
// de pago (efectivo, la cuenta del banco, Nequi...).
//
// Aquí NO aparece "por cobrar" a propósito: un préstamo pendiente no está en
// ningún medio, está con la persona que se lo llevó. Lo que sí pasa es que la
// plata SALIÓ del medio con el que se prestó, y cada abono ENTRA por el medio
// con el que pagaron (que puede ser otro distinto, y distinto en cada abono).
//
//	Recibido = ingresos + lo que te prestaron + abonos que te hicieron + traslados que entraron
//	Pagado   = gastos   + lo que prestaste    + abonos que hiciste     + traslados que salieron
//	Saldo    = Recibido - Pagado
//
// Con eso las dos columnas explican el saldo, y la suma de todos los saldos
// da exactamente el balance general.
type ResumenMedio struct {
	// MedioID es nil en la fila de los movimientos sin medio registrado.
	MedioID     *int64 `json:"medio_id"`
	Nombre      string `json:"nombre"`
	Recibido    string `json:"recibido"`
	Pagado      string `json:"pagado"`
	Saldo       string `json:"saldo"`
	Movimientos int    `json:"movimientos"`
}

// Contraparte es cada persona o negocio con el que hay una deuda viva, en
// cualquiera de los dos sentidos.
//
// Se agrupan por el nombre normalizado (sin mayúsculas ni espacios de sobra),
// así que "Carlos", "carlos" y " Carlos " son el mismo. Lo que se muestra es
// como se escribió la última vez.
type Contraparte struct {
	Nombre string `json:"nombre"`
	// TeDeben y LeDebes son saldos, no montos originales.
	TeDeben string `json:"te_deben"`
	LeDebes string `json:"le_debes"`
	// Neto = TeDeben - LeDebes. Negativo significa que en conjunto le debes tú.
	Neto   string `json:"neto"`
	Deudas int    `json:"deudas"`

	// ProximaFecha es la fecha acordada más cercana de sus deudas vivas
	// (puede ser una que ya pasó). nil si ninguna tiene fecha.
	ProximaFecha *string `json:"proxima_fecha"`

	// EsCategoria avisa que ese nombre también es una categoría del usuario.
	// Sirve para no confundir "le presté a Negocio 2" (la contraparte) con la
	// categoría Negocio 2 — y el asistente lo dice en voz alta.
	EsCategoria bool `json:"es_categoria"`
}

type Totales struct {
	Recibido   string `json:"recibido"`
	Pagado     string `json:"pagado"`
	PorCobrar  string `json:"por_cobrar"`
	PorPagar   string `json:"por_pagar"`
	Recuperado string `json:"recuperado"`
	Abonado    string `json:"abonado"`
	Balance    string `json:"balance"`
}

type Resumen struct {
	Categorias []ResumenCategoria `json:"categorias"`
	Medios     []ResumenMedio     `json:"medios"`
	Totales    Totales            `json:"totales"`

	// Lo que le deben al usuario y lo que el usuario debe. Son los mismos
	// numeros que Totales.PorCobrar y Totales.PorPagar, expuestos aparte
	// porque son las cifras protagonistas del dashboard.
	PrestadoPendiente string `json:"prestado_pendiente"`
	DebidoPendiente   string `json:"debido_pendiente"`

	PrestamosPendientes int `json:"prestamos_pendientes"`
	PrestamosCobrados   int `json:"prestamos_cobrados"`
	DeudasPendientes    int `json:"deudas_pendientes"`
	DeudasPagadas       int `json:"deudas_pagadas"`

	Contrapartes []Contraparte `json:"contrapartes"`
}

// saldosCTE es la base de casi todas las consultas de este archivo: cada
// movimiento con lo que le falta.
//
// Va en una constante porque el dia que cambie la definicion de "lo que se
// debe" tiene que cambiar en un solo sitio. Si cada consulta llevara su propia
// resta, el dashboard y los avisos podrian terminar diciendo cifras distintas.
const saldosCTE = `
	SELECT m.id, m.categoria_id, m.tipo, m.monto, m.fecha, m.cobrar_el, m.a_quien,
	       (m.monto - coalesce((
	           SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id
	       ), 0)) AS saldo
	FROM movimientos m
	WHERE m.usuario_id = $1`

// Resumen arma el dashboard con cuatro consultas.
//
// Se podria hacer en una sola con CTEs, pero varias consultas simples se leen
// y se depuran mucho mejor, y con el volumen de una app personal la diferencia
// de rendimiento es irrelevante.
func (s *Store) Resumen(ctx context.Context, usuarioID int64) (*Resumen, error) {
	resumen := &Resumen{
		Categorias:   []ResumenCategoria{},
		Medios:       []ResumenMedio{},
		Contrapartes: []Contraparte{},
	}

	// 1) Totales por categoria.
	//
	// sum(...) FILTER (WHERE ...) es la forma limpia de Postgres para sumar
	// solo algunas filas dentro de un GROUP BY.
	// LEFT JOIN + coalesce: una categoria sin movimientos aparece en ceros
	// en vez de desaparecer del dashboard.
	const porCategoria = `
		WITH s AS (` + saldosCTE + `)
		SELECT c.id,
		       c.nombre,
		       coalesce(sum(s.monto) FILTER (WHERE s.tipo = 'recibi'), 0)::numeric(14,2)::text,
		       coalesce(sum(s.monto) FILTER (WHERE s.tipo = 'pague'),  0)::numeric(14,2)::text,
		       coalesce(sum(s.saldo) FILTER (WHERE s.tipo = 'preste'), 0)::numeric(14,2)::text,
		       coalesce(sum(s.saldo) FILTER (WHERE s.tipo = 'me_prestaron'), 0)::numeric(14,2)::text,
		       coalesce(sum(s.monto - s.saldo) FILTER (WHERE s.tipo = 'preste'), 0)::numeric(14,2)::text,
		       coalesce(sum(s.monto - s.saldo) FILTER (WHERE s.tipo = 'me_prestaron'), 0)::numeric(14,2)::text,
		       (coalesce(sum(s.monto) FILTER (WHERE s.tipo = 'recibi'), 0)
		      - coalesce(sum(s.monto) FILTER (WHERE s.tipo = 'pague'),  0)
		      - coalesce(sum(s.saldo) FILTER (WHERE s.tipo = 'preste'), 0)
		      + coalesce(sum(s.saldo) FILTER (WHERE s.tipo = 'me_prestaron'), 0)
		       )::numeric(14,2)::text,
		       count(s.id)
		FROM categorias c
		LEFT JOIN s ON s.categoria_id = c.id
		WHERE c.usuario_id = $1
		GROUP BY c.id, c.nombre
		ORDER BY lower(c.nombre)`

	filas, err := s.db.QueryContext(ctx, porCategoria, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("resumen por categoria: %w", err)
	}
	defer filas.Close()

	for filas.Next() {
		var rc ResumenCategoria
		if err := filas.Scan(&rc.CategoriaID, &rc.Nombre, &rc.Recibido, &rc.Pagado,
			&rc.PorCobrar, &rc.PorPagar, &rc.Recuperado, &rc.Abonado,
			&rc.Balance, &rc.Movimientos); err != nil {
			return nil, fmt.Errorf("leyendo resumen de categoria: %w", err)
		}
		resumen.Categorias = append(resumen.Categorias, rc)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo resumen: %w", err)
	}

	// 2) Totales generales.
	//
	// Los conteos van sobre el SALDO y no sobre el campo `estado`: una deuda
	// con saldo 0 esta saldada, se llame como se llame. Asi el conteo no puede
	// contradecir a la cifra que aparece al lado.
	const generales = `
		WITH s AS (` + saldosCTE + `)
		SELECT coalesce(sum(monto) FILTER (WHERE tipo = 'recibi'), 0)::numeric(14,2)::text,
		       coalesce(sum(monto) FILTER (WHERE tipo = 'pague'),  0)::numeric(14,2)::text,
		       coalesce(sum(saldo) FILTER (WHERE tipo = 'preste'), 0)::numeric(14,2)::text,
		       coalesce(sum(saldo) FILTER (WHERE tipo = 'me_prestaron'), 0)::numeric(14,2)::text,
		       coalesce(sum(monto - saldo) FILTER (WHERE tipo = 'preste'), 0)::numeric(14,2)::text,
		       coalesce(sum(monto - saldo) FILTER (WHERE tipo = 'me_prestaron'), 0)::numeric(14,2)::text,
		       (coalesce(sum(monto) FILTER (WHERE tipo = 'recibi'), 0)
		      - coalesce(sum(monto) FILTER (WHERE tipo = 'pague'),  0)
		      - coalesce(sum(saldo) FILTER (WHERE tipo = 'preste'), 0)
		      + coalesce(sum(saldo) FILTER (WHERE tipo = 'me_prestaron'), 0)
		       )::numeric(14,2)::text,
		       count(*) FILTER (WHERE tipo = 'preste'       AND saldo > 0),
		       count(*) FILTER (WHERE tipo = 'preste'       AND saldo <= 0),
		       count(*) FILTER (WHERE tipo = 'me_prestaron' AND saldo > 0),
		       count(*) FILTER (WHERE tipo = 'me_prestaron' AND saldo <= 0)
		FROM s`

	err = s.db.QueryRowContext(ctx, generales, usuarioID).Scan(
		&resumen.Totales.Recibido,
		&resumen.Totales.Pagado,
		&resumen.Totales.PorCobrar,
		&resumen.Totales.PorPagar,
		&resumen.Totales.Recuperado,
		&resumen.Totales.Abonado,
		&resumen.Totales.Balance,
		&resumen.PrestamosPendientes,
		&resumen.PrestamosCobrados,
		&resumen.DeudasPendientes,
		&resumen.DeudasPagadas,
	)
	if err != nil {
		return nil, fmt.Errorf("resumen general: %w", err)
	}

	resumen.PrestadoPendiente = resumen.Totales.PorCobrar
	resumen.DebidoPendiente = resumen.Totales.PorPagar

	// 3) Donde esta la plata: saldo por medio de pago.
	//
	// "flujos" descompone cada movimiento en entradas y salidas de un medio.
	// Un traslado genera DOS filas (sale de uno, entra al otro) y por eso no
	// cambia el total. Cada abono genera la suya, con SU medio: asi "presto en
	// efectivo, me pagaron la mitad por transferencia" queda bien en los dos
	// saldos, que es justo lo que una sola columna no podia representar.
	const porMedio = `
		WITH flujos AS (
			-- Ingresos
			SELECT medio_pago_id AS medio_id, monto AS entro, 0::numeric AS salio, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'recibi'

			UNION ALL

			-- Gastos
			SELECT medio_pago_id, 0::numeric, monto, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'pague'

			UNION ALL

			-- Prestar saca la plata del medio con el que se presto, la
			-- devuelvan despues o no.
			SELECT medio_pago_id, 0::numeric, monto, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'preste'

			UNION ALL

			-- Que te presten la mete por el medio con el que te la dieron.
			SELECT medio_pago_id, monto, 0::numeric, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'me_prestaron'

			UNION ALL

			-- Un traslado sale del origen...
			SELECT medio_pago_id, 0::numeric, monto, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'traslado'

			UNION ALL

			-- ...y entra al destino, por el mismo monto. Se cancelan.
			SELECT medio_cobro_id, monto, 0::numeric, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'traslado'

			UNION ALL

			-- Cada abono de un prestamo ENTRA por su propio medio.
			SELECT a.medio_id, a.monto, 0::numeric, m.id
			FROM abonos a
			JOIN movimientos m ON m.id = a.movimiento_id
			WHERE a.usuario_id = $1 AND m.tipo = 'preste'

			UNION ALL

			-- Y cada abono de una deuda propia SALE por el suyo.
			SELECT a.medio_id, 0::numeric, a.monto, m.id
			FROM abonos a
			JOIN movimientos m ON m.id = a.movimiento_id
			WHERE a.usuario_id = $1 AND m.tipo = 'me_prestaron'
		)
		SELECT * FROM (
			SELECT 0 AS orden,
			       mp.id,
			       mp.nombre,
			       coalesce(sum(f.entro), 0)::numeric(14,2)::text,
			       coalesce(sum(f.salio), 0)::numeric(14,2)::text,
			       (coalesce(sum(f.entro), 0) - coalesce(sum(f.salio), 0))::numeric(14,2)::text,
			       count(DISTINCT f.id) AS movimientos
			FROM medios_pago mp
			LEFT JOIN flujos f ON f.medio_id = mp.id
			WHERE mp.usuario_id = $1
			GROUP BY mp.id, mp.nombre

			UNION ALL

			SELECT 1 AS orden,
			       NULL::bigint,
			       'Sin registrar',
			       coalesce(sum(entro), 0)::numeric(14,2)::text,
			       coalesce(sum(salio), 0)::numeric(14,2)::text,
			       (coalesce(sum(entro), 0) - coalesce(sum(salio), 0))::numeric(14,2)::text,
			       count(DISTINCT id)
			FROM flujos
			WHERE medio_id IS NULL
			HAVING count(*) > 0
		) t
		ORDER BY orden, lower(nombre)`

	filasMedios, err := s.db.QueryContext(ctx, porMedio, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("resumen por medio de pago: %w", err)
	}
	defer filasMedios.Close()

	for filasMedios.Next() {
		var (
			orden   int
			rm      ResumenMedio
			medioID sql.NullInt64
		)
		if err := filasMedios.Scan(&orden, &medioID, &rm.Nombre, &rm.Recibido, &rm.Pagado,
			&rm.Saldo, &rm.Movimientos); err != nil {
			return nil, fmt.Errorf("leyendo resumen de medio: %w", err)
		}
		if medioID.Valid {
			rm.MedioID = &medioID.Int64
		}
		resumen.Medios = append(resumen.Medios, rm)
	}
	if err := filasMedios.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo medios: %w", err)
	}

	// 4) Con quien hay cuentas pendientes, en los dos sentidos.
	//
	// Se agrupa por el nombre NORMALIZADO y se muestra la ultima forma en que
	// se escribio: si un dia se anoto "carlos" y otro "Carlos", es una sola
	// persona y un solo saldo. Agrupar por el texto crudo partiria la deuda en
	// dos y ninguna de las dos mitades seria cierta.
	const contrapartes = `
		WITH s AS (
			SELECT lower(btrim(m.a_quien)) AS clave, m.a_quien, m.tipo, m.cobrar_el,
			       m.fecha, m.id,
			       (m.monto - coalesce((
			           SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id
			       ), 0)) AS saldo
			FROM movimientos m
			WHERE m.usuario_id = $1 AND m.tipo IN ('preste', 'me_prestaron')
		)
		SELECT (array_agg(a_quien ORDER BY fecha DESC, id DESC))[1],
		       coalesce(sum(saldo) FILTER (WHERE tipo = 'preste'), 0)::numeric(14,2)::text,
		       coalesce(sum(saldo) FILTER (WHERE tipo = 'me_prestaron'), 0)::numeric(14,2)::text,
		       (coalesce(sum(saldo) FILTER (WHERE tipo = 'preste'), 0)
		      - coalesce(sum(saldo) FILTER (WHERE tipo = 'me_prestaron'), 0)
		       )::numeric(14,2)::text,
		       count(*),
		       to_char(min(cobrar_el), 'YYYY-MM-DD'),
		       exists(
		           SELECT 1 FROM categorias c
		           WHERE c.usuario_id = $1 AND lower(btrim(c.nombre)) = clave
		       )
		FROM s
		WHERE saldo > 0
		GROUP BY clave
		ORDER BY sum(saldo) DESC, clave`

	filasContrapartes, err := s.db.QueryContext(ctx, contrapartes, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("consultando contrapartes: %w", err)
	}
	defer filasContrapartes.Close()

	for filasContrapartes.Next() {
		var c Contraparte
		if err := filasContrapartes.Scan(&c.Nombre, &c.TeDeben, &c.LeDebes, &c.Neto,
			&c.Deudas, &c.ProximaFecha, &c.EsCategoria); err != nil {
			return nil, fmt.Errorf("leyendo contraparte: %w", err)
		}
		resumen.Contrapartes = append(resumen.Contrapartes, c)
	}
	if err := filasContrapartes.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo contrapartes: %w", err)
	}

	return resumen, nil
}
