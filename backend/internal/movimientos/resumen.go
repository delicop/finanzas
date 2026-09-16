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
// COMO CUENTA UN PRESTAMO
// ------------------------------------------------------------------
// Prestar y que te devuelvan son DOS movimientos de plata que se anulan:
// salieron $200.000 y volvieron $200.000. Neto: cero.
//
//	pendiente -> la plata esta afuera. Cuenta en "por cobrar" y RESTA del balance.
//	pagado    -> la plata volvio. NO cuenta en "por cobrar" y queda en CERO
//	             dentro del balance (salio y regreso).
//
// Por eso al marcar un prestamo como pagado el balance SUBE por ese monto:
// es plata que volvio a tu bolsillo. Lo que NO hacemos es sumarlo ademas a
// "recibido", porque entonces los mismos $200.000 se contarian dos veces y
// el balance subiria $400.000.
//
//	balance = recibido - pagado - por_cobrar
//
// "Recuperado" (los prestamos ya devueltos) se reporta aparte, solo como
// informacion: no entra en la formula del balance.

type ResumenCategoria struct {
	CategoriaID int64  `json:"categoria_id"`
	Nombre      string `json:"nombre"`
	Recibido    string `json:"recibido"`
	Pagado      string `json:"pagado"`
	// PorCobrar son los prestamos PENDIENTES de esta categoria.
	PorCobrar string `json:"por_cobrar"`
	// Recuperado son los prestamos que ya devolvieron (informativo).
	Recuperado string `json:"recuperado"`
	// Balance = recibido - pagado - por_cobrar
	Balance     string `json:"balance"`
	Movimientos int    `json:"movimientos"`
}

// ResumenMedio responde "¿dónde está la plata?": cuánto hay en cada medio
// de pago (efectivo, la cuenta del banco, Nequi...).
//
// Aquí NO aparece "por cobrar" a propósito: un préstamo pendiente no está en
// ningún medio, está con la persona que se lo llevó. Lo que sí pasa es que
// la plata SALIÓ del medio con el que se prestó, y cuando la devuelven
// ENTRA por el medio con el que pagaron (que puede ser otro distinto).
//
//	Recibido = ingresos por ese medio + préstamos devueltos por ese medio
//	Pagado   = gastos por ese medio   + préstamos entregados por ese medio
//	Tengo    = Recibido - Pagado
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

type Deudor struct {
	AQuien    string `json:"a_quien"`
	Total     string `json:"total"`
	Prestamos int    `json:"prestamos"`
}

type Totales struct {
	Recibido   string `json:"recibido"`
	Pagado     string `json:"pagado"`
	PorCobrar  string `json:"por_cobrar"`
	Recuperado string `json:"recuperado"`
	Balance    string `json:"balance"`
}

type Resumen struct {
	Categorias []ResumenCategoria `json:"categorias"`
	Medios     []ResumenMedio     `json:"medios"`
	Totales    Totales            `json:"totales"`

	// Lo que le deben al usuario: suma de los 'preste' en estado pendiente.
	// Es el mismo numero que Totales.PorCobrar, expuesto aparte porque es
	// la cifra protagonista del dashboard.
	PrestadoPendiente   string   `json:"prestado_pendiente"`
	PrestamosPendientes int      `json:"prestamos_pendientes"`
	PrestamosCobrados   int      `json:"prestamos_cobrados"`
	Deudores            []Deudor `json:"deudores"`
}

