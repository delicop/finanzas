# 02 · El enlace "Cuentas con cada quien" no filtra

Contexto: en `frontend/src/paginas/Dashboard.jsx` (~línea 254) cada persona
de "Cuentas con cada quien" enlaza a `/movimientos?a_quien=NOMBRE`. Pero en
`frontend/src/paginas/Movimientos.jsx` (~líneas 56-64) el objeto `filtros` se
arma solo con `categoria_id`, `medio_pago_id`, `tipo`, `estado`, `desde`,
`hasta` y `q`: el parámetro `a_quien` se pierde y el usuario ve la lista
completa creyendo que es la de esa persona. El backend **sí** acepta
`a_quien` (`backend/internal/movimientos/handler.go` ~línea 102).

Qué hacer:

1. Agrega `a_quien` a `filtros` en `Movimientos.jsx` y pásalo al API.
2. Muéstralo como filtro activo con su chip o etiqueta, y que se pueda quitar
   igual que los demás (que cuente en `filtrosActivos`).
3. Revisa que `movimientosApi.listar` en `lib/api.js` no descarte parámetros
   que no conoce.
4. Prueba manual: desde el dashboard, tocar un nombre debe mostrar solo los
   movimientos con esa persona, y "Limpiar filtros" debe quitarlo.

Commit: "El enlace de cada persona en el resumen ya filtra sus movimientos".
