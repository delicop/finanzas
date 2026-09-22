# 25 · Quitar la duplicación pequeña del backend

Contexto, todo verificado:

- `escaneable`/`fila` (la interfaz para escanear `*sql.Row` y `*sql.Rows`)
  está definida cinco veces: `movimientos/store.go` ~577, `agente/store.go`
  ~167, `recurrentes/store.go` ~204, `registro/registro.go` ~170,
  `admin/store.go` ~139.
- `zonaColombia` tres veces en Go (`movimientos/exportar.go` ~34,
  `avisos/generador.go` ~581, `recurrentes/recurrente.go` ~57) y
  `'America/Bogota'` a mano en SQL (`admin/store.go` ~119,
  `recurrentes/store.go` ~33).
- `contextoDeCierre` en `agente/propuestas.go` ~364 y
  `recurrentes/confirmar.go` ~125.
- `porQueNoEntro`/`existe` en `movimientos/store.go` ~323/~564 y
  `recurrentes/store.go` ~179/~193.
- `pesos` en `avisos/generador.go` ~610 hace lo mismo que `dinero.Formatear`.
- `middleware.Logger` de chi (`router.go` ~66) escribe texto coloreado
  mientras `main.go` configura slog en JSON: dos formatos en el mismo stdout.
- Código sin uso: `ErrLimiteDiario` y `ErrPropuestaCaducada`
  (`agente/agente.go` ~229, ~251), `Config.IsProduction` (`config.go`
  ~190), `Store.CambiarEstado` (`abonos.go` ~544).
- Mensajes viejos al modelo: `herramientas.go` ~499 ("recibi, pague y
  preste", faltan dos tipos) y ~502 ("pendiente y pagado", falta parcial).
- `recurrentes/confirmar.go` ~115 y ~122 descartan errores con `_ =`; en
  `agente/propuestas.go` sí se registran.
- `recurrentes/store.go` ~351-366 inserta y relee fecha por fecha;
  `abonos.go` ~375-383 inserta las cuotas de un acuerdo una a una.

Qué hacer:

1. Paquete `internal/sqlx` (o dentro de `db`, escoge uno) con `Escaneable`
   y úsalo en los cinco sitios.
2. `dinero.ZonaColombia` (o `internal/fechas`) única; en SQL, pasa la zona
   como parámetro o usa `AT TIME ZONE $n`.
3. Un solo `contextoDeCierre` en el paquete que lo posea (probablemente
   `movimientos`).
4. `pesos` pasa a `dinero.Formatear`.
5. Reemplaza `middleware.Logger` por uno propio de veinte líneas que escriba
   con `slog` (método, ruta, status, duración, request id).
6. Borra el código muerto, corrige los mensajes al modelo, registra los
   errores de `confirmar.go` con `slog.Error`.
7. `INSERT ... VALUES (...),(...) RETURNING` para ocurrencias y cuotas.
8. `go vet`, `staticcheck` y todos los tests en verde. `staticcheck` debería
   quedar sin advertencias después de esto.

Commit: "Limpieza del servidor: una sola versión de cada cosa repetida".
