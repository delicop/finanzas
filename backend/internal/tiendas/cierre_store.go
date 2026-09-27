package tiendas

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DatosCierre son las casillas que se escriben en la hoja, ya validadas.
// Lo que se calcula (diferencias y totales) no esta aqui: sale al leer.
type DatosCierre struct {
	Fecha       string
	Responsable string

	QRBanco  string
	QRTienda string

	DatafonoReporte string
	DatafonoTienda  string

	EfectivoBillete string
	EfectivoMoneda  string
	EfectivoTienda  string

	VentaTienda string
	Novedades   string

	Lineas []Linea
}

// efectivoContado y metodosPago se repiten en varias casillas calculadas.
// Escribirlas una vez evita que dos de ellas terminen diciendo cosas
// distintas sobre las mismas cifras.
const (
	efectivoContado = `(c.efectivo_billete + c.efectivo_moneda)`
	metodosPago     = `(c.qr_banco + c.datafono_reporte + c.efectivo_billete + c.efectivo_moneda)`
	// Lo que salio de la caja durante el dia. Los pagos por Nequi no entran:
	// van aparte en la hoja y su plata ya viene contada en lo del banco.
	salidas       = `(coalesce(l.compra, 0) + coalesce(l.gasto, 0) + coalesce(l.descuento, 0) + coalesce(l.vale, 0))`
	deberiaQuedar = `(c.venta_tienda - ` + salidas + `)`
)

// columnasCierre: lo escrito y, detras, lo calculado. Todo el dinero sale como
// texto de una columna NUMERIC; Go no hace cuentas con plata.
const columnasCierre = `
	c.id, c.tienda_id, t.nombre, c.fecha, c.responsable,
	c.qr_banco::text, c.qr_tienda::text,
	c.datafono_reporte::text, c.datafono_tienda::text,
	c.efectivo_billete::text, c.efectivo_moneda::text, c.efectivo_tienda::text,
	c.venta_tienda::text, c.novedades,
	c.foto_nombre, c.foto_tipo, (c.foto_ruta <> ''),
	(c.qr_tienda - c.qr_banco)::numeric(14,2)::text,
	(c.datafono_tienda - c.datafono_reporte)::numeric(14,2)::text,
	` + efectivoContado + `::numeric(14,2)::text,
	(` + efectivoContado + ` - c.efectivo_tienda)::numeric(14,2)::text,
	` + metodosPago + `::numeric(14,2)::text,
	(` + metodosPago + ` - c.venta_tienda)::numeric(14,2)::text,
	coalesce(l.pago_nequi, 0)::numeric(14,2)::text,
	coalesce(l.compra, 0)::numeric(14,2)::text,
	coalesce(l.gasto, 0)::numeric(14,2)::text,
	coalesce(l.descuento, 0)::numeric(14,2)::text,
	coalesce(l.vale, 0)::numeric(14,2)::text,
	` + salidas + `::numeric(14,2)::text,
	` + deberiaQuedar + `::numeric(14,2)::text,
	(` + metodosPago + ` - ` + deberiaQuedar + `)::numeric(14,2)::text`

// Los totales de las cinco listas, en una sola pasada por cierre.
const unionesCierre = `
	JOIN tiendas t ON t.id = c.tienda_id
	LEFT JOIN LATERAL (
		SELECT sum(monto) FILTER (WHERE grupo = 'pago_nequi') AS pago_nequi,
		       sum(monto) FILTER (WHERE grupo = 'compra')     AS compra,
		       sum(monto) FILTER (WHERE grupo = 'gasto')      AS gasto,
		       sum(monto) FILTER (WHERE grupo = 'descuento')  AS descuento,
		       sum(monto) FILTER (WHERE grupo = 'vale')       AS vale
		FROM cierre_lineas WHERE cierre_id = c.id
	) l ON true`

