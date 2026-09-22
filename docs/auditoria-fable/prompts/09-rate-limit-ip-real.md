# 09 · El límite de intentos de login se salta con una cabecera

Contexto: `backend/internal/auth/limitador.go` (`ipDelRequest`, ~líneas
86-91) y `middleware.RealIP` en `cmd/api/router.go` (~65) confían en
`X-Forwarded-For` sin lista de proxies confiables. Como
`docker-compose.yml` publica el puerto 8080 del backend en el host,
cualquiera manda `X-Forwarded-For: 1.2.3.4`, rota el valor en cada intento y
hace fuerza bruta contra `/api/auth/login` sin tocar el límite de 10 cada 15
minutos. Además `Login` (`auth/handler.go` ~84-90) cuenta el intento antes
de validar el cuerpo, y el freno es solo por IP (`docs/decisiones.md` lo
reconoce como pendiente).

Qué hacer:

1. Nueva variable `PROXIES_CONFIABLES` (lista de IPs o CIDR, vacía por
   defecto). Solo si `RemoteAddr` está en esa lista se lee
   `X-Forwarded-For`; si no, se usa `RemoteAddr` y punto. Quita
   `middleware.RealIP` o cámbialo por uno propio con la misma regla.
2. Cuenta el intento **solo** cuando las credenciales son incorrectas, no
   cuando el JSON viene mal.
3. Agrega un segundo contador **por correo** (por ejemplo 5 fallos en 15
   min) que se suma al de IP. Al bloquear por correo, responde el mismo 429
   sin decir si el correo existe.
4. Tests en `limitador_test.go`: una cabecera falsa desde una IP no confiable
   no cambia la clave del contador; desde una confiable sí; el contador por
   correo bloquea aunque cambie la IP.
5. Documenta la variable en `.env.example`, `docker-compose.yml` y
   `docs/despliegue.md` (Caddy/Cloudflare en la Raspberry: qué IP poner).
   Actualiza el párrafo de `docs/decisiones.md` sobre el límite por IP.

Commit: "El freno de intentos de login ya no se salta con una cabecera falsa".
