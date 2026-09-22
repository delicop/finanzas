# 23 · Sacar las reglas de dominio del handler de movimientos

Contexto: `backend/internal/movimientos/handler.go` tiene 780 líneas.
`Validar` y `validarCubrir` (~345-517, unas 200 líneas de reglas de negocio:
qué campos exige cada tipo, cuándo hay `a_quien`, `medio_cobro`, `cobrar_el`,
qué es un traslado válido) viven en el handler aunque las usan `agente` y
`recurrentes`. `fechaValida` (~773) exige año 2000-2100 mientras
`agente/herramientas.go` (~652) tiene otra versión sin ese tope: dos reglas
distintas para el mismo dato.

Qué hacer:

1. Nuevo archivo `movimientos/reglas.go` con la función de validación (el
   nombre que ya tenga) y todo lo que hoy está en ~345-517. El handler solo
   lee el JSON, llama a la validación y responde.
2. Una sola `FechaValida` en `dinero` o en `movimientos` y que `agente` y
   `recurrentes` la importen. Decide una regla (2000-2100 es razonable) y
   documéntala.
3. Los errores de dominio que hoy nadie mapea a HTTP: mapea `ErrMismoMedio`
   a 400 (hoy da 500 si llega) y borra los que no se usan
   (`ErrYaTieneFactura`).
4. Tests: mueve los de validación que existan a `reglas_test.go` y agrega
   uno por tipo con el caso feliz y el caso roto.
5. `docs/arquitectura.md`: el patrón pasa a ser modelo / reglas / store /
   handler. Explica que `reglas.go` es lo que comparten el formulario, el
   chat y los recurrentes.

Sin cambios de comportamiento: la API responde igual antes y después.

Commit: "Las reglas de un movimiento viven en un solo sitio para el formulario, el chat y los recurrentes".
