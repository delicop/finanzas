# 16 · Búsqueda con debounce, cancelación y sin ensuciar el historial

Contexto: en `frontend/src/paginas/Movimientos.jsx` (~319-324) el campo de
búsqueda llama `cambiarFiltro('q', ...)` en cada `onChange`, eso hace
`setParams` (push, no replace) y dispara `cargar()` (~70-91) sin
`AbortSignal`. Una petición por tecla, respuestas que pueden llegar en
desorden (la última no siempre gana) y el botón Atrás del navegador
recorre la búsqueda letra por letra. Además `acciones` se crea nuevo en
cada render (~178-188) y `FilaMovimiento`/`TarjetaMovimiento` no están
memoizados, así que 50 filas se repintan al escribir.

Qué hacer:

1. Estado local para el texto + `useDeferredValue` o un debounce de 300 ms
   antes de tocar la URL.
2. `setParams(nuevos, { replace: true })` para los filtros: un cambio de
   filtro no es una página nueva en el historial.
3. Cancelación: si ya migraste a TanStack Query (prompt 08) esto viene
   gratis con el `signal`; si no, pasa `senal` a `apiFetch` desde un
   `AbortController` en el `useEffect`.
4. `memo(FilaMovimiento)` y `memo(TarjetaMovimiento)`, con handlers estables
   (`useCallback`) o pasando solo ids.
5. Aplica lo mismo al buscador de `Clientes.jsx` si lo tiene.

Verifica en DevTools > Network: escribir "gasolina" debe producir una o dos
peticiones, no ocho.

Commit: "Buscar ya no dispara una petición por cada letra".
