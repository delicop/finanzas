package avisos

import (
	"context"
	"database/sql"
	"fmt"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// --------------------------------------------------------------------------
// Las notificaciones
// --------------------------------------------------------------------------

// Guardar deja el aviso, salvo que ya existiera uno igual.
//
// El ON CONFLICT no es una optimizacion: es LA regla. La tarea que genera
// avisos corre cada pocas horas y vuelve a evaluarlo todo; lo que impide que
// el usuario reciba el mismo resumen cuatro veces al dia es el indice unico
// sobre (usuario_id, tipo, clave), no que la tarea lleve bien la cuenta.
func (s *Store) Guardar(ctx context.Context, nuevo Nuevo) (*Aviso, error) {
	const q = `
		INSERT INTO notificaciones (usuario_id, tipo, clave, titulo, cuerpo)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (usuario_id, tipo, clave) DO NOTHING
		RETURNING id, tipo, titulo, cuerpo, leida_en, creada_en`

	var a Aviso
	err := s.db.QueryRowContext(ctx, q, nuevo.UsuarioID, nuevo.Tipo, nuevo.Clave, nuevo.Titulo, nuevo.Cuerpo).
		Scan(&a.ID, &a.Tipo, &a.Titulo, &a.Cuerpo, &a.LeidaEn, &a.CreadaEn)

	// Sin filas devueltas = el ON CONFLICT lo descarto: ya existia.
	if err == sql.ErrNoRows {
		return nil, ErrYaExiste
	}
	if err != nil {
		return nil, fmt.Errorf("guardando aviso: %w", err)
	}
	return &a, nil
}

// Existe dice si ya hay un aviso con esa clave. Se pregunta ANTES de
// redactarlo: sin esto, cada corrida de la tarea le pediria al modelo un texto
// que despues el indice unico descarta, y eso se paga.
func (s *Store) Existe(ctx context.Context, usuarioID int64, tipo, clave string) (bool, error) {
	const q = `SELECT exists(SELECT 1 FROM notificaciones WHERE usuario_id = $1 AND tipo = $2 AND clave = $3)`

	var existe bool
	if err := s.db.QueryRowContext(ctx, q, usuarioID, tipo, clave).Scan(&existe); err != nil {
		return false, fmt.Errorf("buscando aviso: %w", err)
	}
	return existe, nil
}

// Listar devuelve los ultimos avisos del usuario y cuantos sin leer tiene.
func (s *Store) Listar(ctx context.Context, usuarioID int64) ([]Aviso, int, error) {
	const q = `
		SELECT id, tipo, titulo, cuerpo, leida_en, creada_en
		FROM notificaciones
		WHERE usuario_id = $1
		ORDER BY creada_en DESC, id DESC
		LIMIT $2`

	filas, err := s.db.QueryContext(ctx, q, usuarioID, MaxNotificaciones)
	if err != nil {
		return nil, 0, fmt.Errorf("listando avisos: %w", err)
	}
	defer filas.Close()

	lista := []Aviso{}
	for filas.Next() {
		var a Aviso
		if err := filas.Scan(&a.ID, &a.Tipo, &a.Titulo, &a.Cuerpo, &a.LeidaEn, &a.CreadaEn); err != nil {
			return nil, 0, fmt.Errorf("leyendo aviso: %w", err)
		}
		lista = append(lista, a)
	}
	if err := filas.Err(); err != nil {
		return nil, 0, fmt.Errorf("recorriendo avisos: %w", err)
	}

	// El conteo va sobre TODOS los sin leer, no solo sobre los de la pagina:
	// es lo que pinta la campana.
	const conteo = `SELECT count(*) FROM notificaciones WHERE usuario_id = $1 AND leida_en IS NULL`
	var sinLeer int
	if err := s.db.QueryRowContext(ctx, conteo, usuarioID).Scan(&sinLeer); err != nil {
		return nil, 0, fmt.Errorf("contando avisos sin leer: %w", err)
	}

	return lista, sinLeer, nil
}

// MarcarLeidos marca los sin leer del usuario. Se llama cuando abre el panel:
// los vio, no hay mas que saber.
func (s *Store) MarcarLeidos(ctx context.Context, usuarioID int64) error {
	const q = `
		UPDATE notificaciones
		SET leida_en = now()
		WHERE usuario_id = $1 AND leida_en IS NULL`

	if _, err := s.db.ExecContext(ctx, q, usuarioID); err != nil {
		return fmt.Errorf("marcando avisos como leidos: %w", err)
	}
	return nil
}

// LimpiarViejos borra los avisos que ya nadie va a leer.
func (s *Store) LimpiarViejos(ctx context.Context) (int64, error) {
	const q = `DELETE FROM notificaciones WHERE creada_en < now() - make_interval(days => $1)`

	resultado, err := s.db.ExecContext(ctx, q, RetencionDias)
	if err != nil {
		return 0, fmt.Errorf("limpiando avisos viejos: %w", err)
	}
	return resultado.RowsAffected()
}

// --------------------------------------------------------------------------
// Los datos con los que se arman: TODO sumado por Postgres
// --------------------------------------------------------------------------

// Destinatario es cada cuenta a la que hay que evaluarle los avisos.
type Destinatario struct {
	ID     int64
	Nombre string
	Rol    string
	// Si al redactar su aviso se puede usar el modelo. Lo tiene quien paga un
	// plan con IA, y el administrador, que es quien paga el modelo.
	ConIA bool
}

// Destinatarios son las cuentas activas. Las desactivadas no reciben nada:
// cortarles el acceso y seguir mandandoles avisos seria incoherente.
func (s *Store) Destinatarios(ctx context.Context) ([]Destinatario, error) {
	const q = `
		SELECT u.id, u.nombre, u.rol, (u.rol = 'admin' OR coalesce(p.incluye_ia, false))
		FROM usuarios u
		LEFT JOIN planes p ON p.id = u.plan_id
		WHERE u.activo
		ORDER BY u.id`

	filas, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listando destinatarios: %w", err)
	}
	defer filas.Close()

	lista := []Destinatario{}
	for filas.Next() {
		var d Destinatario
		if err := filas.Scan(&d.ID, &d.Nombre, &d.Rol, &d.ConIA); err != nil {
			return nil, fmt.Errorf("leyendo destinatario: %w", err)
		}
		lista = append(lista, d)
	}
	return lista, filas.Err()
}

