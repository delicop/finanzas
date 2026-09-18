package movimientos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Datos son los campos editables de un movimiento (lo que manda el cliente).
type Datos struct {
	CategoriaID int64
	Tipo        string
	Monto       string
	Fecha       string
	Descripcion string
	AQuien      *string // solo para las deudas (preste / me_prestaron)
	Estado      *string // solo para las deudas
	CobrarEl    *string // solo para las deudas, opcional
	MedioPagoID *int64  // de donde sale la plata; en un traslado, el ORIGEN

	// MedioCobroID es el DESTINO de un traslado, y solo eso. Para los demas
	// tipos va nil: por donde volvio un prestamo lo dice cada abono.
	MedioCobroID *int64
}

// Filtros del listado. Los campos vacios simplemente no filtran.
type Filtros struct {
	CategoriaID int64
	MedioPagoID int64
	Tipo        string
	Estado      string
	Desde       string
	Hasta       string
	Texto       string

	// AQuien filtra por persona/negocio sin distinguir mayusculas ni espacios
	// de sobra: es como se agrupan las contrapartes en el resumen, y tiene que
	// agrupar igual aqui o la lista no cuadraria con el total.
	AQuien string

	Limite int
	Offset int
}

// columnas se repite en varias consultas; mantenerlo en una constante evita
// que el SELECT y el Scan se desincronicen al agregar un campo.
//
// Las cuatro ultimas salen de los LATERAL de abajo: lo abonado, lo que falta,
// cuantos abonos hay y cuantas cuotas tiene el acuerdo. Las suma Postgres, no
// Go, igual que todo el dinero de esta app.
const columnas = `
	m.id, m.categoria_id, c.nombre, m.medio_pago_id, mp.nombre,
	m.medio_cobro_id, mc.nombre, m.tipo, m.monto::text, m.fecha,
	m.descripcion, m.a_quien, m.estado, m.cobrar_el,
	m.factura_ruta, m.factura_nombre, m.factura_tipo,
	m.creado_en, m.actualizado_en,
	coalesce(ab.abonado, 0)::numeric(14,2)::text,
	(m.monto - coalesce(ab.abonado, 0))::numeric(14,2)::text,
	coalesce(ab.cuantos, 0),
	coalesce(cu.cuantas, 0)`

// unionesDeuda trae lo abonado y las cuotas de cada fila.
//
// LATERAL y no dos subconsultas en el SELECT: con LATERAL la suma se calcula
// UNA vez por fila y sirve para las dos columnas (abonado y saldo). Con
// subconsultas habria que repetir el mismo sum() dos veces.
//
// LEFT: un movimiento sin abonos (o que ni siquiera es una deuda) tiene que
// salir igual, con 0, no desaparecer del listado.
const unionesDeuda = `
	LEFT JOIN LATERAL (
		SELECT sum(a.monto) AS abonado, count(*) AS cuantos
		FROM abonos a WHERE a.movimiento_id = m.id
	) ab ON true
	LEFT JOIN LATERAL (
		SELECT count(*) AS cuantas
		FROM cuotas q WHERE q.movimiento_id = m.id
	) cu ON true`

// uniones son los JOIN de las consultas de LECTURA. Crear y Actualizar no lo
// usan porque alli la categoria sale de una CTE, no de la tabla.
const uniones = `
	JOIN categorias c ON c.id = m.categoria_id
	LEFT JOIN medios_pago mp ON mp.id = m.medio_pago_id
	LEFT JOIN medios_pago mc ON mc.id = m.medio_cobro_id` + unionesDeuda

// unionesCTE son los mismos, pero con la categoria saliendo de la CTE `cat`.
const unionesCTE = `
	JOIN cat c ON c.id = m.categoria_id
	LEFT JOIN medios_pago mp ON mp.id = m.medio_pago_id
	LEFT JOIN medios_pago mc ON mc.id = m.medio_cobro_id` + unionesDeuda

