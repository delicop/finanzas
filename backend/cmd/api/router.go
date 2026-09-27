package main

import (
	"database/sql"
	"errors"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"finanzas/internal/admin"
	"finanzas/internal/agente"
	"finanzas/internal/auth"
	"finanzas/internal/avisos"
	"finanzas/internal/categorias"
	"finanzas/internal/config"
	"finanzas/internal/medios"
	"finanzas/internal/movimientos"
	"finanzas/internal/push"
	"finanzas/internal/recurrentes"
	"finanzas/internal/registro"
	"finanzas/internal/suscripciones"
	"finanzas/internal/tiendas"
)

// nuevoRouter arma todas las rutas y middlewares de la API.
//
// Usamos chi en vez del ServeMux estandar por dos cosas que vamos a necesitar
// ya: parametros en la URL (/categorias/{id}) y grupos de rutas con
// middlewares distintos (publicas vs. protegidas por JWT).
// dependencias son las piezas que main construye y este archivo solo conecta.
//
// Van en una struct y no como siete parametros sueltos porque varias las
// necesita tambien main para sus tareas de fondo (limpiar la bitacora, generar
// avisos), y con una lista posicional larga es cuestion de tiempo que dos
// punteros del mismo tipo se crucen sin que el compilador diga nada.
type dependencias struct {
	cfg     *config.Config
	pool    *sql.DB
	almacen *movimientos.AlmacenFacturas

	registro *registro.Store
	avisos   *avisos.Store

	// agente y proveedor son nil cuando no hay modelo configurado: entonces
	// el chat no se monta y la app funciona igual, sin asistente.
	agente    *agente.Store
	proveedor agente.Proveedor

	// push existe siempre (para poder borrar suscripciones viejas), pero
	// enviadorPush es nil sin llaves VAPID y entonces las rutas de push no se
	// montan: el navegador pregunta, recibe 404 y no ofrece activarlas.
	push         *push.Store
	enviadorPush *push.Enviador

	recurrentes *recurrentes.Store
}

