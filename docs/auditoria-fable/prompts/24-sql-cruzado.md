# 24 · Cada paquete es dueño de sus tablas

Contexto: varios paquetes escriben SQL contra tablas de otros:
`admin/store.go` (~96-131) consulta `movimientos`, `abonos`,
`agente_mensajes` y `planes`; `auth/store.go` (~305-310, `TieneIA`) lee
`planes`; `movimientos/exportar.go` (~98) lee `usuarios`; `avisos/store.go`
(~140) lee `planes`. Un cambio de esquema en un paquete rompe SQL en otros
cuatro y nadie se entera hasta producción.

Qué hacer:

1. Regla nueva en `docs/arquitectura.md`: **un paquete solo escribe SQL
   sobre sus propias tablas**. Lo que otro necesite se pide por una función
   exportada del dueño.
2. `suscripciones` expone `PlanDeUsuario(ctx, usuarioID)` y `TieneIA`; que
   `auth`, `avisos` y `admin` la usen. `auth.TieneIA` pasa a ser un
   adaptador o desaparece.
3. `movimientos` expone `ActividadPorUsuario(ctx)` (o lo que el panel de
   admin necesite) y `admin` la consume. Lo mismo con `agente` para el
   consumo de mensajes.
4. `exportar.go` recibe el nombre del usuario como parámetro desde el
   handler (que ya lo tiene en el context) en vez de consultarlo.
5. Comprueba con grep que ningún `FROM`/`JOIN` de un paquete nombra una
   tabla ajena. Si te queda una excepción justificada (un JOIN de
   rendimiento), documéntala en el archivo con el porqué.
6. Los tests de integración de `admin` deben seguir pasando.

Sin cambios de comportamiento.

Commit: "Cada parte del servidor consulta solo sus tablas".