// filtrosSQL arma el WHERE del listado y de la exportacion, que filtran igual.
//
// El WHERE se arma dinamicamente, pero los valores SIEMPRE van como parametros
// ($1, $2, ...). Concatenar valores dentro del string SQL seria inyeccion SQL.
func filtrosSQL(usuarioID int64, f Filtros) (string, []any) {
	condiciones := []string{"m.usuario_id = $1"}
	args := []any{usuarioID}

	agregar := func(expresion string, valor any) {
		args = append(args, valor)
		condiciones = append(condiciones, fmt.Sprintf(expresion, len(args)))
	}

	if f.CategoriaID > 0 {
		agregar("m.categoria_id = $%d", f.CategoriaID)
	}
	if f.MedioPagoID > 0 {
		// En un traslado el medio filtrado puede ser el origen O el destino:
		// al mirar "lo de Nequi" se esperan las dos puntas. El mismo parametro
		// va dos veces, por eso no usa el helper de arriba.
		args = append(args, f.MedioPagoID)
		n := strconv.Itoa(len(args))
		condiciones = append(condiciones,
			"(m.medio_pago_id = $"+n+" OR m.medio_cobro_id = $"+n+")")
	}
	if f.AQuien != "" {
		agregar("lower(btrim(m.a_quien)) = lower(btrim($%d))", f.AQuien)
	}
	if f.Tipo != "" {
		agregar("m.tipo = $%d", f.Tipo)
	}
	if f.Estado != "" {
		agregar("m.estado = $%d", f.Estado)
	}
	if f.Desde != "" {
		agregar("m.fecha >= $%d::date", f.Desde)
	}
	if f.Hasta != "" {
		agregar("m.fecha <= $%d::date", f.Hasta)
	}
	if f.Texto != "" {
		// ILIKE = LIKE sin distinguir mayusculas. Los % van en el VALOR, no en
		// el SQL, para que el texto del usuario siga siendo un parametro.
		// Este caso usa el mismo parametro dos veces, por eso no usa el helper.
		args = append(args, "%"+f.Texto+"%")
		n := strconv.Itoa(len(args))
		condiciones = append(condiciones,
			"(m.descripcion ILIKE $"+n+" OR m.a_quien ILIKE $"+n+")")
	}

	return "WHERE " + strings.Join(condiciones, " AND "), args
}

