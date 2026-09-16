package movimientos

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// MaxFacturaBytes limita el archivo a 10 MB. Una foto de factura o un PDF
// escaneado caben de sobra, y evita que alguien llene el disco de la Raspberry.
const MaxFacturaBytes = 10 << 20

// tiposPermitidos mapea el mime REAL detectado -> extension que le damos.
// Solo imagenes y PDF: nada de ejecutables ni archivos raros en el disco.
var tiposPermitidos = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

var (
	ErrFacturaMuyGrande = errors.New("La factura supera el tamaño máximo de 10 MB")
	ErrFacturaTipo      = errors.New("Solo se aceptan imágenes (JPG, PNG, WEBP, HEIC) o PDF")
	ErrFacturaVacia     = errors.New("El archivo está vacío")
)

// AlmacenFacturas guarda los archivos en disco local.
//
// Toda la logica de archivos vive aqui. El dia que quieras mover esto a un
// disco externo de la Raspberry o a S3, solo cambia este archivo.
type AlmacenFacturas struct {
	raiz string
}

func NuevoAlmacen(raiz string) (*AlmacenFacturas, error) {
	if err := os.MkdirAll(raiz, 0o755); err != nil {
		return nil, fmt.Errorf("creando carpeta de uploads: %w", err)
	}
	return &AlmacenFacturas{raiz: raiz}, nil
}

// ArchivoGuardado es lo que se persiste en la fila del movimiento.
type ArchivoGuardado struct {
	Ruta   string // relativa a la raiz, ej: "2026/09/a1b2c3....jpg"
	Nombre string // nombre original, ya saneado
	Tipo   string // mime real detectado
}

// Guardar valida y escribe el archivo en disco.
//
// Tres decisiones de seguridad aqui:
//
//  1. El tipo se DETECTA leyendo los primeros bytes (http.DetectContentType),
//     no se confia en el header Content-Type ni en la extension: ambos los
//     controla el cliente y se falsifican en un segundo.
//
//  2. El nombre en disco es aleatorio. Si usaramos el nombre original, un
//     archivo llamado "../../algo" podria escribir fuera de /uploads
//     (path traversal), y dos facturas con el mismo nombre se pisarian.
//
//  3. Se guarda por anio/mes para no terminar con miles de archivos sueltos
//     en una sola carpeta, que vuelve lento hasta listarla.
func (a *AlmacenFacturas) Guardar(archivo multipart.File, encabezado *multipart.FileHeader) (*ArchivoGuardado, error) {
	if encabezado.Size == 0 {
		return nil, ErrFacturaVacia
	}
	if encabezado.Size > MaxFacturaBytes {
		return nil, ErrFacturaMuyGrande
	}

	// 512 bytes es lo que necesita DetectContentType para decidir.
	cabecera := make([]byte, 512)
	n, err := io.ReadFull(archivo, cabecera)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("leyendo archivo: %w", err)
	}
	cabecera = cabecera[:n]

	mime := http.DetectContentType(cabecera)
	// DetectContentType a veces devuelve "text/plain; charset=utf-8".
	if i := strings.Index(mime, ";"); i != -1 {
		mime = strings.TrimSpace(mime[:i])
	}

	extension, permitido := tiposPermitidos[mime]
	if !permitido && esHEIC(cabecera) {
		// DetectContentType no conoce HEIC (el formato por defecto del iPhone),
		// asi que lo identificamos por su marca "ftyp" en los primeros bytes.
		mime, extension, permitido = "image/heic", ".heic", true
	}
	if !permitido {
		return nil, ErrFacturaTipo
	}

	// Volvemos al inicio: ya consumimos los primeros 512 bytes al detectar.
	if _, err := archivo.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rebobinando archivo: %w", err)
	}

	ahora := time.Now()
	subcarpeta := filepath.Join(ahora.Format("2006"), ahora.Format("01"))
	if err := os.MkdirAll(filepath.Join(a.raiz, subcarpeta), 0o755); err != nil {
		return nil, fmt.Errorf("creando carpeta destino: %w", err)
	}

	nombreDisco, err := nombreAleatorio(extension)
	if err != nil {
		return nil, err
	}

	rutaRelativa := filepath.ToSlash(filepath.Join(subcarpeta, nombreDisco))
	rutaAbsoluta := filepath.Join(a.raiz, filepath.FromSlash(rutaRelativa))

	// O_EXCL: si el nombre ya existiera (imposible en la practica con 16 bytes
	// aleatorios) fallamos en vez de sobreescribir una factura ajena.
	destino, err := os.OpenFile(rutaAbsoluta, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("creando archivo: %w", err)
	}

	// Copiamos con un byte de margen: si alcanza a copiar mas bytes de los
	// permitidos, el Size del encabezado mentia.
	copiados, err := io.CopyN(destino, archivo, MaxFacturaBytes+1)
	cerrarErr := destino.Close()

	if err != nil && !errors.Is(err, io.EOF) {
		_ = os.Remove(rutaAbsoluta)
		return nil, fmt.Errorf("escribiendo archivo: %w", err)
	}
	if cerrarErr != nil {
		_ = os.Remove(rutaAbsoluta)
		return nil, fmt.Errorf("cerrando archivo: %w", cerrarErr)
	}
	if copiados > MaxFacturaBytes {
		_ = os.Remove(rutaAbsoluta)
		return nil, ErrFacturaMuyGrande
	}

	return &ArchivoGuardado{
		Ruta:   rutaRelativa,
		Nombre: sanearNombre(encabezado.Filename),
		Tipo:   mime,
	}, nil
}

