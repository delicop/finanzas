package movimientos

import (
	"bytes"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cabeceras reales de cada formato: http.DetectContentType las reconoce
// por estos primeros bytes, no por la extensión del archivo.
var (
	cabeceraPNG = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	cabeceraJPG = []byte{0xFF, 0xD8, 0xFF, 0xE0}
	cabeceraPDF = []byte("%PDF-1.4\n")
	cabeceraGIF = []byte("GIF89a")
)

func cabeceraHEIC() []byte {
	b := make([]byte, 0, 16)
	b = append(b, 0x00, 0x00, 0x00, 0x18)
	b = append(b, []byte("ftyp")...)
	b = append(b, []byte("heic")...)
	b = append(b, []byte("0000")...)
	return b
}

// archivoDePrueba arma un multipart en memoria, igual que lo haría el navegador.
func archivoDePrueba(t *testing.T, nombre string, contenido []byte) (multipart.File, *multipart.FileHeader) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	parte, err := w.CreateFormFile("factura", nombre)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := parte.Write(contenido); err != nil {
		t.Fatalf("escribiendo contenido: %v", err)
	}
	w.Close()

	lector := multipart.NewReader(&buf, w.Boundary())
	form, err := lector.ReadForm(32 << 20)
	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })

	encabezado := form.File["factura"][0]
	archivo, err := encabezado.Open()
	if err != nil {
		t.Fatalf("abriendo parte: %v", err)
	}
	t.Cleanup(func() { _ = archivo.Close() })

	return archivo, encabezado
}

func TestGuardarAceptaLosFormatosPermitidos(t *testing.T) {
	casos := []struct {
		nombre    string
		archivo   string
		contenido []byte
		mime      string
		extension string
	}{
		{"png", "factura.png", cabeceraPNG, "image/png", ".png"},
		{"jpg", "foto.jpg", cabeceraJPG, "image/jpeg", ".jpg"},
		{"pdf", "recibo.pdf", cabeceraPDF, "application/pdf", ".pdf"},
		{"heic del iPhone", "IMG_0001.HEIC", cabeceraHEIC(), "image/heic", ".heic"},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			almacen, err := NuevoAlmacen(t.TempDir())
			if err != nil {
				t.Fatalf("NuevoAlmacen: %v", err)
			}

			archivo, encabezado := archivoDePrueba(t, c.archivo, c.contenido)
			guardado, err := almacen.Guardar(archivo, encabezado)
			if err != nil {
				t.Fatalf("Guardar(%s) falló: %v", c.archivo, err)
			}

			if guardado.Tipo != c.mime {
				t.Errorf("tipo detectado = %q, se esperaba %q", guardado.Tipo, c.mime)
			}
			if !strings.HasSuffix(guardado.Ruta, c.extension) {
				t.Errorf("ruta %q no termina en %q", guardado.Ruta, c.extension)
			}
			if guardado.Nombre != c.archivo {
				t.Errorf("nombre original = %q, se esperaba %q", guardado.Nombre, c.archivo)
			}
		})
	}
}

// LA prueba de seguridad del upload: el tipo se decide leyendo el archivo,
// no confiando en la extensión que manda el cliente.
func TestGuardarRechazaArchivoDisfrazado(t *testing.T) {
	almacen, err := NuevoAlmacen(t.TempDir())
	if err != nil {
		t.Fatalf("NuevoAlmacen: %v", err)
	}

	casos := []struct {
		nombre    string
		archivo   string
		contenido []byte
	}{
		{"texto plano con nombre .png", "malicioso.png", []byte("esto es texto, no una imagen")},
		{"script con nombre .pdf", "recibo.pdf", []byte("#!/bin/sh\nrm -rf /\n")},
		{"gif (no está permitido)", "animacion.gif", cabeceraGIF},
		{"html con nombre .jpg", "foto.jpg", []byte("<html><body>hola</body></html>")},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			archivo, encabezado := archivoDePrueba(t, c.archivo, c.contenido)

			if _, err := almacen.Guardar(archivo, encabezado); err == nil {
				t.Fatalf("SE ACEPTÓ un archivo disfrazado (%s). Es un hueco de seguridad.", c.archivo)
			}
		})
	}
}

func TestGuardarRechazaArchivoVacio(t *testing.T) {
	almacen, _ := NuevoAlmacen(t.TempDir())
	archivo, encabezado := archivoDePrueba(t, "vacio.png", nil)

	if _, err := almacen.Guardar(archivo, encabezado); err == nil {
		t.Error("un archivo vacío debería rechazarse")
	}
}

// El nombre en disco es aleatorio: dos archivos con el mismo nombre original
// no se pueden pisar entre ellos.
func TestGuardarNoPisaArchivos(t *testing.T) {
	raiz := t.TempDir()
	almacen, _ := NuevoAlmacen(raiz)

	vistas := map[string]bool{}
	for i := 0; i < 5; i++ {
		archivo, encabezado := archivoDePrueba(t, "factura.png", cabeceraPNG)
		guardado, err := almacen.Guardar(archivo, encabezado)
		if err != nil {
			t.Fatalf("Guardar: %v", err)
		}
		if vistas[guardado.Ruta] {
			t.Fatalf("se repitió la ruta %q: un archivo pisaría al otro", guardado.Ruta)
		}
		vistas[guardado.Ruta] = true
	}
}

