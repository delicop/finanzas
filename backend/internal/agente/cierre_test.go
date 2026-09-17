package agente

import (
	"context"
	"net/http/httptest"
	"testing"

	"finanzas/internal/httpx"
)

// Si el usuario cierra la pestaña justo cuando falla una escritura, el
// "deshacer" tiene que poder correr igual: si heredara la cancelación, la
// tarjeta quedaría resuelta sin que exista el movimiento.
func TestElCierreNoHeredaLaCancelacionDelNavegador(t *testing.T) {
	ctx, cancelar := context.WithCancel(httpx.ConUsuarioID(context.Background(), 7))
	cancelar() // el navegador ya se fue
	r := httptest.NewRequest("POST", "/propuestas/1/confirmar", nil).WithContext(ctx)

	cierre, liberar := contextoDeCierreConTope(r)
	defer liberar()

	if err := cierre.Err(); err != nil {
		t.Fatalf("el context de cierre nació cancelado: %v", err)
	}
	if _, tieneTope := cierre.Deadline(); !tieneTope {
		t.Error("el context de cierre no tiene tope de tiempo: una base colgada lo dejaría esperando")
	}
	// Conserva los valores del request: el usuario sigue siendo el mismo.
	if id, ok := httpx.UsuarioID(cierre); !ok || id != 7 {
		t.Errorf("se perdió el usuario del request: id=%d ok=%v", id, ok)
	}
}