func escanearCierre(fila interface{ Scan(...any) error }) (*Cierre, error) {
	var (
		c          Cierre
		fecha      time.Time
		fotoNombre string
		fotoTipo   string
		tieneFoto  bool
	)
	err := fila.Scan(
		&c.ID, &c.TiendaID, &c.TiendaNombre, &fecha, &c.Responsable,
		&c.QRBanco, &c.QRTienda,
		&c.DatafonoReporte, &c.DatafonoTienda,
		&c.EfectivoBillete, &c.EfectivoMoneda, &c.EfectivoTienda,
		&c.VentaTienda, &c.Novedades,
		&fotoNombre, &fotoTipo, &tieneFoto,
		&c.Totales.QRDiferencia, &c.Totales.DatafonoDiferencia,
		&c.Totales.EfectivoTotal, &c.Totales.EfectivoDiferencia,
		&c.Totales.MetodosPago, &c.Totales.VentaDiferencia,
		&c.Totales.PagosNequi, &c.Totales.Compras, &c.Totales.Gastos,
		&c.Totales.Descuentos, &c.Totales.Vales,
		&c.Totales.Salidas, &c.Totales.DeberiaQuedar, &c.Totales.QuedaDiferencia,
	)
	if err != nil {
		return nil, err
	}
	c.Fecha = fecha.Format("2006-01-02")
	if tieneFoto {
		// La ruta en disco no sale nunca de aqui: solo el endpoint por donde
		// se descarga, que va detras del mismo login que todo lo demas.
		c.Foto = &Foto{
			Nombre: fotoNombre,
			Tipo:   fotoTipo,
			URL:    fmt.Sprintf("/api/tiendas/%d/cierres/%d/foto", c.TiendaID, c.ID),
		}
	}
	// Nunca nil: el JSON tiene que ser [] y no null cuando la hoja no tiene
	// ninguna linea escrita.
	c.Lineas = []Linea{}
	return &c, nil
}

// ListarCierres trae las hojas de una tienda, de la mas nueva a la mas vieja.
// Sin las lineas: la lista muestra el dia, quien respondio y si cuadro.
func (s *Store) ListarCierres(ctx context.Context, usuarioID, tiendaID int64) ([]Cierre, error) {
	q := `SELECT ` + columnasCierre + `
		FROM cierres c ` + unionesCierre + `
		WHERE c.usuario_id = $1 AND c.tienda_id = $2
		ORDER BY c.fecha DESC`

	filas, err := s.db.QueryContext(ctx, q, usuarioID, tiendaID)
	if err != nil {
		return nil, fmt.Errorf("listando cierres: %w", err)
	}
	defer filas.Close()

	lista := []Cierre{}
	for filas.Next() {
		c, err := escanearCierre(filas)
		if err != nil {
			return nil, fmt.Errorf("leyendo cierre: %w", err)
		}
		lista = append(lista, *c)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo cierres: %w", err)
	}
	return lista, nil
}

// FiltrosCierres son los del listado. Los campos vacios no filtran.
type FiltrosCierres struct {
	TiendaID int64
	Desde    string // AAAA-MM-DD
	Hasta    string
	// Limite pone un LIMIT en la consulta; cero es sin tope. Lo deja en cero
	// quien no lo lea del query (ver ExportarCierres, que necesita todo).
	Limite int
}

// Activos dice si se filtro por algo ademas del rango. Lo usa el informe para
// avisar en el archivo que no es todo.
func (f FiltrosCierres) Activos() bool { return f.TiendaID > 0 }

// where arma el filtro. Los valores SIEMPRE van como parametros: concatenar
// uno dentro del SQL seria inyeccion.
func (f FiltrosCierres) where(usuarioID int64) (string, []any) {
	condiciones := []string{"c.usuario_id = $1"}
	args := []any{usuarioID}

	agregar := func(expresion string, valor any) {
		args = append(args, valor)
		condiciones = append(condiciones, fmt.Sprintf(expresion, len(args)))
	}
	if f.TiendaID > 0 {
		agregar("c.tienda_id = $%d", f.TiendaID)
	}
	if f.Desde != "" {
		agregar("c.fecha >= $%d::date", f.Desde)
	}
	if f.Hasta != "" {
		agregar("c.fecha <= $%d::date", f.Hasta)
	}
	return " WHERE " + strings.Join(condiciones, " AND "), args
}

