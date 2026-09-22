# 12 · /health debe mirar la base de datos

Contexto: `cmd/api/router.go` (~136-141) responde `{"status":"ok"}` siempre.
Si Postgres se cae, Docker y cualquier monitoreo siguen viendo la API sana.
El servicio `backend` en `docker-compose.yml` no tiene `healthcheck`.

Qué hacer:

1. `/health` hace `pool.PingContext` con timeout de 1 s. Si falla, 503 con
   `{"status":"error","db":"sin conexion"}`. No des más detalle: es público.
2. Agrega en `docker-compose.yml` un `healthcheck` al backend con
   `wget -qO- http://localhost:8080/health` (alpine trae wget) y que
   `frontend` dependa con `condition: service_healthy`.
3. Opcional y barato: `/health` incluye `"version"` leída de una variable
   inyectada en el build (`-ldflags "-X main.version=..."`), para saber qué
   está corriendo en la Raspberry.
4. Documenta en `docs/despliegue.md` (sección Verificar).

Commit: "El chequeo de salud avisa si la base de datos no responde".
