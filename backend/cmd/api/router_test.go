package main

import (
	"testing"

	"finanzas/internal/movimientos"
	"finanzas/internal/tiendas"
)

// El almacen de facturas es el que de verdad decide que formato se acepta
// (foto.go:ArchivoSubido); tiendas.ErrFotoTipo es su propio texto, para no
// decirle "factura" al cliente de un cierre. Si un dia se acepta un formato
// nuevo y solo se actualiza uno de los dos mensajes, este test lo agarra.
func TestMensajesDeTipoDeArchivoCoinciden(t *testing.T) {
	if movimientos.ErrFacturaTipo.Error() != tiendas.ErrFotoTipo.Error() {
		t.Errorf("los mensajes no coinciden:\nfacturas: %q\nfotos:    %q",
			movimientos.ErrFacturaTipo.Error(), tiendas.ErrFotoTipo.Error())
	}
}