// Semana son los numeros de un rango de fechas, ya sumados.
type Semana struct {
	Recibido    string
	Pagado      string
	Prestado    string
	MePrestaron string
	Movimientos int

	// En que se le fue mas la plata. Vacio si no gasto nada.
	CategoriaTop string
	MontoTop     string
}

// ResumenSemana suma los movimientos del usuario entre dos fechas (inclusive).
//
// Las sumas las hace Postgres sobre columnas NUMERIC y llegan como texto, como
// en toda la app: Go no suma un peso, y menos para un texto que despues va a
// leer un modelo de lenguaje.
func (s *Store) ResumenSemana(ctx context.Context, usuarioID int64, desde, hasta string) (*Semana, error) {
	// Los traslados quedan fuera de las cuatro sumas (no son ingreso ni gasto)
	// pero SI se cuentan como movimientos: pasar plata de un lado a otro es
	// algo que el usuario hizo esa semana.
	const q = `
		SELECT
			coalesce(sum(monto) FILTER (WHERE tipo = 'recibi'), 0)::text,
			coalesce(sum(monto) FILTER (WHERE tipo = 'pague'),  0)::text,
			coalesce(sum(monto) FILTER (WHERE tipo = 'preste'), 0)::text,
			coalesce(sum(monto) FILTER (WHERE tipo = 'me_prestaron'), 0)::text,
			count(*)
		FROM movimientos
		WHERE usuario_id = $1 AND fecha >= $2::date AND fecha <= $3::date`

	var semana Semana
	err := s.db.QueryRowContext(ctx, q, usuarioID, desde, hasta).
		Scan(&semana.Recibido, &semana.Pagado, &semana.Prestado, &semana.MePrestaron, &semana.Movimientos)
	if err != nil {
		return nil, fmt.Errorf("resumen de la semana: %w", err)
	}

	if semana.Movimientos == 0 {
		return &semana, nil
	}

	// La categoria en la que mas gasto. Es lo unico del resumen que no es una
	// cifra global, y es lo que lo hace util: "se te fue en Negocio 1".
	const top = `
		SELECT c.nombre, sum(m.monto)::text
		FROM movimientos m
		JOIN categorias c ON c.id = m.categoria_id
		WHERE m.usuario_id = $1 AND m.tipo = 'pague'
		  AND m.fecha >= $2::date AND m.fecha <= $3::date
		GROUP BY c.nombre
		ORDER BY sum(m.monto) DESC
		LIMIT 1`

	err = s.db.QueryRowContext(ctx, top, usuarioID, desde, hasta).
		Scan(&semana.CategoriaTop, &semana.MontoTop)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("categoria con mas gasto: %w", err)
	}

	return &semana, nil
}

