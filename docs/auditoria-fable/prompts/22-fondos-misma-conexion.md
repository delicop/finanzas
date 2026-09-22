# 22 · El diagnóstico de fondos usa una segunda conexión con la transacción abierta

Contexto: el pool tiene `MaxOpenConns(10)` (`backend/internal/db/db.go`
~28). En `movimientos/store.go` (~253 `porQueNoEntro`, ~416-423 `existe`)
y en `fondos.go` (~341) se consulta con `s.db` mientras la transacción
sigue viva y el advisory lock por usuario está tomado. Cada escritura
fallida ocupa dos conexiones; diez a la vez se bloquean entre sí. Aparte,
`flujosSQL` (`fondos.go` ~44-95, ocho ramas `UNION ALL`) se ejecuta hasta
cuatro veces por escritura y recorre todo el historial del usuario: hoy
aguanta, pero crece con cada movimiento y no hay índice `(usuario_id,
tipo)`.

Qué hacer:

1. Pasa la transacción a esas consultas. Ya existe la interfaz `consultor`
   en `fondos.go` (~177): que `porQueNoEntro` y `existe` la acepten y
   reciban `tx`. Alternativa aceptable: hacer el diagnóstico **después** del
   `Rollback`.
2. Índice compuesto en una migración nueva: `movimientos (usuario_id, tipo,
   fecha)` y revisa con `EXPLAIN ANALYZE` que `flujosSQL` lo use.
3. Calcula la foto de saldos **una vez** por escritura y reutilízala en
   `verificarFondos`, `otrosMedios` y `comparar`, en vez de cuatro veces.
4. No materialices saldos todavía: es un cambio grande y con las dos cosas
   anteriores alcanza por años a la escala de la Raspberry.
5. Test de concurrencia en `integracion_test.go`: 15 escrituras sin fondos
   en paralelo terminan todas (con 409) en menos de 5 s.

Commit: "Registrar plata sin fondos ya no deja conexiones colgadas".
