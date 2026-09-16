// Package db abre la conexion a Postgres y corre las migraciones.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// Driver de Postgres registrado como "pgx" para database/sql.
	// Usamos pgx (no lib/pq) porque es el driver mantenido hoy en dia.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open abre el pool de conexiones y verifica que Postgres responda.
//
// sql.Open NO conecta: solo valida la URL y prepara el pool. Por eso hacemos
// PingContext con reintentos: dentro de Docker el contenedor de la API puede
// arrancar medio segundo antes de que Postgres acepte conexiones.
func Open(ctx context.Context, url string) (*sql.DB, error) {
	pool, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("abriendo conexion: %w", err)
	}

	// Valores conservadores pensados en la Raspberry Pi: Postgres por defecto
	// acepta 100 conexiones, y cada conexion ociosa consume memoria.
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(5 * time.Minute)

	var lastErr error
	for intento := 1; intento <= 10; intento++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = pool.PingContext(pingCtx)
		cancel()

		if lastErr == nil {
			return pool, nil
		}

		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	pool.Close()
	return nil, fmt.Errorf("no se pudo conectar a Postgres: %w", lastErr)
}