// Prestamos es lo que lleva tiempo sin cobrar.
type Prestamos struct {
	Cantidad     int
	Total        string
	DiasMasViejo int
	AQuien       string
}

// DeudasViejas son las deudas con saldo que llevan mas de `dias` dias.
//
// `tipo` decide el sentido: 'preste' es lo que no te han devuelto,
// 'me_prestaron' es lo que tu no has pagado. La consulta es identica porque el
// problema es identico — solo cambia quien tiene que sacar la plata.
//
// La suma va sobre el SALDO y no sobre el monto: si de un prestamo de 500 mil
// ya te devolvieron 400, lo que llevas sin cobrar son 100, no 500. Con el monto
// el aviso exageraria mas cuanto mas te van pagando, que es justo al reves.
func (s *Store) DeudasViejas(ctx context.Context, usuarioID int64, tipo string, dias int) (*Prestamos, error) {
	const q = `
		WITH s AS (
			SELECT m.fecha, m.a_quien,
			       (m.monto - coalesce((
			           SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id
			       ), 0)) AS saldo
			FROM movimientos m
			WHERE m.usuario_id = $1
			  AND m.tipo = $2
			  AND m.fecha <= current_date - make_interval(days => $3)
		)
		SELECT
			count(*),
			coalesce(sum(saldo), 0)::numeric(14,2)::text,
			coalesce(max(current_date - fecha), 0),
			coalesce((array_agg(a_quien ORDER BY fecha))[1], '')
		FROM s
		WHERE saldo > 0`

	var p Prestamos
	err := s.db.QueryRowContext(ctx, q, usuarioID, tipo, dias).
		Scan(&p.Cantidad, &p.Total, &p.DiasMasViejo, &p.AQuien)
	if err != nil {
		return nil, fmt.Errorf("deudas viejas (%s): %w", tipo, err)
	}
	return &p, nil
}

// Cobro es una deuda con saldo cuya fecha acordada cayo en estos dias.
type Cobro struct {
	MovimientoID int64
	// Tipo dice de que lado esta la plata: 'preste' (te pagan a ti) o
	// 'me_prestaron' (pagas tu). Es lo unico que cambia el texto del aviso.
	Tipo        string
	AQuien      string
	Monto       string // el SALDO, no el monto original
	Fecha       string // cuando se hizo el prestamo, AAAA-MM-DD
	CobrarEl    string // cuando se quedo de pagar, AAAA-MM-DD
	Descripcion string
}

// CobrosDelDia son los prestamos pendientes con fecha de cobro entre
// `desde` y `hoy` (inclusive). `hoy` lo manda quien llama, en hora de
// Colombia: el current_date de Postgres es el del servidor, que corre en UTC,
// y a las 8 de la noche ya diria que es manana.
func (s *Store) CobrosDelDia(ctx context.Context, usuarioID int64, desde, hoy string) ([]Cobro, error) {
	const q = `
		WITH s AS (
			SELECT m.id, m.tipo, m.a_quien, m.fecha, m.cobrar_el, m.descripcion,
			       (m.monto - coalesce((
			           SELECT sum(a.monto) FROM abonos a WHERE a.movimiento_id = m.id
			       ), 0)) AS saldo
			FROM movimientos m
			WHERE m.usuario_id = $1
			  AND m.tipo IN ('preste', 'me_prestaron')
			  AND m.cobrar_el BETWEEN $2::date AND $3::date
		)
		SELECT id, tipo, a_quien, saldo::numeric(14,2)::text,
		       to_char(fecha, 'YYYY-MM-DD'), to_char(cobrar_el, 'YYYY-MM-DD'), descripcion
		FROM s
		WHERE saldo > 0
		ORDER BY cobrar_el, id`

	filas, err := s.db.QueryContext(ctx, q, usuarioID, desde, hoy)
	if err != nil {
		return nil, fmt.Errorf("cobros del dia: %w", err)
	}
	defer filas.Close()

	var lista []Cobro
	for filas.Next() {
		var c Cobro
		if err := filas.Scan(&c.MovimientoID, &c.Tipo, &c.AQuien, &c.Monto, &c.Fecha, &c.CobrarEl, &c.Descripcion); err != nil {
			return nil, fmt.Errorf("leyendo cobro: %w", err)
		}
		lista = append(lista, c)
	}
	return lista, filas.Err()
}
