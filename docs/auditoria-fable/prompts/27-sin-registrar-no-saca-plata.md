# 27 · "Sin registrar" no puede sacar plata (ALTA)

Contexto: la regla de la app es que ningún medio queda en negativo (commits
recientes, `fondos.go`). Al crear un movimiento el medio es obligatorio
(`movimientos/handler.go` ~373: "sin él, el resumen no puede decir dónde
está la plata"). Pero en `ModalAbonos.jsx` (~312), `ModalCobro.jsx` (~82) y
`PropuestaAgente.jsx` (~415, ~531) existe la opción `Sin registrar`
(medio vacío o 0). Cuando la plata **entra** (te devolvieron un préstamo y
no sabes por dónde) es una opción legítima y el resumen la muestra en su
fila punteada (`Dashboard.jsx` ~345). Cuando la plata **sale** es un hueco:
pagar una deuda propia o abonarla con "Sin registrar" no pregunta nada y
"Tienes" baja de cero. Con todo en $0 quedó en −$30.000. Segundo caso del
mismo hueco: crear un "Me prestaron" ya marcado como `pagado` descuadra
Efectivo (muestra $50.000 que ya devolviste), porque la salida de la
devolución no pasa por la guardia de fondos.

Decisión tomada: **"Sin registrar" solo existe para plata que entra.** Para
plata que sale, el medio es obligatorio y aplica la misma pregunta de
"¿de dónde salió?" que en los gastos.

Qué hacer:

1. Backend, `ValidarAbono` (`handler_abonos.go` ~98) y el cobro/pago de
   deudas (`movimientos/abonos.go`, `handler.go` cambiar estado): si el
   movimiento es `me_prestaron` (deuda propia) o el abono es un pago tuyo,
   `medio_id` es obligatorio. Mensaje: "Indica de dónde salió la plata".
   Si es `preste` (te pagan), `medio_id` puede ir vacío como hoy.
2. Backend, crear o editar un `me_prestaron` con `estado = pagado`: exige
   `medio_cobro_id` y pasa esa salida por `verificarFondos` igual que un
   gasto (con `cubrir` si no alcanza). Mismo trato al cambiar el estado de
   pendiente a pagado.
3. Frontend: en `ModalAbonos`, `ModalCobro` y `PropuestaAgente`, la opción
   "Sin registrar" solo se dibuja cuando la plata entra (`!propia`). Cuando
   sale, el select arranca vacío con placeholder "¿De dónde salió?" y el
   error de campo del backend se muestra debajo.
4. Agente: en `herramientas.go` (~1049) y `propuestas.go` (~129) la regla
   "sin registrar es legítimo al cobrar un préstamo" se mantiene, pero al
   **pagar** una deuda la tarjeta exige medio. Ajusta `prompt.go` para que
   el modelo lo pida.
5. Test de integración: pagar una deuda propia sin medio → 400; un
   `me_prestaron` creado ya pagado sin fondos → 409 con `falta_plata`; te
   pagan un préstamo sin medio → 201 y la fila "Sin registrar" del resumen
   sube. Y el test de siempre: después de todo, `Tienes` nunca es negativo.
6. `docs/api.md` (sección "Ningún medio queda en negativo") y
   `docs/decisiones.md`: explicar que "Sin registrar" es solo de entrada.

Commit: "Pagar una deuda ya no puede salir de 'Sin registrar': se pregunta de dónde salió".
