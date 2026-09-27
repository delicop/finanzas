package tiendas

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"finanzas/internal/httpx"
)

// Foto es la hoja firmada, adjunta al cierre. Nunca se expone la ruta en
// disco: solo el endpoint por donde se descarga.
type Foto struct {
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`
	URL    string `json:"url"`
}

// ArchivoSubido es lo que el almacen devuelve al guardar.
type ArchivoSubido struct {
	Ruta   string // relativa a la raiz del almacen
	Nombre string // el nombre original, ya saneado
	Tipo   string // el mime REAL, detectado leyendo los bytes
}

// Archivos es todo lo que este paquete necesita del almacen.
//
// Es una interfaz y no el tipo concreto para no importar el paquete de los
// movimientos entero: la foto de un cierre y la foto de una factura son el
// mismo problema (validar tipo y tamaño, escribir con nombre aleatorio,
// no dejar salir la ruta de su carpeta) y comparten el mismo almacen, pero
// aqui no hace falta saber nada de facturas. Quien conecta los dos es el
// router, que ya conoce a los dos paquetes.
type Archivos interface {
	Guardar(archivo multipart.File, encabezado *multipart.FileHeader) (ArchivoSubido, error)
	Abrir(rutaRelativa string) (*os.File, error)
	Eliminar(rutaRelativa string) error
}

// MaxFotoBytes: el mismo limite que una factura. Una foto de la hoja cabe de
// sobra y evita que alguien llene el disco.
const MaxFotoBytes = 10 << 20

var (
	ErrFotoMuyGrande = errors.New("La foto supera el tamaño máximo de 10 MB")
	ErrFotoTipo      = errors.New("Solo se aceptan imágenes (JPG, PNG, WEBP) o PDF")
	ErrFotoVacia     = errors.New("El archivo está vacío")
	ErrSinFoto       = errors.New("el cierre no tiene foto")
)

/* ------------------------------- store ---------------------------------- */

// GuardarFoto deja la foto en el cierre y devuelve la RUTA DE LA ANTERIOR,
// para poder borrarla del disco despues de que la base ya no la nombre.
//
// Ese orden importa: si se borrara primero el archivo y luego fallara el
// UPDATE, la fila quedaria apuntando a algo que ya no existe.
func (s *Store) GuardarFoto(ctx context.Context, usuarioID, tiendaID, id int64, a ArchivoSubido) (string, error) {
	// Las CTE comparten el mismo snapshot: "previa" ve la fila TAL COMO
	// ESTABA, aunque el UPDATE de al lado ya la haya cambiado.
	const q = `
		WITH previa AS (
			SELECT id, foto_ruta FROM cierres
			WHERE id = $1 AND usuario_id = $2 AND tienda_id = $3
		), upd AS (
			UPDATE cierres c
			SET foto_ruta = $4, foto_nombre = $5, foto_tipo = $6, actualizado_en = now()
			FROM previa WHERE c.id = previa.id
			RETURNING c.id
		)
		SELECT previa.foto_ruta FROM previa JOIN upd ON upd.id = previa.id`

	var anterior string
	err := s.db.QueryRowContext(ctx, q, id, usuarioID, tiendaID, a.Ruta, a.Nombre, a.Tipo).Scan(&anterior)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCierreNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("guardando la foto del cierre: %w", err)
	}
	return anterior, nil
}

// FotoDe devuelve donde esta el archivo y como se llamaba.
func (s *Store) FotoDe(ctx context.Context, usuarioID, tiendaID, id int64) (ArchivoSubido, error) {
	const q = `
		SELECT foto_ruta, foto_nombre, foto_tipo FROM cierres
		WHERE id = $1 AND usuario_id = $2 AND tienda_id = $3`

	var a ArchivoSubido
	err := s.db.QueryRowContext(ctx, q, id, usuarioID, tiendaID).Scan(&a.Ruta, &a.Nombre, &a.Tipo)
	if errors.Is(err, sql.ErrNoRows) {
		return ArchivoSubido{}, ErrCierreNoEncontrado
	}
	if err != nil {
		return ArchivoSubido{}, fmt.Errorf("consultando la foto del cierre: %w", err)
	}
	if a.Ruta == "" {
		return ArchivoSubido{}, ErrSinFoto
	}
	return a, nil
}

// QuitarFoto la desprende del cierre y devuelve su ruta para borrar el archivo.
func (s *Store) QuitarFoto(ctx context.Context, usuarioID, tiendaID, id int64) (string, error) {
	const q = `
		WITH previa AS (
			SELECT id, foto_ruta FROM cierres
			WHERE id = $1 AND usuario_id = $2 AND tienda_id = $3
		), upd AS (
			UPDATE cierres c
			SET foto_ruta = '', foto_nombre = '', foto_tipo = '', actualizado_en = now()
			FROM previa WHERE c.id = previa.id
			RETURNING c.id
		)
		SELECT previa.foto_ruta FROM previa JOIN upd ON upd.id = previa.id`

	var anterior string
	err := s.db.QueryRowContext(ctx, q, id, usuarioID, tiendaID).Scan(&anterior)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCierreNoEncontrado
	}
	if err != nil {
		return "", fmt.Errorf("quitando la foto del cierre: %w", err)
	}
	if anterior == "" {
		return "", ErrSinFoto
	}
	return anterior, nil
}

// FotosDe devuelve las rutas de las fotos de todos los cierres de un usuario,
// para poder borrarlas del disco cuando su cuenta se va: el CASCADE se lleva
// las filas, pero al disco no llega ninguna llave foranea.
//
// Vive aqui y no en admin, que es quien borra la cuenta, porque la consulta es
// sobre las tablas de ESTE paquete: admin la recibe como una funcion y no sabe
// que hay dentro.
func (s *Store) FotosDe(ctx context.Context, usuarioID int64) ([]string, error) {
	const q = `SELECT foto_ruta FROM cierres WHERE usuario_id = $1 AND foto_ruta <> ''`

	filas, err := s.db.QueryContext(ctx, q, usuarioID)
	if err != nil {
		return nil, fmt.Errorf("listando las fotos de los cierres: %w", err)
	}
	defer filas.Close()

	rutas := []string{}
	for filas.Next() {
		var ruta string
		if err := filas.Scan(&ruta); err != nil {
			return nil, fmt.Errorf("leyendo la ruta de una foto: %w", err)
		}
		rutas = append(rutas, ruta)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo las fotos de los cierres: %w", err)
	}
	return rutas, nil
}

/* ------------------------------ handlers -------------------------------- */

// SubirFoto recibe la hoja escaneada o la foto del papel.
func (h *Handler) SubirFoto(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	id, ok := idDeParametro(w, r, "cierreID")
	if !ok {
		return
	}
	if h.archivos == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "Las fotos no están disponibles en este servidor")
		return
	}

	// Doble freno de tamaño: MaxBytesReader corta la conexión si el cuerpo
	// entero se pasa, y ParseMultipartForm limita cuánto se guarda en RAM.
	r.Body = http.MaxBytesReader(w, r.Body, MaxFotoBytes+(1<<20))

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, ErrFotoMuyGrande.Error())
			return
		}
		httpx.Error(w, http.StatusBadRequest, "No se pudo leer el archivo enviado")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	archivo, encabezado, err := r.FormFile("foto")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, `Falta el archivo en el campo "foto"`)
		return
	}
	defer archivo.Close()

	// Que el cierre exista se revisa ANTES de escribir en disco, para no
	// dejar basura por un id equivocado.
	if _, err := h.store.CierrePorID(r.Context(), usuarioID, tiendaID, id); err != nil {
		if errors.Is(err, ErrCierreNoEncontrado) {
			httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
			return
		}
		httpx.ErrorInterno(w, r, err, "tiendas: verificando el cierre de la foto")
		return
	}

	guardado, err := h.archivos.Guardar(archivo, encabezado)
	switch {
	case errors.Is(err, ErrFotoMuyGrande):
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	case errors.Is(err, ErrFotoTipo), errors.Is(err, ErrFotoVacia):
		httpx.ErrorCampos(w, map[string]string{"foto": err.Error()})
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: guardando la foto del cierre")
		return
	}

	anterior, err := h.store.GuardarFoto(r.Context(), usuarioID, tiendaID, id, guardado)
	if err != nil {
		// La fila no quedó apuntando al archivo nuevo: se borra para no
		// dejarlo huérfano en el disco.
		_ = h.archivos.Eliminar(guardado.Ruta)
		if errors.Is(err, ErrCierreNoEncontrado) {
			httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
			return
		}
		httpx.ErrorInterno(w, r, err, "tiendas: guardando la foto del cierre")
		return
	}
	// La vieja se borra DESPUÉS de que la base ya no la nombra. Si esto
	// falla, queda un archivo de sobra en disco: molesto, no grave.
	if anterior != "" && anterior != guardado.Ruta {
		_ = h.archivos.Eliminar(anterior)
	}

	cierre, err := h.store.CierrePorID(r.Context(), usuarioID, tiendaID, id)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: releyendo el cierre con su foto")
		return
	}
	httpx.JSON(w, http.StatusOK, cierre)
}

// VerFoto entrega el archivo. Va detras del mismo login que todo lo demas:
// la foto de una hoja de caja no es publica.
func (h *Handler) VerFoto(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	id, ok := idDeParametro(w, r, "cierreID")
	if !ok {
		return
	}
	if h.archivos == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "Las fotos no están disponibles en este servidor")
		return
	}

	datos, err := h.store.FotoDe(r.Context(), usuarioID, tiendaID, id)
	switch {
	case errors.Is(err, ErrCierreNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
		return
	case errors.Is(err, ErrSinFoto):
		httpx.Error(w, http.StatusNotFound, "Ese cierre no tiene foto")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: consultando la foto del cierre")
		return
	}

	archivo, err := h.archivos.Abrir(datos.Ruta)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: abriendo la foto del cierre")
		return
	}
	defer archivo.Close()

	info, err := archivo.Stat()
	if err != nil {
		httpx.ErrorInterno(w, r, err, "tiendas: leyendo la foto del cierre")
		return
	}

	w.Header().Set("Content-Type", datos.Tipo)
	// inline: se ve en la app. El nombre va entre comillas y sin saltos de
	// linea, que es lo unico que podria romper la cabecera.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`inline; filename="%s"`, strings.NewReplacer("\"", "", "\r", "", "\n", "").Replace(datos.Nombre)))
	http.ServeContent(w, r, datos.Nombre, info.ModTime(), archivo)
}

// QuitarFoto la desprende del cierre y borra el archivo.
func (h *Handler) QuitarFoto(w http.ResponseWriter, r *http.Request) {
	usuarioID, tiendaID, ok := h.contextoTienda(w, r)
	if !ok {
		return
	}
	id, ok := idDeParametro(w, r, "cierreID")
	if !ok {
		return
	}

	ruta, err := h.store.QuitarFoto(r.Context(), usuarioID, tiendaID, id)
	switch {
	case errors.Is(err, ErrCierreNoEncontrado):
		httpx.Error(w, http.StatusNotFound, "Cierre no encontrado")
		return
	case errors.Is(err, ErrSinFoto):
		httpx.Error(w, http.StatusNotFound, "Ese cierre no tiene foto")
		return
	case err != nil:
		httpx.ErrorInterno(w, r, err, "tiendas: quitando la foto del cierre")
		return
	}

	if h.archivos != nil {
		_ = h.archivos.Eliminar(ruta)
	}
	w.WriteHeader(http.StatusNoContent)
}
