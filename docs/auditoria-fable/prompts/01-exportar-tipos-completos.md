# 01 · Exportar a Excel y PDF: faltan tipos y estados

Contexto: la app exporta movimientos a Excel y PDF desde
`backend/internal/movimientos/informe.go` y `exportar.go`. Los mapas
`nombresTipo` y `nombresEstado` (informe.go, líneas ~18-26) solo conocen
`recibi`, `pague`, `preste` y los estados `pendiente`, `pagado`. Los tipos
`me_prestaron` y `traslado` y el estado `parcial` (definidos en
`movimiento.go`) salen con la celda en blanco. Además la columna "Devuelto
por" muestra `medio_cobro`, que en un traslado es el medio destino, no por
dónde devolvieron; y el detalle de deuda del PDF solo se arma para `preste`.

Qué hacer:

1. Deriva los nombres visibles de las constantes de `movimiento.go`, no de un
   mapa aparte que se olvide actualizar. Nombres sugeridos: "Me prestaron",
   "Traslado", "Parcial".
2. En traslados, la columna del medio secundario debe decir "A" o "Destino",
   no "Devuelto por". Revisa las cabeceras de Excel y PDF.
3. El detalle de deuda (abonos, saldo) debe salir también para `me_prestaron`.
4. Las pruebas manuales vieron además: **columnas corridas** en Excel (el
   medio destino del traslado cae en "Devuelto por"), **falta la línea de
   "Debes"** en el resumen final del informe (hay "Te deben" pero no lo que
   tú debes), y en el PDF los **emojis de las categorías salen como "."**
   porque la fuente de `go-pdf/fpdf` no los tiene. Para los emojis: quítalos
   del texto del PDF con una función que filtre fuera del rango latino (el
   nombre queda "Comida" en vez de "🍕 Comida"), o incrusta una fuente TTF
   con soporte; lo primero es suficiente.
5. Agrega un test en `informe_test.go` que exporte un movimiento de **cada
   tipo** y de cada estado y compruebe que ninguna celda de tipo o estado
   queda vacía, que las columnas de un traslado caen donde deben, y que el
   pie trae "Te deben" y "Debes". Que falle si alguien agrega un tipo nuevo
   y no lo nombra.
6. Actualiza `docs/api.md` (sección "Exportar a Excel o PDF") si cambia alguna
   columna.

Verifica con `cd backend && go test ./internal/movimientos/...`.
Commit en español, una línea que diga qué ve el usuario, por ejemplo:
"El Excel y el PDF ya nombran las deudas propias, los traslados y los parciales".
