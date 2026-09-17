package avisos_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"finanzas/internal/avisos"
	"finanzas/internal/movimientos"
	"finanzas/internal/suscripciones"
)

// prestarConCobro deja un préstamo pendiente de Juan con su fecha de cobro.
func (e *entorno) prestarConCobro(t *testing.T, monto, fecha, cobrarEl string) *movimientos.Movimiento {
	t.Helper()

	aQuien, estado := "Juan", movimientos.EstadoPendiente
	m, err := e.movimientos.Crear(context.Background(), e.ana, movimientos.Datos{
		CategoriaID: e.categoria,
		MedioPagoID: &e.efectivo,
		Tipo:        movimientos.TipoPreste,
		Monto:       monto,
		Fecha:       fecha,
		Descripcion: "para el arriendo",
		AQuien:      &aQuien,
		Estado:      &estado,
		CobrarEl:    &cobrarEl,
	})
	if err != nil {
		t.Fatalf("creando préstamo: %v", err)
	}
	return m
}

func (e *entorno) avisosDeCobro(t *testing.T) []avisos.Aviso {
	t.Helper()
	var lista []avisos.Aviso
	for _, a := range e.avisosDe(t, e.ana) {
		if a.Tipo == avisos.TipoCobroDelDia {
			lista = append(lista, a)
		}
	}
	return lista
}

// El día del cobro llega "Hoy te paga Juan", una sola vez aunque la tarea
// corra cada hora.
func TestElDiaDelCobroLlegaElAviso(t *testing.T) {
	e := nuevoEntorno(t)
	e.prestarConCobro(t, "200000", "2026-09-01", "2026-09-16")

	e.correr(t, miercoles) // 16 de septiembre
	e.correr(t, miercoles.Add(time.Hour))
	e.correr(t, miercoles.Add(5*time.Hour))

	lista := e.avisosDeCobro(t)
	if len(lista) != 1 {
		t.Fatalf("avisos de cobro = %d, se esperaba 1", len(lista))
	}
	if lista[0].Titulo != "Hoy te paga Juan" {
		t.Errorf("título = %q", lista[0].Titulo)
	}
	for _, debe := range []string{"$200.000", "1 de septiembre", "para el arriendo"} {
		if !strings.Contains(lista[0].Cuerpo, debe) {
			t.Errorf("el cuerpo no dice %q: %s", debe, lista[0].Cuerpo)
		}
	}
}

// Antes del día, nada: el aviso es para el día acordado.
func TestAntesDelDiaNoAvisa(t *testing.T) {
	e := nuevoEntorno(t)
	e.prestarConCobro(t, "200000", "2026-09-01", "2026-09-18")

	e.correr(t, miercoles)

	if lista := e.avisosDeCobro(t); len(lista) != 0 {
		t.Errorf("avisó antes de tiempo: %+v", lista)
	}
}

// El día se cuenta en hora de Colombia: el martes 15 a las 9 de la noche en
// Bogotá ya es miércoles 16 en UTC, y todavía no toca avisar lo del 16.
func TestElDiaEsElDeColombia(t *testing.T) {
	e := nuevoEntorno(t)
	e.prestarConCobro(t, "200000", "2026-09-01", "2026-09-16")

	martesDeNocheEnBogota := time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC) // 15 sep, 21:00 COT
	e.correr(t, martesDeNocheEnBogota)
	if lista := e.avisosDeCobro(t); len(lista) != 0 {
		t.Fatalf("avisó el día anterior: %+v", lista)
	}

	e.correr(t, martesDeNocheEnBogota.Add(4*time.Hour)) // 16 sep, 01:00 COT
	if lista := e.avisosDeCobro(t); len(lista) != 1 {
		t.Errorf("avisos = %d, se esperaba 1 ya el 16", len(lista))
	}
}