// Un nombre con "../" no debe poder escribir fuera de la carpeta de uploads.
func TestSanearNombre(t *testing.T) {
	casos := map[string]string{
		"factura.pdf":              "factura.pdf",
		"../../etc/passwd":         "passwd",
		`..\..\windows\system.ini`: "system.ini",
		"con espacios.png":         "con espacios.png",
		`comillas".png`:            "comillas_.png",
		"":                         "factura",
		".":                        "factura",
		"..":                       "factura",
		"/":                        "factura",
	}

	for entrada, esperado := range casos {
		if obtenido := sanearNombre(entrada); obtenido != esperado {
			t.Errorf("sanearNombre(%q) = %q, se esperaba %q", entrada, obtenido, esperado)
		}
	}
}

func TestSanearNombreRecortaLargos(t *testing.T) {
	largo := strings.Repeat("a", 300) + ".pdf"
	obtenido := sanearNombre(largo)

	if len(obtenido) > 120 {
		t.Errorf("el nombre quedó de %d bytes; el tope son 120", len(obtenido))
	}
}

// rutaSegura es la última línea de defensa: aunque la base de datos tuviera
// una ruta manipulada, no debe poder salir de la carpeta de uploads.
func TestRutaSeguraBloqueaSalidas(t *testing.T) {
	raiz := t.TempDir()
	almacen, _ := NuevoAlmacen(raiz)

	peligrosas := []string{
		"../secreto.txt",
		"../../etc/passwd",
		"2026/../../../etc/passwd",
		"/etc/passwd",
		`..\..\windows\system.ini`,
	}

	for _, ruta := range peligrosas {
		if _, err := almacen.rutaSegura(ruta); err == nil {
			t.Errorf("rutaSegura(%q) no bloqueó una ruta fuera de la carpeta", ruta)
		}
	}
}

func TestRutaSeguraPermiteLasNormales(t *testing.T) {
	raiz := t.TempDir()
	almacen, _ := NuevoAlmacen(raiz)

	ruta, err := almacen.rutaSegura("2026/09/abc123.png")
	if err != nil {
		t.Fatalf("rutaSegura rechazó una ruta válida: %v", err)
	}

	raizAbs, _ := filepath.Abs(raiz)
	if !strings.HasPrefix(ruta, raizAbs) {
		t.Errorf("la ruta resuelta %q quedó fuera de %q", ruta, raizAbs)
	}
}

func TestEliminarEsIdempotente(t *testing.T) {
	almacen, _ := NuevoAlmacen(t.TempDir())

	// Borrar algo que no existe no es un error: el objetivo ya se cumplió.
	if err := almacen.Eliminar("2026/09/no-existe.png"); err != nil {
		t.Errorf("Eliminar sobre un archivo inexistente devolvió error: %v", err)
	}
}

func TestGuardarYLuegoAbrir(t *testing.T) {
	almacen, _ := NuevoAlmacen(t.TempDir())

	contenido := append(cabeceraPNG, []byte("datos de la imagen")...)
	archivo, encabezado := archivoDePrueba(t, "factura.png", contenido)

	guardado, err := almacen.Guardar(archivo, encabezado)
	if err != nil {
		t.Fatalf("Guardar: %v", err)
	}

	f, err := almacen.Abrir(guardado.Ruta)
	if err != nil {
		t.Fatalf("Abrir: %v", err)
	}
	defer f.Close()

	leido, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("leyendo el archivo: %v", err)
	}
	if !bytes.Equal(leido, contenido) {
		t.Error("el archivo guardado no coincide byte a byte con el original")
	}
}

func TestEsHEIC(t *testing.T) {
	if !esHEIC(cabeceraHEIC()) {
		t.Error("no se reconoció una cabecera HEIC válida")
	}
	for _, otro := range [][]byte{cabeceraPNG, cabeceraPDF, nil, []byte("corto")} {
		if esHEIC(otro) {
			t.Errorf("se marcó como HEIC algo que no lo es: %q", otro)
		}
	}
}

func TestValidadoresDeTipoYEstado(t *testing.T) {
	for _, t2 := range []string{TipoRecibi, TipoPague, TipoPreste} {
		if !EsTipoValido(t2) {
			t.Errorf("EsTipoValido(%q) = false", t2)
		}
	}
	for _, t2 := range []string{"", "RECIBI", "otro", "recibí"} {
		if EsTipoValido(t2) {
			t.Errorf("EsTipoValido(%q) = true, debería rechazarse", t2)
		}
	}

	for _, e := range []string{EstadoPendiente, EstadoPagado} {
		if !EsEstadoValido(e) {
			t.Errorf("EsEstadoValido(%q) = false", e)
		}
	}
	for _, e := range []string{"", "PAGADO", "pendiente ", "cancelado"} {
		if EsEstadoValido(e) {
			t.Errorf("EsEstadoValido(%q) = true, debería rechazarse", e)
		}
	}
}

func TestFechaValida(t *testing.T) {
	validas := []string{"2026-09-16", "2000-01-01", "2100-12-31"}
	for _, f := range validas {
		if !fechaValida(f) {
			t.Errorf("fechaValida(%q) = false", f)
		}
	}

	invalidas := []string{
		"", "16/09/2026", "2026-9-16", "2026-13-01", "2026-02-30",
		"1999-12-31", // año absurdo, casi siempre un dedazo
		"2101-01-01",
		"hoy",
	}
	for _, f := range invalidas {
		if fechaValida(f) {
			t.Errorf("fechaValida(%q) = true, debería rechazarse", f)
		}
	}
}
