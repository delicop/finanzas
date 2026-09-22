# 33 · El Resumen dice "Septiembre de 2026" pero suma todo el historial

Contexto: `Dashboard.jsx` (~333-340) pinta el mes actual como rótulo, pero
`resumen.go` suma todo: un gasto de agosto y los de octubre entran en
"Pagado". Aparte, el conteo de movimientos por medio no coincide entre la
página Medios y el Resumen (probablemente uno cuenta traslados o abonos y
el otro no).

Decisión tomada: **los saldos son totales, los flujos son del periodo.**
"Tienes", "Te deben" y "Debes" son fotos y no tienen mes. "Recibido" y
"Pagado" son flujos y sí lo tienen; por defecto el mes actual, con forma
de cambiarlo.

Qué hacer:

1. `GET /api/dashboard` acepta `desde` y `hasta` (por defecto el mes
   actual en Colombia). `recibido`, `pagado` y el desglose por categoría se
   calculan en ese rango; `tienes`, saldos por medio, `te_deben`, `debes` y
   cuentas por persona son totales hasta hoy (ver prompt 30).
2. La respuesta dice el rango que usó (`periodo: {desde, hasta}`) para que
   el rótulo no se invente en el cliente.
3. Frontend: el rótulo sale de `periodo`; un selector sencillo "‹ Mes ›" y
   un enlace "Todo el historial". Las tarjetas de saldo llevan un subtítulo
   "hoy" y las de flujo el mes, para que se lea la diferencia.
4. Conteo por medio: decide una sola definición ("movimientos que tocan
   este medio, incluidos traslados y abonos") y úsala en `medios/store.go` y
   en `resumen.go` con la misma consulta, o mejor, que Medios llame a la
   función del paquete `movimientos` (regla del prompt 24).
5. Tests: `TestLosSaldosPorMedioSumanElBalance` sigue pasando; uno nuevo
   donde un gasto de agosto no entra en "Pagado" de septiembre pero sí baja
   "Tienes".
6. `docs/api.md` (sección Resumen).

Commit: "El Resumen muestra los gastos del mes y los saldos de hoy, y se puede cambiar de mes".
