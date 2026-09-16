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

	"finanzas/internal/agente"
	"finanzas/internal/avisos"
	"finanzas/internal/config"
	"finanzas/internal/db"
	"finanzas/internal/httpx"
	"finanzas/internal/movimientos"
	"finanzas/internal/registro"
	"finanzas/internal/suscripciones"
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

	// El chat solo existe si hay llave del modelo. Sin ella la app funciona
	// igual: no se monta la ruta ni se arranca su limpieza.
	var agenteStore *agente.Store
	var proveedor agente.Proveedor
	var redactor avisos.Redactor

	if cfg.LLM.Habilitado() {
		agenteStore = agente.NewStore(pool)
		proveedor = agente.NuevoProveedorHTTP(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Modelo, cfg.LLM.Timeout)
		// El mismo proveedor redacta los avisos automaticos. Es un adaptador,
		// no un cliente nuevo: una sola llave, un solo pool de conexiones.
		redactor = agente.NuevoRedactor(proveedor)
		slog.Info("agente conversacional activo", "modelo", cfg.LLM.Modelo, "limite_diario", cfg.LLM.LimiteDiario)
	} else {
		slog.Info("agente conversacional apagado: falta LLM_API_KEY")
	}

	// Los avisos NO dependen del modelo: sus cifras las calcula Postgres y el
	// texto base lo arma la app. El modelo, cuando esta, solo lo redacta mejor.
	avisosStore := avisos.NewStore(pool)
	generador := avisos.NuevoGenerador(avisosStore, suscripciones.NewStore(pool), redactor)

	if cfg.TokenMantenimiento == "" {
		slog.Warn("TOKEN_MANTENIMIENTO no configurado: la bitácora de errores solo se podrá leer con psql")
	}

	// El registro de errores se conecta ANTES de montar las rutas: desde aquí
	// en adelante, todo 500 y todo panic queda guardado en la base.
	registroStore := registro.NewStore(pool)
	httpx.UsarRegistrador(registro.NuevoAdaptador(registroStore))

	go limpiarErroresPeriodicamente(ctx, registroStore)
	go generarAvisosPeriodicamente(ctx, generador, avisosStore)
	if agenteStore != nil {
		go limpiarConsumoPeriodicamente(ctx, agenteStore)
	}

	router := nuevoRouter(dependencias{
		cfg:       cfg,
		pool:      pool,
		almacen:   almacen,
		registro:  registroStore,
		avisos:    avisosStore,
		agente:    agenteStore,
		proveedor: proveedor,
	})

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

// limpiarConsumoPeriodicamente borra las marcas de uso que ya salieron de la
// ventana del limite. Es una fila por mensaje: sin esto, la tabla crece para
// siempre guardando datos que nadie vuelve a consultar.
func limpiarConsumoPeriodicamente(ctx context.Context, store *agente.Store) {
	limpiar := func() {
		borrados, err := store.LimpiarConsumoViejo(ctx)
		if err != nil {
			slog.Error("no se pudo limpiar el consumo del agente", "error", err)
			return
		}
		if borrados > 0 {
			slog.Info("consumo del agente limpiado", "borrados", borrados)
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

// generarAvisosPeriodicamente arma los resumenes y recordatorios.
//
// Es una goroutine dentro del propio servidor y no un cron del sistema: una
// pieza menos que instalar, que configurar y que se puede olvidar al mover la
// app de maquina. Cada aviso lleva su clave de periodo, asi que correr cada
// pocas horas no significa repetirlos — y si el servidor estuvo apagado tres
// dias, al volver genera lo que falte sin llevar ninguna cuenta aparte.
func generarAvisosPeriodicamente(ctx context.Context, generador *avisos.Generador, store *avisos.Store) {
	trabajar := func() {
		creados, err := generador.Correr(ctx, time.Now())
		if err != nil {
			// Un fallo con un usuario no impide los avisos de los demas: el
			// generador los junta y los reporta todos aqui.
			slog.Error("fallos generando avisos", "error", err)
		}
		if creados > 0 {
			slog.Info("avisos generados", "cantidad", creados)
		}

		borrados, err := store.LimpiarViejos(ctx)
		if err != nil {
			slog.Error("no se pudieron limpiar los avisos viejos", "error", err)
			return
		}
		if borrados > 0 {
			slog.Info("avisos viejos limpiados", "borrados", borrados, "retencion_dias", avisos.RetencionDias)
		}
	}

	trabajar()

	// Cada 6 horas: el resumen semanal no tiene por que salir a las 3 de la
	// manana en punto, y con este intervalo el del lunes llega temprano.
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			trabajar()
		}
	}
}
