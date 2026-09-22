# 26 · Partir herramientas.go del agente

Contexto: `backend/internal/agente/herramientas.go` tiene 1098 líneas y
mezcla tres cosas: los esquemas JSON que se le mandan al modelo (~90-282),
la ejecución de cada herramienta (consultas de lectura), y la preparación de
propuestas (escrituras por confirmar). Ya existen `herramientas_deudas.go`,
`herramientas_recurrentes.go` y `propuestas.go`, así que el criterio de
partición está a medias.

Qué hacer:

1. `esquemas.go`: solo las definiciones de herramientas que ve el modelo
   (nombre, descripción, parámetros). Que un test compruebe que cada
   herramienta del esquema tiene su función registrada y viceversa.
2. `lectura.go`: las herramientas que solo consultan (resumen, buscar
   movimientos, categorías, medios, cuentas por persona).
3. `proponer.go`: las que preparan un movimiento, un abono o un recurrente
   por confirmar. Que reutilicen las reglas de `movimientos` del prompt 23.
4. `catalogo.go`: `NuevoCatalogo`, `ConRecurrentes` y el despacho por nombre.
5. Sin cambios de comportamiento: los tests de `agente` (cierre, tarjetas,
   guardadas, integración) pasan sin tocarlos. Si hay que tocar uno, es
   señal de que se cambió algo más que la ubicación.
6. Actualiza el párrafo de `docs/arquitectura.md` que describe los archivos
   de `agente` y la sección "Las herramientas" de `docs/agente.md`.

Commit: "El catálogo del asistente queda partido por lo que hace cada pieza".
