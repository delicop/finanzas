package main

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"finanzas/internal/auth"
	"finanzas/internal/categorias"
	"finanzas/internal/config"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
	"finanzas/internal/registro"
)

// nuevoRouter arma todas las rutas y middlewares de la API.
//
// Usamos chi en vez del ServeMux estandar por dos cosas que vamos a necesitar
// ya: parametros en la URL (/categorias/{id}) y grupos de rutas con
// middlewares distintos (publicas vs. protegidas por JWT).
func nuevoRouter(cfg *config.Config, pool *sql.DB, almacen *movimientos.AlmacenFacturas, registroStore *registro.Store) http.Handler {
	r := chi.NewRouter()

	// --- Middlewares globales (se aplican a TODA request, en este orden) ---
	r.Use(middleware.RequestID) // un id unico por request, util en logs
	r.Use(middleware.RealIP)    // resuelve la IP real detras del proxy
	r.Use(middleware.Logger)    // loguea metodo, ruta, status y duracion
	// Recuperador (nuestro, en vez del de chi): además de evitar que un panic
	// tumbe el servidor, lo guarda en la base con su traza para poder buscarlo.
	r.Use(registro.Recuperador)
	r.Use(middleware.Timeout(30 * time.Second))

	// --- CORS ---
	// El navegador bloquea que http://localhost:5173 (React) llame a
	// http://localhost:8080 (Go) salvo que el backend responda con las
	// cabeceras correctas. Eso es exactamente lo que hace este middleware.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSOrigins, // lista explicita, NUNCA "*" con auth
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders: []string{"Content-Disposition"}, // para descargar facturas
		// false porque el token viaja en el header Authorization, no en cookies.
		AllowCredentials: false,
		MaxAge:           300, // el navegador cachea el preflight OPTIONS 5 min
	}))

	// --- Dependencias ---
	// Se construyen una sola vez aqui y se inyectan hacia abajo.
	// Nada de variables globales: asi cada pieza es testeable por separado.
	authStore := auth.NewStore(pool)
	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTExpiry)
	authHandler := auth.NewHandler(authStore, tokens)

	categoriasHandler := categorias.NewHandler(categorias.NewStore(pool))
	mediosHandler := medios.NewHandler(medios.NewStore(pool))
	movimientosHandler := movimientos.NewHandler(movimientos.NewStore(pool), almacen)

	// --- Rutas ---
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		// Endpoint sin auth para que Docker/monitoreo sepa si la API vive.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api", func(api chi.Router) {
		// Mantenimiento: la bitácora de errores, para revisarla desde afuera.
		// Va con su propio token (no con el login del cliente) y solo existe
		// si TOKEN_MANTENIMIENTO está configurado.
		if cfg.TokenMantenimiento != "" {
			api.Mount("/mantenimiento", registro.NewHandler(registroStore, cfg.TokenMantenimiento).Rutas())
		}

		// Publicas: login (y el preflight de CORS, que resuelve el middleware).
		api.Mount("/auth", authHandler.Rutas())

		// Privadas: un solo Group con el middleware de auth aplicado una vez.
		// Si manana agregas un endpoint dentro de este bloque, queda protegido
		// automaticamente; no hay forma de "olvidar" ponerle el middleware.
		api.Group(func(priv chi.Router) {
			priv.Use(auth.RequireAuth(tokens))

			priv.Mount("/categorias", categoriasHandler.Rutas())
			priv.Mount("/medios-pago", mediosHandler.Rutas())
			priv.Mount("/movimientos", movimientosHandler.Rutas())
			priv.Get("/dashboard", movimientosHandler.Dashboard)
		})
	})

	return r
}
