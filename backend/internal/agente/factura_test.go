package agente_test

import (
	"net/http"
	"strings"
	"testing"

	"finanzas/internal/agente"
)

// ultimoResultado es lo último que le devolvió una herramienta al modelo.
func (e *entorno) ultimoResultado(t *testing.T) string {
	t.Helper()
	for i := len(e.proveedor.pasos) - 1; i >= 0; i-- {
		if e.proveedor.pasos[i].Rol == agente.RolHerramienta {
			return e.proveedor.pasos[i].Contenido
		}
	}
	t.Fatal("el modelo no recibió ningún resultado de herramienta")
	return ""
}

// Al preparar un gasto, el modelo recibe la instrucción de pedir la foto o el
// PDF de la factura, que el usuario adjunta en la tarjeta.
func TestAlPrepararUnGastoPideLaFactura(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.proponerAlmuerzo(t)

	if resultado := e.ultimoResultado(t); !strings.Contains(resultado, "factura") {
		t.Errorf("no se le indicó pedir la factura:\n%s", resultado)
	}
}

// A un préstamo no se le pide factura: no hay comercio que la emita.
func TestAUnPrestamoNoSeLePideFactura(t *testing.T) {
	e := nuevoEntorno(t, 10)
	e.proveedor.guion = []agente.Respuesta{
		pideHerramienta(agente.HerramientaProponerMovimiento,
			`{"tipo":"preste","monto":"50000","fecha":"2026-09-16","categoria":"Negocio","medio_pago":"Efectivo","a_quien":"Juan","estado":"pendiente"}`),
		{Contenido: "Te preparé el préstamo."},
	}
	if res := e.enviar(t, e.ana, "le presté 50 mil a Juan"); res.Code != http.StatusOK {
		t.Fatalf("enviar: %d — %s", res.Code, res.Body.String())
	}

	if resultado := e.ultimoResultado(t); strings.Contains(resultado, "factura") {
		t.Errorf("a un préstamo se le pidió factura:\n%s", resultado)
	}
}
