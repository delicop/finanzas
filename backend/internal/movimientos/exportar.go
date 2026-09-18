package movimientos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"finanzas/internal/httpx"
)

// Exportar a Excel o PDF los movimientos de un rango de fechas, para el
// contador o para guardar.
//
// Se exporta lo mismo que se ve: los filtros son los del listado (tipo,
// categoria, medio, texto), y el rango de fechas es obligatorio.

// MaxExportar es el tope de filas por archivo. Una persona no llega ni cerca
// en un año; el tope existe para que una peticion no ponga a la Raspberry a
// armar un PDF de mil paginas.
const MaxExportar = 5000

// MaxDiasExportar es el rango mas largo que se acepta: cinco años.
const MaxDiasExportar = 5 * 366

var ErrDemasiados = fmt.Errorf("hay más de %d movimientos en ese rango", MaxExportar)

// zonaColombia es la hora de Colombia (UTC-5, sin horario de verano). Fija y
// no con time.LoadLocation: la imagen de Docker no trae la base de zonas
// horarias, y para una sola zona sin cambios no hace falta.
var zonaColombia = time.FixedZone("COT", -5*60*60)

// Informe es todo lo que va en el archivo.
type Informe struct {
	Titular     string
	Desde       string
	Hasta       string
	ConFiltros  bool // si ademas del rango se filtro por algo
	Generado    time.Time
	Totales     Totales
	Movimientos []Movimiento
}

// Informe junta los movimientos del rango, en orden cronologico (como se lee
// un extracto), con sus totales calculados por Postgres.
func (s *Store) Informe(ctx context.Context, usuarioID int64, f Filtros) (*Informe, error) {
	where, args := filtrosSQL(usuarioID, f)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM movimientos m `+where, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("contando movimientos a exportar: %w", err)
	}
	if total > MaxExportar {
		return nil, ErrDemasiados
	}

	inf := &Informe{
		Desde:      f.Desde,
		Hasta:      f.Hasta,
		ConFiltros: f.CategoriaID > 0 || f.MedioPagoID > 0 || f.Tipo != "" || f.Estado != "" || f.Texto != "",
		Generado:   time.Now().In(zonaColombia),
	}

	// Las mismas reglas del resumen (ver resumen.go): lo que se debe es el
	// SALDO (monto menos abonos), en los dos sentidos, y los traslados no
	// entran en ninguna de las cuatro cifras.
	//
	// Ojo con lo que significan estos totales: son los del RANGO exportado,
	// no los de toda la cuenta. Un abono hecho fuera del rango igual baja el
	// saldo del prestamo que si esta dentro — y tiene que ser asi, porque lo
	// que se debe hoy es lo que se debe hoy, no lo que se debia en septiembre.
	err := s.db.QueryRowContext(ctx, `
		WITH t AS (
			SELECT
				coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'recibi'), 0) AS recibido,
				coalesce(sum(m.monto) FILTER (WHERE m.tipo = 'pague'), 0) AS pagado,
				coalesce(sum(saldo.falta) FILTER (WHERE m.tipo = 'preste'), 0) AS por_cobrar,
				coalesce(sum(saldo.falta) FILTER (WHERE m.tipo = 'me_prestaron'), 0) AS por_pagar,
				coalesce(sum(m.monto - saldo.falta) FILTER (WHERE m.tipo = 'preste'), 0) AS recuperado,
				coalesce(sum(m.monto - saldo.falta) FILTER (WHERE m.tipo = 'me_prestaron'), 0) AS abonado
			FROM movimientos m
			LEFT JOIN LATERAL (
				SELECT m.monto - coalesce(sum(a.monto), 0) AS falta
				FROM abonos a WHERE a.movimiento_id = m.id
			) saldo ON true
			`+where+`
		)
		-- El ::numeric(14,2) antes del ::text no es adorno: sin el, un total en
		-- cero sale como "0" y los demas como "0.00", y el archivo queda con
		-- dos formatos distintos en la misma columna.
		SELECT recibido::numeric(14,2)::text, pagado::numeric(14,2)::text,
		       por_cobrar::numeric(14,2)::text, por_pagar::numeric(14,2)::text,
		       recuperado::numeric(14,2)::text, abonado::numeric(14,2)::text,
		       (recibido - pagado - por_cobrar + por_pagar)::numeric(14,2)::text,
		       (SELECT coalesce(nullif(u.nombre, ''), u.email) FROM usuarios u WHERE u.id = $1)
		FROM t`, args...).Scan(
		&inf.Totales.Recibido, &inf.Totales.Pagado, &inf.Totales.PorCobrar,
		&inf.Totales.PorPagar, &inf.Totales.Recuperado, &inf.Totales.Abonado,
		&inf.Totales.Balance, &inf.Titular)
	if err != nil {
		return nil, fmt.Errorf("sumando movimientos a exportar: %w", err)
	}

	filas, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM movimientos m
		%s
		%s
		ORDER BY m.fecha, m.id`, columnas, uniones, where), args...)
	if err != nil {
		return nil, fmt.Errorf("listando movimientos a exportar: %w", err)
	}
	defer filas.Close()

	inf.Movimientos = make([]Movimiento, 0, total)
	for filas.Next() {
		m, err := escanear(filas)
		if err != nil {
			return nil, err
		}
		inf.Movimientos = append(inf.Movimientos, *m)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo movimientos a exportar: %w", err)
	}
	return inf, nil
}

