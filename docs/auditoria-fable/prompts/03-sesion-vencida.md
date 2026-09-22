# 03 · Cerrar la sesión cuando el token vence

Contexto: en `frontend/src/lib/api.js` (líneas ~97, ~114 y ~137) un 401
hace `tokenStorage.clear()` y nada más. `AuthContext.jsx` solo valida el token
al montar. Resultado: con el JWT vencido (24 h) la app sigue dibujada, cada
acción falla con "token inválido" y solo un F5 lleva al login. Lo mismo
pasa con un 403 por cuenta desactivada.

Qué hacer:

1. En `api.js`, cuando llegue un 401 (o un 403 cuyo mensaje sea de cuenta
   desactivada), además de limpiar el token emite un evento, por ejemplo
   `window.dispatchEvent(new CustomEvent('finanzas:sesion-cerrada', { detail: { motivo } }))`.
   Que los tres sitios que hoy hacen `clear()` pasen por una sola función.
2. En `AuthProvider`, un `useEffect` escucha ese evento y hace lo mismo que
   `logout()`: limpia `verComo`, pone `usuario` en null. React Router entonces
   redirige al login por las rutas protegidas que ya existen.
3. En `Login.jsx`, si llegó con motivo "vencida", muestra una línea tranquila
   tipo "Tu sesión venció, entra de nuevo". Sin alarma.
4. No rompas el modo "ver como": un 403 por escribir mientras se observa a
   otro NO es cierre de sesión. Distingue por el mensaje que manda el backend
   (`auth/middleware.go`) y, si hace falta, agrega un campo `codigo` a esa
   respuesta de error para no depender del texto.

Verifica: pon `JWT_EXPIRY_HOURS=1` no sirve para probar rápido; en su lugar
borra el token a mano en DevTools y haz una acción: debe ir al login sin F5.

Commit: "Al vencer la sesión la app vuelve al login sola".