// Si el servidor estuvo apagado ese día, el aviso sale al volver, sin decir
// "hoy". Pasados unos días ya no: de eso se encarga el recordatorio mensual.
func TestSiSePasoElDiaAvisaAlVolver(t *testing.T) {
	e := nuevoEntorno(t)
	e.prestarConCobro(t, "200000", "2026-09-01", "2026-09-14")
	e.prestarConCobro(t, "70000", "2026-09-01", "2026-09-05")

	e.correr(t, miercoles)

	lista := e.avisosDeCobro(t)
	if len(lista) != 1 {
		t.Fatalf("avisos = %d, se esperaba solo el del 14: %+v", len(lista), lista)
	}
	if lista[0].Titulo != "Juan quedó de pagarte el 14 de septiembre" {
		t.Errorf("título = %q", lista[0].Titulo)
	}
	if !strings.Contains(lista[0].Cuerpo, "sigue pendiente") {
		t.Errorf("cuerpo = %s", lista[0].Cuerpo)
	}
}

// Ya pagado, no se molesta a nadie.
func TestPrestamoYaPagadoNoAvisa(t *testing.T) {
	e := nuevoEntorno(t)
	m := e.prestarConCobro(t, "200000", "2026-09-01", "2026-09-16")

	if _, err := e.movimientos.CambiarEstado(context.Background(), e.ana, m.ID, movimientos.EstadoPagado, &e.efectivo); err != nil {
		t.Fatalf("marcando pagado: %v", err)
	}
	e.correr(t, miercoles)

	if lista := e.avisosDeCobro(t); len(lista) != 0 {
		t.Errorf("avisó un préstamo ya pagado: %+v", lista)
	}
}

// Si cambian la fecha, el nuevo día también avisa.
func TestCambiarLaFechaVuelveAAvisar(t *testing.T) {
	e := nuevoEntorno(t)
	m := e.prestarConCobro(t, "200000", "2026-09-01", "2026-09-16")
	e.correr(t, miercoles)

	nueva := "2026-09-23"
	datos := movimientos.Datos{
		CategoriaID: e.categoria, MedioPagoID: &e.efectivo, Tipo: m.Tipo, Monto: m.Monto,
		Fecha: m.Fecha, AQuien: m.AQuien, Estado: m.Estado, CobrarEl: &nueva,
	}
	if _, err := e.movimientos.Actualizar(context.Background(), e.ana, m.ID, datos); err != nil {
		t.Fatalf("cambiando la fecha: %v", err)
	}
	e.correr(t, miercoles.AddDate(0, 0, 7))

	if lista := e.avisosDeCobro(t); len(lista) != 2 {
		t.Errorf("avisos = %d, se esperaban 2 (uno por fecha)", len(lista))
	}
}

// redactorContado cuenta cuántas veces se le pide un texto al modelo.
type redactorContado struct{ llamadas atomic.Int64 }

func (r *redactorContado) Redactar(_ context.Context, _, datos string) (string, error) {
	r.llamadas.Add(1)
	return datos, nil
}

// La tarea corre cada hora: al modelo solo se le pide el texto de los avisos
// nuevos, no el de los que ya existen.
func TestNoSeRedactaDosVecesElMismoAviso(t *testing.T) {
	e := nuevoEntorno(t)
	if _, err := e.pool.ExecContext(context.Background(), "UPDATE usuarios SET rol = 'admin' WHERE id = $1", e.ana); err != nil {
		t.Fatalf("dándole IA a Ana: %v", err)
	}

	redactor := &redactorContado{}
	generador := avisos.NuevoGenerador(e.store, suscripciones.NewStore(e.pool), redactor)

	e.prestarConCobro(t, "200000", "2026-09-10", "2026-09-16")

	for i := range 3 {
		if _, err := generador.Correr(context.Background(), miercoles.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("corriendo: %v", err)
		}
	}

	// Un aviso de cobro y un resumen semanal (el préstamo es de la semana
	// pasada): dos textos, no seis.
	if n := redactor.llamadas.Load(); n != 2 {
		t.Errorf("llamadas al modelo = %d, se esperaban 2", n)
	}
}