// formatos de exportacion: extension -> tipo MIME y generador.
var formatos = map[string]struct {
	tipo    string
	generar func(*Informe, *bytes.Buffer) error
}{
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", generarExcel},
	"pdf":  {"application/pdf", generarPDF},
}

// Exportar responde el archivo: GET /api/movimientos/exportar?formato=xlsx
// &desde=2026-09-01&hasta=2026-09-30 (+ los mismos filtros del listado).
func (h *Handler) Exportar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := httpx.UsuarioID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "No autenticado")
		return
	}

	q := r.URL.Query()
	formato := strings.ToLower(strings.TrimSpace(q.Get("formato")))
	f := Filtros{
		CategoriaID: enteroDeQuery(q.Get("categoria_id")),
		MedioPagoID: enteroDeQuery(q.Get("medio_pago_id")),
		Tipo:        strings.TrimSpace(q.Get("tipo")),
		Estado:      strings.TrimSpace(q.Get("estado")),
		Desde:       strings.TrimSpace(q.Get("desde")),
		Hasta:       strings.TrimSpace(q.Get("hasta")),
		Texto:       strings.TrimSpace(q.Get("q")),
	}

	v := httpx.NuevoValidador()
	_, formatoValido := formatos[formato]
	v.Check(formatoValido, "formato", "Formato inválido: usa xlsx o pdf")
	v.Check(f.Tipo == "" || EsTipoValido(f.Tipo), "tipo", TiposValidosMsg)
	v.Check(f.Estado == "" || EsEstadoValido(f.Estado), "estado", EstadosValidosMsg)
	v.Check(fechaValida(f.Desde), "desde", "Indica desde qué fecha (AAAA-MM-DD)")
	v.Check(fechaValida(f.Hasta), "hasta", "Indica hasta qué fecha (AAAA-MM-DD)")
	if v.Valido() {
		desde, _ := time.Parse(FormatoFecha, f.Desde)
		hasta, _ := time.Parse(FormatoFecha, f.Hasta)
		v.Check(!hasta.Before(desde), "hasta", "La fecha final no puede ser anterior a la inicial")
		v.Check(hasta.Sub(desde) <= MaxDiasExportar*24*time.Hour, "desde", "El rango no puede pasar de cinco años")
	}
	if !v.Valido() {
		httpx.ErrorCampos(w, v.Campos)
		return
	}

	inf, err := h.store.Informe(r.Context(), usuarioID, f)
	if errors.Is(err, ErrDemasiados) {
		httpx.Error(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("Hay más de %d movimientos en ese rango. Escoge un rango más corto.", MaxExportar))
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "movimientos: armando la exportación")
		return
	}

	// Se arma entero en memoria antes de responder: si algo falla a mitad,
	// el usuario recibe un error y no un archivo roto que parece bueno.
	var archivo bytes.Buffer
	if err := formatos[formato].generar(inf, &archivo); err != nil {
		httpx.ErrorInterno(w, r, err, "movimientos: generando "+formato)
		return
	}

	nombre := fmt.Sprintf("movimientos-%s-a-%s.%s", f.Desde, f.Hasta, formato)
	w.Header().Set("Content-Type", formatos[formato].tipo)
	w.Header().Set("Content-Disposition", `attachment; filename="`+nombre+`"`)
	w.Header().Set("Content-Length", fmt.Sprint(archivo.Len()))
	// Son datos de plata: que ningun proxy ni el navegador lo guarden.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = archivo.WriteTo(w)
}
