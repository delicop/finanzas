package auth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"finanzas/internal/httpx"
)

// CabeceraVerComo la manda el panel de administracion para mirar los datos de
// otro usuario. Es una cabecera y no un parametro de query a proposito: asi no
// choca con los filtros que ya usa /api/movimientos ni queda escrita en una URL
// que alguien pueda compartir por accidente.
const CabeceraVerComo = "X-Ver-Como"

// RequireAuth protege las rutas privadas.
//
// Un middleware en Go es simplemente una funcion que recibe un http.Handler y
// devuelve otro http.Handler que lo envuelve: corre codigo antes, decide si
// sigue (next.ServeHTTP) o si corta la cadena respondiendo el mismo.
//
// Ademas de validar la firma del token, consulta el usuario en la base. Podria
// ahorrarse esa consulta metiendo el rol dentro del JWT, pero entonces
// desactivar a alguien o quitarle el rol no surtiria efecto hasta que su token
// expirara: hasta 24 horas de admin regalado. Una consulta por clave primaria
// es barata; un permiso que sobrevive a su revocacion, no.
func RequireAuth(tm *TokenManager, store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				httpx.Error(w, http.StatusUnauthorized, "Falta el token de autenticación")
				return
			}

			// Formato esperado: "Bearer <token>"
			partes := strings.Fields(header)
			if len(partes) != 2 || !strings.EqualFold(partes[0], "Bearer") {
				httpx.Error(w, http.StatusUnauthorized, "Formato de Authorization inválido")
				return
			}

			usuarioID, err := tm.UsuarioIDDesdeToken(partes[1])
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "Token inválido o expirado")
				return
			}

			usuario, err := store.PorID(r.Context(), usuarioID)
			if errors.Is(err, ErrNoEncontrado) {
				// Token con firma valida pero de un usuario borrado.
				httpx.Error(w, http.StatusUnauthorized, "El usuario ya no existe")
				return
			}
			if err != nil {
				httpx.ErrorInterno(w, r, err, "auth: consultando usuario del token")
				return
			}
			if !usuario.Activo {
				httpx.Error(w, http.StatusUnauthorized, "La cuenta está desactivada")
				return
			}

			// Pasamos el id y el rol al resto de la cadena por el context del
			// request. Desde aqui, cualquier handler protegido puede hacer
			// httpx.UsuarioID(r.Context()) y saber quien es.
			ctx := httpx.ConUsuarioID(r.Context(), usuario.ID)
			ctx = httpx.ConRol(ctx, usuario.Rol)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin corta el paso a todo el que no sea el dueno del servidor.
// Va SIEMPRE despues de RequireAuth, que es quien deja el rol en el context.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.EsAdmin(r.Context()) {
			httpx.Error(w, http.StatusForbidden, "Necesitas permisos de administrador")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// VerComo deja que el admin consulte la informacion de otro usuario sin saber
// su contrasena: si llega la cabecera X-Ver-Como, cambia el id del context por
// el del observado y TODOS los endpoints existentes responden con los datos de
// esa persona, sin tocar una linea de los handlers.
//
// Es, literalmente, saltarse el filtro por usuario_id que impide el IDOR. Por
// eso esta cerrado con tres llaves a la vez:
//
//  1. solo un admin puede usarla;
//  2. solo en peticiones de LECTURA (GET) -- el admin mira las cuentas ajenas,
//     no las edita, y asi ningun movimiento puede aparecer modificado sin que
//     su dueno lo haya hecho;
//  3. el observado tiene que existir de verdad.
//
// El id real de quien pregunta queda en el context (httpx.Observador) para que
// se pueda distinguir de una consulta normal.
func VerComo(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			crudo := strings.TrimSpace(r.Header.Get(CabeceraVerComo))
			if crudo == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !httpx.EsAdmin(r.Context()) {
				httpx.Error(w, http.StatusForbidden, "Necesitas permisos de administrador")
				return
			}

			if r.Method != http.MethodGet {
				httpx.Error(w, http.StatusForbidden,
					"Los datos de otro usuario son de solo lectura")
				return
			}

			objetivo, err := strconv.ParseInt(crudo, 10, 64)
			if err != nil || objetivo <= 0 {
				httpx.Error(w, http.StatusBadRequest, "Cabecera "+CabeceraVerComo+" inválida")
				return
			}

			observado, err := store.PorID(r.Context(), objetivo)
			if errors.Is(err, ErrNoEncontrado) {
				httpx.Error(w, http.StatusNotFound, "El usuario no existe")
				return
			}
			if err != nil {
				httpx.ErrorInterno(w, r, err, "auth: consultando usuario observado")
				return
			}

			adminID, _ := httpx.UsuarioID(r.Context())

			// Mirarse a uno mismo no es "ver como": se deja pasar tal cual
			// para no marcar la peticion como observacion ajena.
			if observado.ID == adminID {
				next.ServeHTTP(w, r)
				return
			}

			ctx := httpx.ConUsuarioID(r.Context(), observado.ID)
			ctx = httpx.ConObservador(ctx, adminID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
