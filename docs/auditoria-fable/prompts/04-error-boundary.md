# 04 · Un error de render no debe dejar la pantalla en blanco

Contexto: no existe ningún `ErrorBoundary` en `frontend/src/`. `main.jsx`
monta `<App/>` sin red. Un campo nulo inesperado del backend (por ejemplo
`u.creado_en.slice` en `Clientes.jsx` ~227 o `nombre[0]` en `Negocio.jsx`
~25) tumba la app entera y el usuario ve blanco.

Qué hacer:

1. Crea `frontend/src/componentes/ErrorBoundary.jsx` (clase con
   `getDerivedStateFromError` y `componentDidCatch`). Muestra un mensaje
   corto en el tono de la app ("Algo se rompió en esta pantalla"), un botón
   "Recargar" y, en desarrollo, el error.
2. Envuelve `<Routes>` en `App.jsx` con él. Mejor aún: uno por página dentro
   del `Layout`, para que el menú siga funcionando y el usuario pueda irse a
   otra pantalla sin recargar.
3. Reporta el error al backend si existe un endpoint para ello; si no, no lo
   inventes: con `console.error` basta por ahora.
4. Corrige los dos accesos inseguros citados arriba con encadenamiento
   opcional (`?.`) y un valor por defecto.

Sigue las reglas visuales de `docs/diseno.md`.

Commit: "Si una pantalla falla se avisa y se puede recargar, no queda en blanco".
