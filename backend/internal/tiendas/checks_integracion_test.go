package tiendas_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"finanzas/internal/tiendas"
)

// Las reglas de la hoja que viven en un CHECK de la base, y no solo en Go.
// Aqui se escribe por el pool, directo, como lo haria un INSERT a mano por
// psql: si Go fuera el unico que las cuida, estas pruebas lo dirian.

// violaCheck dice si err es justo el CHECK nombrado. Mirar solo que haya
// error no alcanza: una llave foranea o un UNIQUE tambien fallan, y la prueba
// pasaria sin que el CHECK existiera.
func violaCheck(err error, nombre string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == nombre
}

// Los grupos validos estan escritos dos veces: en tiendas.Grupos y en el CHECK
// de la migracion 25. Si se separan, la app acepta un grupo que la base
// rechaza (500) o al contrario (un renglon que no se puede escribir). Un POST
// con un grupo inventado solo probaria la mitad de Go.
func TestLosGruposDeGoYLosDeLaBaseSonLosMismos(t *testing.T) {
	e := nuevoEntorno(t)

	var definicion string
	err := e.pool.QueryRowContext(context.Background(), `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname = 'cierre_lineas_grupo_valido'`).Scan(&definicion)
	if err != nil {
		t.Fatalf("leyendo el CHECK de los grupos: %v", err)
	}

	// Postgres lo devuelve como grupo = ANY (ARRAY['pago_nequi'::text, ...]):
	// cada grupo es un literal entre comillas simples.
	var enLaBase []string
	for _, m := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(definicion, -1) {
		enLaBase = append(enLaBase, m[1])
	}
	if len(enLaBase) == 0 {
		t.Fatalf("no se encontro ningun grupo en %q", definicion)
	}

	enGo := slices.Clone(tiendas.Grupos)
	slices.Sort(enLaBase)
	slices.Sort(enGo)
	if !slices.Equal(enLaBase, enGo) {
		t.Errorf("los grupos no coinciden:\n  base: %v\n  Go:   %v", enLaBase, enGo)
	}
	for _, g := range enLaBase {
		if !tiendas.GrupoValido(g) {
			t.Errorf("la base acepta %q y GrupoValido no", g)
		}
	}
}

// Ninguna casilla de la hoja puede ser negativa. La migracion 26 recreo este
// CHECK a mano despues de soltar saldo_nequi, que se lo llevaba puesto: se
// prueba cada columna por separado para que perder una no pase en silencio.
func TestLaBaseRechazaMontosNegativosEnElCierre(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	ctx := context.Background()

	// Primero uno bueno por el mismo camino: si este fallara, los de abajo
	// fallarian por cualquier otra cosa y la prueba no diria nada.
	_, err := e.pool.ExecContext(ctx, `
		INSERT INTO cierres (usuario_id, tienda_id, fecha) VALUES ($1, $2, '2026-09-20')`, ana, tienda)
	if err != nil {
		t.Fatalf("insertando un cierre valido: %v", err)
	}

	columnas := []string{
		"qr_banco", "qr_tienda",
		"datafono_reporte", "datafono_tienda",
		"efectivo_billete", "efectivo_moneda", "efectivo_tienda",
		"venta_tienda",
	}
	for _, col := range columnas {
		// col sale de la lista de arriba, no de afuera: el Sprintf es seguro.
		_, err := e.pool.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO cierres (usuario_id, tienda_id, fecha, %s)
			VALUES ($1, $2, '2026-09-21', -1)`, col), ana, tienda)
		if !violaCheck(err, "cierres_montos_no_negativos") {
			t.Errorf("%s = -1: se esperaba cierres_montos_no_negativos, llego %v", col, err)
		}
	}
}

// Los renglones de las listas: sin monto negativo, sin descripcion en blanco
// y sin un grupo que la hoja no tiene.
func TestLaBaseRechazaRenglonesInvalidos(t *testing.T) {
	e := nuevoEntorno(t)
	ana := e.crearCliente(t)
	e.conPlan(t, ana, true)
	tienda := e.crearTienda(t, ana, "centro")
	c := e.crearCierre(t, ana, tienda, hojaDeUnDia)
	ctx := context.Background()

	insertar := func(grupo, descripcion, monto string) error {
		_, err := e.pool.ExecContext(ctx, `
			INSERT INTO cierre_lineas (cierre_id, grupo, descripcion, monto)
			VALUES ($1, $2, $3, $4)`, c.ID, grupo, descripcion, monto)
		return err
	}

	// El renglon bueno entra: los malos fallan por su CHECK y no por otra cosa.
	if err := insertar(tiendas.GrupoGasto, "Hielo", "5000"); err != nil {
		t.Fatalf("insertando un renglon valido: %v", err)
	}

	casos := []struct {
		que                       string
		grupo, descripcion, monto string
		check                     string
	}{
		{"monto negativo", tiendas.GrupoGasto, "Hielo", "-1", "cierre_lineas_monto_no_negativo"},
		{"descripcion en blanco", tiendas.GrupoGasto, "   ", "5000", "cierre_lineas_descripcion_no_vacia"},
		{"grupo inventado", "inventado", "Hielo", "5000", "cierre_lineas_grupo_valido"},
	}
	for _, caso := range casos {
		if err := insertar(caso.grupo, caso.descripcion, caso.monto); !violaCheck(err, caso.check) {
			t.Errorf("%s: se esperaba %s, llego %v", caso.que, caso.check, err)
		}
	}
}
