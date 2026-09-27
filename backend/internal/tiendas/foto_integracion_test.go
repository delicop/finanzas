package tiendas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"finanzas/internal/httpx"
	"finanzas/internal/tiendas"
)

// almacenTemporal guarda los archivos en una carpeta de prueba. Es lo mismo
// que hace el almacen de verdad, sin sus reglas de tipo y tamaño: lo que se
// prueba aqui es que la foto quede pegada al cierre correcto y que no se
// pueda leer desde otra cuenta, no la deteccion de mime.
type almacenTemporal struct{ raiz string }

func (a almacenTemporal) Guardar(archivo multipart.File, encabezado *multipart.FileHeader) (tiendas.ArchivoSubido, error) {
	datos, err := io.ReadAll(archivo)
	if err != nil {
		return tiendas.ArchivoSubido{}, err
	}
	if len(datos) == 0 {
		return tiendas.ArchivoSubido{}, tiendas.ErrFotoVacia
	}
	nombre := fmt.Sprintf("%x.bin", len(datos))
	if err := os.WriteFile(filepath.Join(a.raiz, nombre), datos, 0o600); err != nil {
		return tiendas.ArchivoSubido{}, err
	}
	return tiendas.ArchivoSubido{Ruta: nombre, Nombre: encabezado.Filename, Tipo: "image/jpeg"}, nil
}

func (a almacenTemporal) Abrir(ruta string) (*os.File, error) {
	return os.Open(filepath.Join(a.raiz, ruta))
}

func (a almacenTemporal) Eliminar(ruta string) error {
	err := os.Remove(filepath.Join(a.raiz, ruta))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// subirFoto manda el archivo como lo mandaria el formulario: multipart.
func (e *entorno) subirFoto(t *testing.T, usuarioID int64, ruta, nombre string, contenido []byte) *httptest.ResponseRecorder {
	t.Helper()

	var cuerpo bytes.Buffer
	escritor := multipart.NewWriter(&cuerpo)
	parte, err := escritor.CreateFormFile("foto", nombre)
	if err != nil {
		t.Fatalf("armando el multipart: %v", err)
	}
	if _, err := parte.Write(contenido); err != nil {
		t.Fatalf("escribiendo el archivo: %v", err)
	}
	escritor.Close()

	req := httptest.NewRequest("POST", ruta, &cuerpo)
	req.Header.Set("Content-Type", escritor.FormDataContentType())
	req = req.WithContext(httpx.ConUsuarioID(req.Context(), usuarioID))

	w := httptest.NewRecorder()
	e.rutas.ServeHTTP(w, req)
	return w
}

// La hoja firmada queda pegada a su cierre, se puede volver a ver y se puede
// quitar. Es el papel que vale: sin el, una cifra rara meses despues no se
// puede contrastar con nada.
func TestLaFotoDelCierreSeSubeSeVeYSeQuita(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	c := e.crearCierre(t, ana, tienda, hojaDeUnDia)

	ruta := fmt.Sprintf("/%d/cierres/%d/foto", tienda, c.ID)
	contenido := []byte("una foto de la hoja")

	w := e.subirFoto(t, ana, ruta, "cierre-20.jpg", contenido)
	if w.Code != http.StatusOK {
		t.Fatalf("subiendo la foto: status = %d, cuerpo %s", w.Code, w.Body)
	}

	// La respuesta ya trae el cierre con su foto: la pantalla no tiene que
	// volver a preguntar para saber que quedo.
	var conFoto tiendas.Cierre
	if err := json.Unmarshal(w.Body.Bytes(), &conFoto); err != nil {
		t.Fatalf("leyendo el cierre: %v", err)
	}
	if conFoto.Foto == nil {
		t.Fatal("el cierre volvio sin foto despues de subirla")
	}
	if conFoto.Foto.Nombre != "cierre-20.jpg" {
		t.Errorf("nombre de la foto = %q", conFoto.Foto.Nombre)
	}
	// La ruta en disco no se expone: solo el endpoint por donde se baja.
	esperada := fmt.Sprintf("/api/tiendas/%d/cierres/%d/foto", tienda, c.ID)
	if conFoto.Foto.URL != esperada {
		t.Errorf("url de la foto = %q, se esperaba %q", conFoto.Foto.URL, esperada)
	}

	w = e.pedir(t, ana, "GET", ruta, "")
	if w.Code != http.StatusOK {
		t.Fatalf("viendo la foto: status = %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), contenido) {
		t.Error("la foto que vuelve no es la que se subio")
	}

	if w := e.pedir(t, ana, "DELETE", ruta, ""); w.Code != http.StatusNoContent {
		t.Fatalf("quitando la foto: status = %d", w.Code)
	}
	if w := e.pedir(t, ana, "GET", ruta, ""); w.Code != http.StatusNotFound {
		t.Errorf("despues de quitarla: status = %d, se esperaba 404", w.Code)
	}
}

// La foto de la hoja de otro tampoco se ve: es el mismo filtro por usuario
// que cuida el resto del cierre.
func TestLaFotoDeOtroNoSeVe(t *testing.T) {
	e := nuevoEntorno(t)

	ana := e.crearCliente(t)
	beto := e.crearCliente(t)
	e.conPlan(t, ana, true)
	e.conPlan(t, beto, true)

	tienda := e.crearTienda(t, ana, "centro")
	c := e.crearCierre(t, ana, tienda, hojaDeUnDia)
	ruta := fmt.Sprintf("/%d/cierres/%d/foto", tienda, c.ID)

	if w := e.subirFoto(t, ana, ruta, "hoja.jpg", []byte("mia")); w.Code != http.StatusOK {
		t.Fatalf("subiendo la foto: status = %d, cuerpo %s", w.Code, w.Body)
	}

	if w := e.pedir(t, beto, "GET", ruta, ""); w.Code != http.StatusNotFound {
		t.Errorf("Beto viendo la foto de Ana: status = %d, se esperaba 404", w.Code)
	}
	if w := e.subirFoto(t, beto, ruta, "suya.jpg", []byte("pisada")); w.Code != http.StatusNotFound {
		t.Errorf("Beto pisando la foto de Ana: status = %d, se esperaba 404", w.Code)
	}
	if w := e.pedir(t, beto, "DELETE", ruta, ""); w.Code != http.StatusNotFound {
		t.Errorf("Beto quitando la foto de Ana: status = %d, se esperaba 404", w.Code)
	}

	// La de Ana sigue ahi, intacta.
	w := e.pedir(t, ana, "GET", ruta, "")
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), []byte("mia")) {
		t.Errorf("la foto de Ana quedo en status %d, cuerpo %q", w.Code, w.Body)
	}
}