// ListarTodosLosCierres trae los de TODAS las tiendas del usuario.
//
// Es lo que pide la seccion Cierres, que se lee como los movimientos: todo
// junto y en orden, con el nombre de la tienda en cada renglon.
func (s *Store) ListarTodosLosCierres(ctx context.Context, usuarioID int64, f FiltrosCierres) ([]Cierre, error) {
	where, args := f.where(usuarioID)
	q := `SELECT ` + columnasCierre + `
		FROM cierres c ` + unionesCierre + where + `
		ORDER BY c.fecha DESC, lower(t.nombre)`
	if f.Limite > 0 {
		args = append(args, f.Limite)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}

	filas, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listando todos los cierres: %w", err)
	}
	defer filas.Close()

	lista := []Cierre{}
	for filas.Next() {
		c, err := escanearCierre(filas)
		if err != nil {
			return nil, fmt.Errorf("leyendo cierre: %w", err)
		}
		lista = append(lista, *c)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo cierres: %w", err)
	}
	return lista, nil
}

// CierrePorID trae una hoja completa, con sus lineas.
func (s *Store) CierrePorID(ctx context.Context, usuarioID, tiendaID, id int64) (*Cierre, error) {
	q := `SELECT ` + columnasCierre + `
		FROM cierres c ` + unionesCierre + `
		WHERE c.id = $1 AND c.usuario_id = $2 AND c.tienda_id = $3`

	c, err := escanearCierre(s.db.QueryRowContext(ctx, q, id, usuarioID, tiendaID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCierreNoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("consultando cierre: %w", err)
	}

	lineas, err := s.lineasDe(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	c.Lineas = lineas
	return c, nil
}

func (s *Store) lineasDe(ctx context.Context, cierreID int64) ([]Linea, error) {
	const q = `
		SELECT grupo, descripcion, monto::text
		FROM cierre_lineas
		WHERE cierre_id = $1
		ORDER BY grupo, orden, id`

	filas, err := s.db.QueryContext(ctx, q, cierreID)
	if err != nil {
		return nil, fmt.Errorf("listando lineas del cierre: %w", err)
	}
	defer filas.Close()

	lineas := []Linea{}
	for filas.Next() {
		var l Linea
		if err := filas.Scan(&l.Grupo, &l.Descripcion, &l.Monto); err != nil {
			return nil, fmt.Errorf("leyendo linea del cierre: %w", err)
		}
		lineas = append(lineas, l)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo lineas del cierre: %w", err)
	}
	return lineas, nil
}

// CrearCierre guarda la hoja entera: las casillas y sus listas, o ninguna.
//
// Va en una transaccion porque una hoja a medias no es una hoja: un cierre sin
// sus gastos cuadra distinto y nadie se enteraria.
func (s *Store) CrearCierre(ctx context.Context, usuarioID, tiendaID int64, d DatosCierre) (*Cierre, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iniciando transaccion: %w", err)
	}
	defer tx.Rollback()

	// La tienda TIENE que ser del usuario. Va en la misma consulta que
	// inserta: sin este WHERE, un id ajeno crearia el cierre igual.
	const q = `
		WITH tienda AS (
			SELECT id FROM tiendas WHERE id = $2 AND usuario_id = $1
		)
		INSERT INTO cierres (usuario_id, tienda_id, fecha, responsable,
			qr_banco, qr_tienda, datafono_reporte, datafono_tienda,
			efectivo_billete, efectivo_moneda, efectivo_tienda,
			venta_tienda, novedades)
		SELECT $1, tienda.id, $3::date, $4,
			$5::numeric, $6::numeric, $7::numeric, $8::numeric,
			$9::numeric, $10::numeric, $11::numeric,
			$12::numeric, $13
		FROM tienda
		RETURNING id`

	var id int64
	err = tx.QueryRowContext(ctx, q, usuarioID, tiendaID, d.Fecha, d.Responsable,
		d.QRBanco, d.QRTienda, d.DatafonoReporte, d.DatafonoTienda,
		d.EfectivoBillete, d.EfectivoMoneda, d.EfectivoTienda,
		d.VentaTienda, d.Novedades).Scan(&id)

	if errors.Is(err, sql.ErrNoRows) {
		// Sin filas: la tienda no es de este usuario o no existe.
		return nil, ErrNoEncontrada
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrCierreDuplicado
		}
		return nil, fmt.Errorf("creando cierre: %w", err)
	}

	if err := guardarLineas(ctx, tx, id, d.Lineas); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando el cierre: %w", err)
	}

	return s.CierrePorID(ctx, usuarioID, tiendaID, id)
}