// Resumen arma el dashboard con cuatro consultas.
//
// Se podria hacer en una sola con CTEs, pero varias consultas simples se leen
// y se depuran mucho mejor, y con el volumen de una app personal la diferencia
// de rendimiento es irrelevante.
func (s *Store) Resumen(ctx context.Context, usuarioID int64) (*Resumen, error) {
	resumen := &Resumen{
		Categorias: []ResumenCategoria{},
		Medios:     []ResumenMedio{},
		Deudores:   []Deudor{},
	}

	// 1) Totales por categoria.
	//
	// sum(...) FILTER (WHERE ...) es la forma limpia de Postgres para sumar
	// solo algunas filas dentro de un GROUP BY.
	// LEFT JOIN + coalesce: una categoria sin movimientos aparece en ceros
	// en vez de desaparecer del dashboard.
	const porCategoria = `
		SELECT c.id,
		       c.nombre,
		       coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'recibi'), 0)::numeric(14,2)::text,
		       coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'pague'),  0)::numeric(14,2)::text,
		       coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'preste' AND m.estado = 'pendiente'), 0)::numeric(14,2)::text,
		       coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'preste' AND m.estado = 'pagado'),    0)::numeric(14,2)::text,
		       (coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'recibi'), 0)
		      - coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'pague'),  0)
		      - coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'preste' AND m.estado = 'pendiente'), 0)
		       )::numeric(14,2)::text,
		       count(m.id)
		FROM categorias c
		LEFT JOIN movimientos m ON m.categoria_id = c.id AND m.usuario_id = c.usuario_id
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
			&rc.PorCobrar, &rc.Recuperado, &rc.Balance, &rc.Movimientos); err != nil {
			return nil, fmt.Errorf("leyendo resumen de categoria: %w", err)
		}
		resumen.Categorias = append(resumen.Categorias, rc)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo resumen: %w", err)
	}

	// 2) Totales generales.
	const generales = `
		SELECT coalesce(sum(monto) FILTER (WHERE tipo = 'recibi'), 0)::numeric(14,2)::text,
		       coalesce(sum(monto) FILTER (WHERE tipo = 'pague'),  0)::numeric(14,2)::text,
		       coalesce(sum(monto) FILTER (WHERE tipo = 'preste' AND estado = 'pendiente'), 0)::numeric(14,2)::text,
		       coalesce(sum(monto) FILTER (WHERE tipo = 'preste' AND estado = 'pagado'),    0)::numeric(14,2)::text,
		       (coalesce(sum(monto) FILTER (WHERE tipo = 'recibi'), 0)
		      - coalesce(sum(monto) FILTER (WHERE tipo = 'pague'),  0)
		      - coalesce(sum(monto) FILTER (WHERE tipo = 'preste' AND estado = 'pendiente'), 0)
		       )::numeric(14,2)::text,
		       count(*) FILTER (WHERE tipo = 'preste' AND estado = 'pendiente'),
		       count(*) FILTER (WHERE tipo = 'preste' AND estado = 'pagado')
		FROM movimientos
		WHERE usuario_id = $1`

	err = s.db.QueryRowContext(ctx, generales, usuarioID).Scan(
		&resumen.Totales.Recibido,
		&resumen.Totales.Pagado,
		&resumen.Totales.PorCobrar,
		&resumen.Totales.Recuperado,
		&resumen.Totales.Balance,
		&resumen.PrestamosPendientes,
		&resumen.PrestamosCobrados,
	)
	if err != nil {
		return nil, fmt.Errorf("resumen general: %w", err)
	}

	resumen.PrestadoPendiente = resumen.Totales.PorCobrar

	// 3) Donde esta la plata: saldo por medio de pago.
	//
	// "flujos" descompone cada movimiento en entradas y salidas de un medio.
	// Un prestamo devuelto genera DOS filas: la salida por el medio con que se
	// presto y la entrada por el medio con que lo pagaron (que puede ser otro).
	// Esa es la unica forma de que "presto en efectivo, me pagaron por
	// transferencia" quede bien reflejado en los dos saldos.
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

			-- Todo prestamo sale del medio con el que se presto,
			-- este cobrado o no.
			SELECT medio_pago_id, 0::numeric, monto, id
			FROM movimientos WHERE usuario_id = $1 AND tipo = 'preste'

			UNION ALL

			-- Y si ya lo devolvieron, entra por el medio con el que pagaron.
			SELECT medio_cobro_id, monto, 0::numeric, id
			FROM movimientos
			WHERE usuario_id = $1 AND tipo = 'preste' AND estado = 'pagado'
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

	// 4) Quien debe cuanto.
	const deudores = `
		SELECT a_quien, sum(monto)::numeric(14,2)::text, count(*)
		FROM movimientos
		WHERE usuario_id = $1 AND tipo = 'preste' AND estado = 'pendiente'
		GROUP BY a_quien
		ORDER BY sum(monto) DESC, lower(a_quien)`

	filasDeudores, err := s.db.QueryContext(ctx, deudores, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("consultando deudores: %w", err)
	}
	defer filasDeudores.Close()

	for filasDeudores.Next() {
		var d Deudor
		if err := filasDeudores.Scan(&d.AQuien, &d.Total, &d.Prestamos); err != nil {
			return nil, fmt.Errorf("leyendo deudor: %w", err)
		}
		resumen.Deudores = append(resumen.Deudores, d)
	}
	if err := filasDeudores.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo deudores: %w", err)
	}

	return resumen, nil
}
