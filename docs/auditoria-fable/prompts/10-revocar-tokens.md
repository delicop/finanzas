# 10 · Cambiar la contraseña debe cerrar las sesiones abiertas

Contexto: `auth/handler.go` (~269-272) y `admin/handler.go` (~326-330) lo
admiten en comentarios: los JWT ya emitidos siguen vivos hasta 24 horas
aunque el usuario o el admin cambien la clave. Un token robado sobrevive al
reseteo. `RequireAuth` (`auth/middleware.go` ~51) ya consulta la fila del
usuario en cada petición (para leer rol y activo), así que comparar un dato
más sale casi gratis.

Qué hacer:

1. Migración nueva: columna `usuarios.sesiones_desde TIMESTAMPTZ NOT NULL
   DEFAULT now()`. Con `Down`.
2. Al cambiar o resetear la contraseña, y al desactivar, poner
   `sesiones_desde = now()` en la misma transacción.
3. El token ya lleva `iat`. En `RequireAuth`, si `iat < sesiones_desde`
   responde 401 con el mismo mensaje de token inválido. Cuidado con la
   precisión: el `iat` del JWT es en segundos; trunca `sesiones_desde` a
   segundos y compara con `<`, no `<=`, para que el token emitido en el
   mismo segundo del login nuevo pase.
4. El propio usuario que cambia su clave debe recibir un token nuevo en la
   respuesta (o el frontend vuelve a hacer login). Escoge lo primero: menos
   fricción.
5. Un botón "Cerrar sesión en todos los dispositivos" no hace falta todavía;
   con lo anterior alcanza.
6. Tests: en `middleware_test.go` (créalo si no existe) un token con `iat`
   anterior a `sesiones_desde` es rechazado; uno posterior pasa.
7. Actualiza `docs/decisiones.md` (sección JWT) y `docs/api.md`.

Commit: "Cambiar la clave cierra las sesiones que estaban abiertas".