// ActualizarCierre reemplaza la hoja entera, listas incluidas.
func (s *Store) ActualizarCierre(ctx context.Context, usuarioID, tiendaID, id int64, d DatosCierre) (*Cierre, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iniciando transaccion: %w", err)
	}
	defer tx.Rollback()

	const q = `
		UPDATE cierres SET
			fecha = $4::date, responsable = $5,
			qr_banco = $6::numeric, qr_tienda = $7::numeric,
			datafono_reporte = $8::numeric, datafono_tienda = $9::numeric,
			efectivo_billete = $10::numeric, efectivo_moneda = $11::numeric,
			efectivo_tienda = $12::numeric,
			venta_tienda = $13::numeric,
			novedades = $14, actualizado_en = now()
		WHERE id = $1 AND usuario_id = $2 AND tienda_id = $3
		RETURNING id`

	var actualizado int64
	err = tx.QueryRowContext(ctx, q, id, usuarioID, tiendaID, d.Fecha, d.Responsable,
		d.QRBanco, d.QRTienda, d.DatafonoReporte, d.DatafonoTienda,
		d.EfectivoBillete, d.EfectivoMoneda, d.EfectivoTienda,
		d.VentaTienda, d.Novedades).Scan(&actualizado)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCierreNoEncontrado
	}
	if err != nil {
		if esViolacionUnica(err) {
			return nil, ErrCierreDuplicado
		}
		return nil, fmt.Errorf("actualizando cierre: %w", err)
	}

	// Las listas se reemplazan enteras y no se van parcheando renglon por
	// renglon: la hoja se guarda como quedo en pantalla.
	if _, err := tx.ExecContext(ctx, `DELETE FROM cierre_lineas WHERE cierre_id = $1`, id); err != nil {
		return nil, fmt.Errorf("limpiando las lineas del cierre: %w", err)
	}
	if err := guardarLineas(ctx, tx, id, d.Lineas); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("confirmando el cierre: %w", err)
	}

	return s.CierrePorID(ctx, usuarioID, tiendaID, id)
}

func guardarLineas(ctx context.Context, tx *sql.Tx, cierreID int64, lineas []Linea) error {
	const q = `
		INSERT INTO cierre_lineas (cierre_id, grupo, descripcion, monto, orden)
		VALUES ($1, $2, $3, $4::numeric, $5)`

	for i, l := range lineas {
		if _, err := tx.ExecContext(ctx, q, cierreID, l.Grupo, l.Descripcion, l.Monto, i); err != nil {
			return fmt.Errorf("guardando la linea %q del cierre: %w", l.Descripcion, err)
		}
	}
	return nil
}

// EliminarCierre borra la hoja y devuelve la ruta de su foto, vacia si no
// tenia, para que el handler borre el archivo despues: ninguna llave foranea
// llega hasta el disco, y despues del DELETE ya no queda fila que diga de
// quien era. Es el mismo RETURNING que usa QuitarFoto (ver foto.go).
func (s *Store) EliminarCierre(ctx context.Context, usuarioID, tiendaID, id int64) (string, error) {
	// Las lineas se van solas (CASCADE).
	const q = `DELETE FROM cierres WHERE id = $1 AND usuario_id = $2 AND tienda_id = $3
	           RETURNING foto_ruta`

	var ruta string
	err := s.db.QueryRowContext(ctx, q, id, usuarioID, tiendaID).Scan(&ruta)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCierreNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("eliminando cierre: %w", err)
	}
	return ruta, nil
}