// Listar devuelve la pagina de movimientos y el total que cumple los filtros.
func (s *Store) Listar(ctx context.Context, usuarioID int64, f Filtros) ([]Movimiento, int, error) {
	where, args := filtrosSQL(usuarioID, f)

	var total int
	conteoSQL := `SELECT count(*) FROM movimientos m ` + where
	if err := s.db.QueryRowContext(ctx, conteoSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contando movimientos: %w", err)
	}

	if f.Limite <= 0 || f.Limite > 200 {
		f.Limite = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	args = append(args, f.Limite, f.Offset)
	listaSQL := fmt.Sprintf(`
		SELECT %s
		FROM movimientos m
		%s
		%s
		ORDER BY m.fecha DESC, m.id DESC
		LIMIT $%d OFFSET $%d`, columnas, uniones, where, len(args)-1, len(args))

	filas, err := s.db.QueryContext(ctx, listaSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listando movimientos: %w", err)
	}
	defer filas.Close()

	lista := []Movimiento{}
	for filas.Next() {
		m, err := escanear(filas)
		if err != nil {
			return nil, 0, err
		}
		lista = append(lista, *m)
	}
	if err := filas.Err(); err != nil {
		return nil, 0, fmt.Errorf("recorriendo movimientos: %w", err)
	}

	return lista, total, nil
}

func (s *Store) PorID(ctx context.Context, usuarioID, id int64) (*Movimiento, error) {
	q := fmt.Sprintf(`
		SELECT %s
		FROM movimientos m
		%s
		WHERE m.id = $1 AND m.usuario_id = $2`, columnas, uniones)

	m, err := escanear(s.db.QueryRowContext(ctx, q, id, usuarioID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Crear inserta el movimiento verificando en la MISMA consulta que la
// categoria exista y sea del usuario.
//
// El truco esta en el INSERT ... SELECT FROM cat: si la categoria no cumple
// la condicion, el SELECT no devuelve filas y el INSERT no inserta nada.
// Asi no hay dos consultas separadas con una ventana de tiempo entre ellas
// en la que la categoria podria borrarse (condicion de carrera).
func (s *Store) Crear(ctx context.Context, usuarioID int64, d Datos) (*Movimiento, error) {
	q := fmt.Sprintf(`
		WITH cat AS (
			SELECT id, nombre FROM categorias WHERE id = $2 AND usuario_id = $1
		), medio AS (
			SELECT id FROM medios_pago WHERE id = $9 AND usuario_id = $1
		), destino AS (
			SELECT id FROM medios_pago WHERE id = $11 AND usuario_id = $1
		), ins AS (
			INSERT INTO movimientos
				(usuario_id, categoria_id, tipo, monto, fecha, descripcion, a_quien, estado, medio_pago_id, cobrar_el, medio_cobro_id)
			SELECT $1, cat.id, $3, $4::numeric, $5::date, $6, $7, $8, (SELECT id FROM medio), $10::date, (SELECT id FROM destino)
			FROM cat
			-- El medio es opcional, pero si viene uno TIENE que ser del usuario.
			-- Sin este WHERE, un id ajeno o inexistente se guardaria como NULL
			-- en silencio y el usuario creeria que quedo registrado.
			-- Lo mismo con el destino del traslado.
			WHERE ($9::bigint  IS NULL OR EXISTS (SELECT 1 FROM medio))
			  AND ($11::bigint IS NULL OR EXISTS (SELECT 1 FROM destino))
			RETURNING *
		), abono AS (
			-- Una deuda que nace saldada ("le presté y ya me pagó") nace con
			-- su abono. Si no, quedaria con estado 'pagado' y saldo completo:
			-- dos datos que se contradicen. Sin medio, que es lo unico honesto
			-- cuando el formulario no lo pregunta en ese momento.
			INSERT INTO abonos (usuario_id, movimiento_id, monto, fecha, medio_id, nota)
			SELECT $1, ins.id, ins.monto, ins.fecha, NULL, 'Registrado como ya saldado'
			FROM ins WHERE ins.estado = 'pagado'
		)
		SELECT %s
		FROM ins m
		%s`, columnas, unionesCTE)

	m, err := escanear(s.db.QueryRowContext(ctx, q,
		usuarioID, d.CategoriaID, d.Tipo, d.Monto, d.Fecha, d.Descripcion, d.AQuien, d.Estado, d.MedioPagoID, d.CobrarEl, d.MedioCobroID))

	if errors.Is(err, sql.ErrNoRows) {
		return nil, s.porQueNoEntro(ctx, usuarioID, d)
	}
	if err != nil {
		return nil, traducirCheck(err, "creando movimiento")
	}
	return s.relerSiNacioSaldada(ctx, usuarioID, m, d)
}

// relerSiNacioSaldada vuelve a consultar cuando el INSERT creo tambien un
// abono en la misma sentencia.
//
// Las CTE comparten un unico snapshot: el SELECT final NO ve la fila que la
// CTE de al lado acaba de insertar, asi que el abonado y el saldo volverian en
// cero. Es una relectura, no un parche: la alternativa seria calcular a mano
// en Go unas cifras que Postgres ya sabe dar bien.
func (s *Store) relerSiNacioSaldada(ctx context.Context, usuarioID int64, m *Movimiento, d Datos) (*Movimiento, error) {
	if d.Estado == nil || *d.Estado != EstadoPagado || !EsDeuda(d.Tipo) {
		return m, nil
	}
	return s.PorID(ctx, usuarioID, m.ID)
}

// porQueNoEntro traduce un "no se insertó/actualizó nada" al error concreto.
// La consulta falla en silencio por diseño (un WHERE sin filas), así que aquí
// averiguamos cuál de las dos referencias era la mala para poder decirlo.
func (s *Store) porQueNoEntro(ctx context.Context, usuarioID int64, d Datos) error {
	var categoriaOK bool
	err := s.db.QueryRowContext(ctx,
		`SELECT exists(SELECT 1 FROM categorias WHERE id = $1 AND usuario_id = $2)`,
		d.CategoriaID, usuarioID).Scan(&categoriaOK)
	if err != nil {
		return fmt.Errorf("verificando categoria: %w", err)
	}
	if !categoriaOK {
		return ErrCategoriaInvalida
	}
	return ErrMedioInvalido
}

// Actualizar reemplaza todos los campos editables.
//
// Nota: a_quien y estado se escriben siempre, incluso con NULL. Si el usuario
// cambia un movimiento de 'preste' a 'pague', los dos campos quedan en NULL
// y el CHECK de la base de datos lo exige asi.
func (s *Store) Actualizar(ctx context.Context, usuarioID, id int64, d Datos) (*Movimiento, error) {
	q := fmt.Sprintf(`
		WITH cat AS (
			SELECT id, nombre FROM categorias WHERE id = $3 AND usuario_id = $1
		), medio AS (
			SELECT id FROM medios_pago WHERE id = $10 AND usuario_id = $1
		), destino AS (
			SELECT id FROM medios_pago WHERE id = $12 AND usuario_id = $1
		), ab AS (
			SELECT coalesce(sum(monto), 0) AS abonado FROM abonos WHERE movimiento_id = $2
		), upd AS (
			UPDATE movimientos m
			SET categoria_id   = cat.id,
			    tipo           = $4,
			    monto          = $5::numeric,
			    fecha          = $6::date,
			    descripcion    = $7,
			    a_quien        = $8,
			    -- El estado NO lo decide el formulario si ya hay abonos: lo
			    -- decide la plata registrada. Lo que el usuario elija solo
			    -- cuenta cuando no se ha abonado nada todavia, que es el
			    -- unico caso en que no hay nada que contradecir.
			    estado = CASE
			        WHEN $4 NOT IN ('preste', 'me_prestaron')  THEN NULL
			        WHEN (SELECT abonado FROM ab) >= $5::numeric THEN 'pagado'
			        WHEN (SELECT abonado FROM ab) > 0            THEN 'parcial'
			        ELSE $9
			    END,
			    medio_pago_id  = (SELECT id FROM medio),
			    cobrar_el      = $11::date,
			    medio_cobro_id = (SELECT id FROM destino),
			    actualizado_en = now()
			FROM cat
			WHERE m.id = $2 AND m.usuario_id = $1
			  AND ($10::bigint IS NULL OR EXISTS (SELECT 1 FROM medio))
			  AND ($12::bigint IS NULL OR EXISTS (SELECT 1 FROM destino))
			RETURNING m.*
		), abono AS (
			-- Mismo caso que al crear: si la marcan saldada y no tenia ningun
			-- abono, se crea el que falta para que el saldo cuadre.
			INSERT INTO abonos (usuario_id, movimiento_id, monto, fecha, medio_id, nota)
			SELECT $1, upd.id, upd.monto, upd.fecha, NULL, 'Marcado como saldado al editar'
			FROM upd
			WHERE upd.estado = 'pagado' AND (SELECT abonado FROM ab) = 0
		)
		SELECT %s
		FROM upd m
		%s`, columnas, unionesCTE)

	// La fila que devuelve esta consulta se descarta a proposito: mas abajo se
	// relee con PorID, porque el SELECT de aqui no alcanza a ver el abono que
	// la CTE de al lado pudo haber insertado.
	_, err := escanear(s.db.QueryRowContext(ctx, q,
		usuarioID, id, d.CategoriaID, d.Tipo, d.Monto, d.Fecha, d.Descripcion, d.AQuien, d.Estado, d.MedioPagoID, d.CobrarEl, d.MedioCobroID))

	if errors.Is(err, sql.ErrNoRows) {
		// Sin filas puede ser: el movimiento no existe, la categoria no sirve
		// o el medio de pago no sirve. Averiguamos cual para dar un mensaje util.
		existe, errExiste := s.existe(ctx, usuarioID, id)
		if errExiste != nil {
			return nil, errExiste
		}
		if !existe {
			return nil, ErrNoEncontrado
		}
		return nil, s.porQueNoEntro(ctx, usuarioID, d)
	}
	if err != nil {
		return nil, traducirCheck(err, "actualizando movimiento")
	}
	// Tras un UPDATE siempre se relee: el estado lo calculo la propia consulta
	// a partir de los abonos, y pudo quedar distinto del que venia en `d`.
	return s.PorID(ctx, usuarioID, id)
}

// Eliminar borra la fila y devuelve la ruta de la factura (si tenia) para que
// el handler borre tambien el archivo del disco.
func (s *Store) Eliminar(ctx context.Context, usuarioID, id int64) (string, error) {
	const q = `
		DELETE FROM movimientos
		WHERE id = $1 AND usuario_id = $2
		RETURNING coalesce(factura_ruta, '')`

	var ruta string
	err := s.db.QueryRowContext(ctx, q, id, usuarioID).Scan(&ruta)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("eliminando movimiento: %w", err)
	}
	return ruta, nil
}

// GuardarFactura asocia el archivo al movimiento y devuelve la ruta de la
// factura ANTERIOR (si la habia) para que el handler borre el archivo viejo.
func (s *Store) GuardarFactura(ctx context.Context, usuarioID, id int64, a *ArchivoGuardado) (string, error) {
	// Las CTE (el WITH) comparten el mismo snapshot: "previa" ve la fila TAL
	// COMO ESTABA antes del UPDATE, aunque ambas corran en la misma consulta.
	// Por eso podemos devolver la ruta vieja y escribir la nueva de una sola vez.
	const q = `
		WITH previa AS (
			SELECT id, coalesce(factura_ruta, '') AS ruta
			FROM movimientos
			WHERE id = $4 AND usuario_id = $5
		), upd AS (
			UPDATE movimientos m
			SET factura_ruta = $1, factura_nombre = $2, factura_tipo = $3, actualizado_en = now()
			FROM previa
			WHERE m.id = previa.id
			RETURNING m.id
		)
		SELECT previa.ruta FROM previa JOIN upd ON upd.id = previa.id`

	var anterior string
	err := s.db.QueryRowContext(ctx, q, a.Ruta, a.Nombre, a.Tipo, id, usuarioID).Scan(&anterior)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("guardando factura: %w", err)
	}
	if anterior == a.Ruta {
		return "", nil
	}
	return anterior, nil
}

// QuitarFactura desasocia el archivo y devuelve su ruta para borrarlo del disco.
func (s *Store) QuitarFactura(ctx context.Context, usuarioID, id int64) (string, error) {
	const q = `
		WITH previa AS (
			SELECT id, coalesce(factura_ruta, '') AS ruta
			FROM movimientos
			WHERE id = $1 AND usuario_id = $2
		), upd AS (
			UPDATE movimientos m
			SET factura_ruta = NULL, factura_nombre = NULL, factura_tipo = NULL, actualizado_en = now()
			FROM previa
			WHERE m.id = previa.id
			RETURNING m.id
		)
		SELECT previa.ruta FROM previa JOIN upd ON upd.id = previa.id`

	var ruta string
	err := s.db.QueryRowContext(ctx, q, id, usuarioID).Scan(&ruta)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("quitando factura: %w", err)
	}
	if ruta == "" {
		return "", ErrSinFactura
	}
	return ruta, nil
}

// RutaFactura devuelve la ruta en disco y el nombre original para la descarga.
func (s *Store) RutaFactura(ctx context.Context, usuarioID, id int64) (ruta, nombre, tipo string, err error) {
	const q = `
		SELECT coalesce(factura_ruta, ''), coalesce(factura_nombre, ''), coalesce(factura_tipo, '')
		FROM movimientos
		WHERE id = $1 AND usuario_id = $2`

	err = s.db.QueryRowContext(ctx, q, id, usuarioID).Scan(&ruta, &nombre, &tipo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", ErrNoEncontrado
	}
	if err != nil {
		return "", "", "", fmt.Errorf("consultando factura: %w", err)
	}
	if ruta == "" {
		return "", "", "", ErrSinFactura
	}
	return ruta, nombre, tipo, nil
}

func (s *Store) existe(ctx context.Context, usuarioID, id int64) (bool, error) {
	var existe bool
	err := s.db.QueryRowContext(ctx,
		`SELECT exists(SELECT 1 FROM movimientos WHERE id = $1 AND usuario_id = $2)`,
		id, usuarioID).Scan(&existe)
	if err != nil {
		return false, fmt.Errorf("verificando movimiento: %w", err)
	}
	return existe, nil
}

// escaneable abstrae *sql.Row y *sql.Rows: ambos tienen Scan con la misma firma.
// Asi una sola funcion sirve para el listado y para las consultas de una fila.
type escaneable interface {
	Scan(dest ...any) error
}

func escanear(fila escaneable) (*Movimiento, error) {
	var (
		m             Movimiento
		fecha         time.Time
		medioID       sql.NullInt64
		medioNombre   sql.NullString
		cobroID       sql.NullInt64
		cobroNombre   sql.NullString
		aQuien        sql.NullString
		estado        sql.NullString
		cobrarEl      sql.NullTime
		facturaRuta   sql.NullString
		facturaNombre sql.NullString
		facturaTipo   sql.NullString
	)

	err := fila.Scan(
		&m.ID, &m.CategoriaID, &m.CategoriaNombre, &medioID, &medioNombre, &cobroID, &cobroNombre, &m.Tipo, &m.Monto, &fecha,
		&m.Descripcion, &aQuien, &estado, &cobrarEl,
		&facturaRuta, &facturaNombre, &facturaTipo,
		&m.CreadoEn, &m.ActualizadoEn,
		&m.Abonado, &m.Saldo, &m.Abonos, &m.Cuotas,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("leyendo movimiento: %w", err)
	}

	m.Fecha = fecha.Format(FormatoFecha)

	if medioID.Valid {
		m.MedioPagoID = &medioID.Int64
		if medioNombre.Valid {
			m.MedioPagoNombre = &medioNombre.String
		}
	}
	if cobroID.Valid {
		m.MedioCobroID = &cobroID.Int64
		if cobroNombre.Valid {
			m.MedioCobroNombre = &cobroNombre.String
		}
	}

	if aQuien.Valid {
		m.AQuien = &aQuien.String
	}
	if estado.Valid {
		m.Estado = &estado.String
	}
	if cobrarEl.Valid {
		dia := cobrarEl.Time.Format(FormatoFecha)
		m.CobrarEl = &dia
	}
	if facturaRuta.Valid && facturaRuta.String != "" {
		// Nunca exponemos la ruta en disco: solo el endpoint de descarga.
		m.Factura = &Factura{
			Nombre: facturaNombre.String,
			Tipo:   facturaTipo.String,
			URL:    fmt.Sprintf("/api/movimientos/%d/factura", m.ID),
		}
	}

	return &m, nil
}

// traducirCheck convierte el fallo de un CHECK de la base en el error de Go
// que le corresponde. Validar ya revisa todas estas reglas; esto cubre al que
// llegue por otro camino (el asistente con una fecha rara, un cliente viejo) y
// garantiza que el usuario vea un mensaje util y no un 500.
//
// Si el CHECK que fallo no es ninguno de los conocidos, el error sube tal cual
// para que quede en la bitacora: es un bug nuestro, no un dato malo del usuario.
func traducirCheck(err error, contexto string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "movimientos_cobrar_el_despues_del_prestamo":
			return ErrCobroAntes
		case "movimientos_traslado_completo":
			return ErrMismoMedio
		}
	}
	return fmt.Errorf("%s: %w", contexto, err)
}
