package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"finanzas/internal/config"
	"finanzas/internal/db"
	"finanzas/internal/httpx"
	"finanzas/internal/movimientos"
	"finanzas/internal/registro"
)

func main() {
	// slog es el logger estructurado de la libreria estandar (Go 1.21+).
	// En JSON queda listo para leerlo con `docker compose logs`.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("la aplicacion no pudo arrancar", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Este context se cancela con Ctrl+C o cuando Docker manda SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("conectado a Postgres")

	// Las migraciones corren en cada arranque, antes de aceptar trafico.
	if err := db.Migrate(pool); err != nil {
		return err
	}
	slog.Info("migraciones al dia")

	// El almacen de facturas crea la carpeta /uploads si no existe.
	almacen, err := movimientos.NuevoAlmacen(cfg.UploadsDir)
	if err != nil {
		return err
	}
	slog.Info("carpeta de facturas lista", "ruta", cfg.UploadsDir)

	if cfg.TokenMantenimiento == "" {
		slog.Warn("TOKEN_MANTENIMIENTO no configurado: la bitácora de errores solo se podrá leer con psql")
	}

	// El registro de errores se conecta ANTES de montar las rutas: desde aquí
	// en adelante, todo 500 y todo panic queda guardado en la base.
	registroStore := registro.NewStore(pool)
	httpx.UsarRegistrador(registro.NuevoAdaptador(registroStore))

	go limpiarErroresPeriodicamente(ctx, registroStore)

	router := nuevoRouter(cfg, pool, almacen, registroStore)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
		// Timeouts obligatorios: sin ellos una conexion lenta (o maliciosa)
		// puede quedarse abierta para siempre consumiendo memoria.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // amplio: luego subiremos facturas
		IdleTimeout:       90 * time.Second,
	}

	// El servidor corre en su propia goroutine para que main pueda quedarse
	// esperando la senal de apagado.
	errCh := make(chan error, 1)
	go func() {
		slog.Info("servidor escuchando", "puerto", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("apagando servidor...")
	}

	// Apagado ordenado: dejamos hasta 15s a los requests en curso para terminar
	// en vez de cortarlos a mitad de camino.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("servidor apagado limpiamente")
	return nil
}

// limpiarErroresPeriodicamente borra la bitácora vieja para que no crezca sin
// control. En una Raspberry el disco es chico y una falla en bucle podría
// llenarlo en cuestión de horas.
func limpiarErroresPeriodicamente(ctx context.Context, store *registro.Store) {
	limpiar := func() {
		borrados, err := store.LimpiarViejos(ctx)
		if err != nil {
			slog.Error("no se pudo limpiar la bitácora de errores", "error", err)
			return
		}
		if borrados > 0 {
			slog.Info("bitácora de errores limpiada", "borrados", borrados, "retencion_dias", registro.RetencionDias)
		}
	}

	limpiar()

	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			limpiar()
		}
	}
}