func nuevoRouter(d dependencias) http.Handler {
	cfg, pool := d.cfg, d.pool
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
		// La lista es explícita: un header que no esté aquí lo bloquea el
		// navegador en el preflight, aunque el backend lo entienda
		// perfectamente. Por eso X-Ver-Como tiene que estar nombrado — con
		// curl funciona sin él, que es justo lo que hace el fallo difícil de
		// ver: el panel de administración pediría datos y el navegador ni
		// siquiera mandaría la petición.
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", auth.CabeceraVerComo},
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

	mediosStore := medios.NewStore(pool)
	categoriasStore := categorias.NewStore(pool)
	movimientosStore := movimientos.NewStore(pool)

	categoriasHandler := categorias.NewHandler(categoriasStore)
	mediosHandler := medios.NewHandler(mediosStore)
	movimientosHandler := movimientos.NewHandler(movimientosStore, d.almacen)
	// Los recurrentes escriben movimientos al confirmarse, y lo hacen por el
	// MISMO store que el formulario: no tienen una puerta propia a la tabla
	// del dinero.
	recurrentesHandler := recurrentes.NewHandler(d.recurrentes, movimientosStore)
	tiendasStore := tiendas.NewStore(pool)
	// Al borrar una cuenta hay que sacar del disco tambien las fotos de sus
	// cierres: el CASCADE se lleva las filas, pero ninguna llave foranea llega
	// al disco. La consulta la hace tiendas sobre sus propias tablas; aqui,
	// que es el unico sitio que conoce a los dos paquetes, se conectan.
	adminHandler := admin.NewHandler(admin.NewStore(pool), authStore, mediosStore, d.almacen,
		tiendasStore.FotosDe)
	// El chat con el asistente. Se arma solo si hay llave del modelo: sin
	// ella la ruta no se monta y la app funciona exactamente igual, sin chat.
	// Mismo criterio que el token de mantenimiento.
	var agenteHandler *agente.Handler
	if d.agente != nil {
		// El catalogo reusa los MISMOS stores que los endpoints: las
		// herramientas no tienen una puerta propia a la base, y cualquier
		// filtro que proteja la API protege tambien al agente.
		catalogo := agente.NuevoCatalogo(movimientosStore, categoriasStore, mediosStore).
			// Y los recurrentes, con el MISMO confirmador del boton "Lo
			// pague" del resumen: decirle al chat "ya pague el internet" y
			// darle al boton tienen que terminar en el mismo movimiento, no
			// en dos.
			ConRecurrentes(d.recurrentes, recurrentes.NuevoConfirmador(d.recurrentes, movimientosStore))
		// El permiso es el plan del cliente: sin IA en el plan, no hay chat.
		agenteHandler = agente.NewHandler(d.agente, d.proveedor, catalogo, cfg.LLM.LimiteDiario, authStore.TieneIA)
	}

	// Las tiendas son una seccion que se vende: el handler recibe el permiso
	// (el plan del cliente) igual que el del asistente, y sin el no deja pasar
	// a nadie.
	tiendasHandler := tiendas.NewHandler(tiendasStore, authStore.TieneTiendas,
		fotosDeCierres{d.almacen})

	avisosHandler := avisos.NewHandler(d.avisos)

	// El lado negocio: planes, cobros y el tablero del dueno. Va bajo /admin
	// porque es lo mismo que administrar el servidor, no una seccion aparte.
	negocioHandler := suscripciones.NewHandler(suscripciones.NewStore(pool))

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
			api.Mount("/mantenimiento/errores", registro.NewHandler(d.registro, cfg.TokenMantenimiento).Rutas())
		}

		// Publicas: login (y el preflight de CORS, que resuelve el middleware).
		api.Mount("/auth", authHandler.Rutas())

		// Privadas: un solo Group con el middleware de auth aplicado una vez.
		// Si manana agregas un endpoint dentro de este bloque, queda protegido
		// automaticamente; no hay forma de "olvidar" ponerle el middleware.
		api.Group(func(priv chi.Router) {
			priv.Use(auth.RequireAuth(tokens, authStore))

			// VerComo va JUSTO despues de la autenticacion y antes de todo lo
			// demas: si el admin mando la cabecera X-Ver-Como, cambia el id
			// del context y los handlers de abajo responden con los datos del
			// usuario observado sin enterarse de nada. Solo lectura, y solo
			// para un admin: lo verifica el propio middleware.
			priv.Use(auth.VerComo(authStore))

			priv.Mount("/categorias", categoriasHandler.Rutas())
			priv.Mount("/medios-pago", mediosHandler.Rutas())
			priv.Mount("/movimientos", movimientosHandler.Rutas())
			priv.Mount("/recurrentes", recurrentesHandler.Rutas())
			// Existe para todos, pero solo responde a quien tenga las tiendas
			// en su plan: lo revisa el propio handler en cada peticion.
			priv.Mount("/tiendas", tiendasHandler.Rutas())
			// Los cierres de todas las tiendas juntos, que es como se leen
			// en la app. Crear uno sigue yendo por su tienda.
			priv.Mount("/cierres", tiendasHandler.RutasTodosLosCierres())
			priv.Get("/dashboard", movimientosHandler.Dashboard)

			// Las notificaciones al celular. Sin llaves VAPID la ruta no
			// existe, y la app sencillamente no ofrece activarlas.
			if d.enviadorPush != nil {
				priv.Mount("/push", push.NewHandler(d.push, d.enviadorPush).Rutas())
			}

			// Va dentro del grupo privado como todo lo demas. El propio
			// handler rechaza ademas las peticiones en modo "ver como": el
			// chat de alguien no es un dato de negocio que el admin revise.
			if agenteHandler != nil {
				priv.Mount("/agente", agenteHandler.Rutas())
			}

			// Los avisos existen siempre: no dependen del modelo. Sin el
			// salen con el texto que arma la app, que es el que lleva las
			// cifras de todos modos.
			priv.Mount("/notificaciones", avisosHandler.Rutas())

			// Administracion: un solo Group con RequireAdmin puesto una vez.
			// Igual que arriba con el token, aqui no hay forma de agregar una
			// ruta de admin y olvidarse de protegerla.
			priv.Group(func(adm chi.Router) {
				adm.Use(auth.RequireAdmin)

				// Dentro va tambien la bitacora de errores (/api/admin/errores).
				// Sigue existiendo /api/mantenimiento/errores con su token
				// propio: eso es para revisar el servidor por curl sin entrar
				// a la app ni pedirle la clave a nadie.
				// Toda la superficie de administracion, en un solo bloque:
				// lo que no este aqui, no existe para el panel.
				adm.Route("/admin", func(a chi.Router) {
					a.Mount("/usuarios", adminHandler.Rutas())
					a.Mount("/planes", negocioHandler.RutasPlanes())
					a.Mount("/pagos", negocioHandler.RutasPagos())
					a.Get("/negocio", negocioHandler.Negocio)

					// La bitacora de errores. Sigue existiendo aparte en
					// /api/mantenimiento/errores con su token propio: eso es
					// para revisar el servidor por curl sin entrar a la app.
					a.Mount("/errores", registro.NewHandlerSesion(d.registro).Rutas())
				})
			})
		})
	})

	return r
}

// fotosDeCierres conecta el almacen de las facturas con las fotos de los
// cierres: son el mismo problema y comparten carpeta, pero cada paquete habla
// de lo suyo. La traduccion vive aqui, que es el unico sitio que conoce a los
// dos, y no dentro de ninguno de ellos.
//
// Tambien traduce los errores: el cliente de un cierre no tiene por que leer
// que "la factura" es muy grande.
type fotosDeCierres struct{ almacen *movimientos.AlmacenFacturas }

func (f fotosDeCierres) Guardar(archivo multipart.File, encabezado *multipart.FileHeader) (tiendas.ArchivoSubido, error) {
	if f.almacen == nil {
		return tiendas.ArchivoSubido{}, errors.New("no hay almacen de archivos configurado")
	}
	guardado, err := f.almacen.Guardar(archivo, encabezado)
	switch {
	case errors.Is(err, movimientos.ErrFacturaMuyGrande):
		return tiendas.ArchivoSubido{}, tiendas.ErrFotoMuyGrande
	case errors.Is(err, movimientos.ErrFacturaTipo):
		return tiendas.ArchivoSubido{}, tiendas.ErrFotoTipo
	case errors.Is(err, movimientos.ErrFacturaVacia):
		return tiendas.ArchivoSubido{}, tiendas.ErrFotoVacia
	case err != nil:
		return tiendas.ArchivoSubido{}, err
	}
	return tiendas.ArchivoSubido{Ruta: guardado.Ruta, Nombre: guardado.Nombre, Tipo: guardado.Tipo}, nil
}

func (f fotosDeCierres) Abrir(rutaRelativa string) (*os.File, error) {
	return f.almacen.Abrir(rutaRelativa)
}

func (f fotosDeCierres) Eliminar(rutaRelativa string) error {
	return f.almacen.Eliminar(rutaRelativa)
}