// Abrir devuelve el archivo para descargarlo.
func (a *AlmacenFacturas) Abrir(rutaRelativa string) (*os.File, error) {
	ruta, err := a.rutaSegura(rutaRelativa)
	if err != nil {
		return nil, err
	}
	return os.Open(ruta)
}

// Eliminar borra el archivo del disco. Si ya no existe no es error:
// el objetivo (que no este) ya se cumplio.
func (a *AlmacenFacturas) Eliminar(rutaRelativa string) error {
	ruta, err := a.rutaSegura(rutaRelativa)
	if err != nil {
		return err
	}
	if err := os.Remove(ruta); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// rutaSegura evita que una ruta guardada en la base de datos (manipulada o
// corrupta) pueda apuntar a un archivo fuera de /uploads.
func (a *AlmacenFacturas) rutaSegura(rutaRelativa string) (string, error) {
	limpia := filepath.Clean(filepath.FromSlash(rutaRelativa))

	// Las comprobaciones son explicitas y no dependen del sistema operativo.
	// Ojo con esto: filepath.IsAbs("/etc/passwd") es TRUE en Linux pero FALSE
	// en Windows (alli una ruta absoluta necesita letra de unidad). Si solo
	// usaramos IsAbs, el mismo codigo protegeria distinto segun donde corra.
	separador := string(os.PathSeparator)
	invalida := limpia == "" ||
		limpia == "." ||
		filepath.IsAbs(limpia) ||
		filepath.VolumeName(limpia) != "" ||
		strings.HasPrefix(limpia, separador) ||
		strings.HasPrefix(limpia, "/") ||
		limpia == ".." ||
		strings.HasPrefix(limpia, ".."+separador) ||
		strings.HasPrefix(limpia, "../")

	if invalida {
		return "", errors.New("ruta de factura invalida")
	}

	raizAbs, err := filepath.Abs(a.raiz)
	if err != nil {
		return "", err
	}
	completaAbs, err := filepath.Abs(filepath.Join(a.raiz, limpia))
	if err != nil {
		return "", err
	}

	// Comprobacion final: el resultado tiene que seguir estando dentro de la raiz.
	if !strings.HasPrefix(completaAbs, raizAbs+string(os.PathSeparator)) {
		return "", errors.New("ruta de factura fuera de la carpeta permitida")
	}
	return completaAbs, nil
}

// esHEIC busca la marca "ftyp" + una submarca HEIC en la cabecera del archivo.
func esHEIC(cabecera []byte) bool {
	if len(cabecera) < 12 || string(cabecera[4:8]) != "ftyp" {
		return false
	}
	marca := string(cabecera[8:12])
	switch marca {
	case "heic", "heix", "hevc", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
		return true
	}
	return false
}

func nombreAleatorio(extension string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generando nombre de archivo: %w", err)
	}
	return hex.EncodeToString(b) + extension, nil
}

// sanearNombre deja un nombre presentable para la descarga, sin rutas ni
// caracteres que rompan el header Content-Disposition.
func sanearNombre(nombre string) string {
	nombre = path.Base(strings.ReplaceAll(nombre, `\`, "/"))
	nombre = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, nombre)
	nombre = strings.TrimSpace(nombre)
	nombre = strings.Trim(nombre, "_")

	if nombre == "" || nombre == "." || nombre == ".." {
		return "factura"
	}
	if len(nombre) > 120 {
		nombre = nombre[:120]
	}
	return nombre
}
