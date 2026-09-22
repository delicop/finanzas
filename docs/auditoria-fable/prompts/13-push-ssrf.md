# 13 · El endpoint de push acepta cualquier URL (SSRF)

Contexto: `backend/internal/push/handler.go` (~línea 90) solo exige que el
`endpoint` de la suscripción empiece por `https://`. Después, la tarea de
fondo (`webpush.go` ~177-190) hace un `POST` a esa URL desde el servidor.
Un usuario puede apuntar a servicios internos de la red de la Raspberry o
de la red de Docker.

Qué hacer:

1. Lista de hosts permitidos para los servicios de push reales:
   `fcm.googleapis.com`, `updates.push.services.mozilla.com`,
   `*.notify.windows.com`, `web.push.apple.com`, y los sufijos que use
   Chrome/Edge (`*.push.services.mozilla.com`, `android.googleapis.com`).
   Configurable por variable `PUSH_HOSTS_PERMITIDOS` por si aparece uno
   nuevo, con esa lista como valor por defecto.
2. Además, resolver el host y rechazar IPs privadas, loopback y link-local
   (`net.IP.IsPrivate`, `IsLoopback`, `IsLinkLocalUnicast`). Es la segunda
   llave por si la lista se abre demasiado.
3. Rechazar al **suscribirse** (400 con mensaje claro), no al enviar.
4. En el cliente HTTP de `webpush.go`, `CheckRedirect` que no siga
   redirecciones.
5. Tests en `push/handler_test.go` (créalo): un endpoint a `https://10.0.0.5`
   y otro a un host fuera de la lista se rechazan; uno de FCM pasa.
6. Nota en `docs/avisos.md` (sección "Detalles que importan").

Commit: "Solo se aceptan suscripciones de los servicios de push conocidos".
