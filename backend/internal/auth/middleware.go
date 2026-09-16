package auth

import (
	"net/http"
	"strings"

	"finanzas/internal/httpx"
)

// RequireAuth protege las rutas privadas.
//
// Un middleware en Go es simplemente una funcion que recibe un http.Handler y
// devuelve otro http.Handler que lo envuelve: corre codigo antes, decide si
// sigue (next.ServeHTTP) o si corta la cadena respondiendo el mismo.
func RequireAuth(tm *TokenManager) func(http.Handler) http.Handler {
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

			// Pasamos el id al resto de la cadena por el context del request.
			// Desde aqui, cualquier handler protegido puede hacer
			// httpx.UsuarioID(r.Context()) y saber quien es.
			ctx := httpx.ConUsuarioID(r.Context(), usuarioID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
