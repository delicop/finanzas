package db

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

// Los archivos .sql quedan EMBEBIDOS dentro del binario de Go.
//
// Por que importa: la imagen Docker final no lleva ni el codigo fuente ni el
// CLI de goose. Al arrancar, la API trae sus propias migraciones adentro y las
// aplica sola. Desplegar en la Raspberry = "docker compose up -d", nada mas.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate aplica todas las migraciones pendientes.
//
// goose guarda en la tabla goose_db_version que version ya corrio, asi que
// llamar esto en cada arranque es seguro e idempotente.
func Migrate(pool *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("configurando goose: %w", err)
	}
	if err := goose.Up(pool, "migrations"); err != nil {
		return fmt.Errorf("aplicando migraciones: %w", err)
	}
	return nil
}