// Al borrar la hoja se va tambien su foto del disco. Ninguna llave foranea
// llega hasta alla: si el borrado no la arrastra, el archivo se queda para
// siempre y ya no queda fila que diga de quien era.
func TestBorrarElCierreSeLlevaSuFoto(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	c := e.crearCierre(t, ana, tienda, hojaDeUnDia)

	rutaFoto := fmt.Sprintf("/%d/cierres/%d/foto", tienda, c.ID)
	if w := e.subirFoto(t, ana, rutaFoto, "hoja.jpg", []byte("la hoja firmada")); w.Code != http.StatusOK {
		t.Fatalf("subiendo la foto: status = %d, cuerpo %s", w.Code, w.Body)
	}

	// El nombre que le puso el almacen: es lo que hay que buscar en el disco,
	// porque la ruta no se expone por la API.
	var ruta string
	err := e.pool.QueryRowContext(context.Background(),
		"SELECT foto_ruta FROM cierres WHERE id = $1", c.ID).Scan(&ruta)
	if err != nil {
		t.Fatalf("leyendo la ruta de la foto: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.almacen.raiz, ruta)); err != nil {
		t.Fatalf("la foto no quedo en el disco: %v", err)
	}

	if w := e.pedir(t, ana, "DELETE", fmt.Sprintf("/%d/cierres/%d", tienda, c.ID), ""); w.Code != http.StatusNoContent {
		t.Fatalf("borrando el cierre: status = %d, cuerpo %s", w.Code, w.Body)
	}

	if _, err := os.Stat(filepath.Join(e.almacen.raiz, ruta)); !os.IsNotExist(err) {
		t.Errorf("la foto sigue en el disco despues de borrar el cierre (err = %v)", err)
	}
}
