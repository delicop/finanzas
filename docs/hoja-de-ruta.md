# Hoja de ruta

Lo que se le podría agregar a la app, en orden de valor. Las marcadas
**hecho** ya están en la app; el resto queda para después.

## Para el cliente

| # | Idea | Estado |
|---|---|---|
| 1 | **Foto de la factura en el chat.** Adjuntar la foto o el PDF desde el chat y que quede como factura del gasto; el asistente la pide al preparar un "pagué". (Que el asistente *lea* la foto queda para después.) | Hecho ([agente.md](agente.md#foto-o-pdf-de-la-factura)) |
| 2 | **Notas de voz.** Dictar "pagué 20 mil de gasolina" en vez de escribirlo, como en WhatsApp. | Hecho ([agente.md](agente.md#notas-de-voz)) |
| 3 | **Presupuestos por categoría.** "Máximo $500.000 al mes en Personal", con aviso al llegar al 80%. | Después |
| 4 | **Gastos que se repiten.** El arriendo o el internet se crean solos cada mes y solo se confirman. | Después |
| 5 | **Gráfica simple del mes.** Barras de en qué se fue la plata y comparación con el mes anterior. | Después |
| 6 | **Exportar a Excel o PDF** los movimientos de un rango de fechas, para el contador. | Hecho ([api.md](api.md#exportar-a-excel-o-pdf)) |
| 7 | **Recordatorio de préstamos por WhatsApp.** Un botón que arma el mensaje ("Hola Carlos, te recuerdo los $200.000…") y abre WhatsApp listo para enviar. | Después |
| 8 | **Instalar la app en el celular (PWA).** Ícono en la pantalla de inicio y sin barra del navegador. | Hecho ([despliegue.md](despliegue.md#instalar-la-app-en-el-celular-pwa)) |

### Agregado por el camino

- **Fecha de pago de los préstamos** y aviso el día del cobro
  ([avisos.md](avisos.md#el-día-del-cobro)).
- **Terminar y guardar conversaciones** del asistente, con la pregunta "¿Necesitas
  algo más?" después de guardar ([agente.md](agente.md#terminar-y-guardar-la-conversación)).

## Para el dueño del servidor

| # | Idea | Estado |
|---|---|---|
| 9 | **Cobro en línea** (Wompi o Mercado Pago, que funcionan en Colombia). El cliente paga solo y el pago queda registrado sin anotarlo a mano. | Después |
| 10 | **Cortar el acceso si no paga.** Unos días de gracia después de vencer y luego solo lectura. | Después |
| 11 | **Periodo de prueba.** Un plan gratis de 15 días, con IA, para enganchar clientes. | Después |
| 12 | **Consumo de IA por cliente.** Cuánto cuesta cada uno en el modelo, para saber si su plan es rentable. Los tokens ya se guardan; falta mostrarlos. | Después |
| 13 | **Respaldo automático diario** de la base y las facturas en la Raspberry, con aviso si falla. | Después |

## Seguridad

| # | Idea | Estado |
|---|---|---|
| 14 | **Recuperar la contraseña por correo.** Hoy solo el administrador puede resetearla desde el panel. | Después |

## Si hubiera que escoger tres

1. **Que el asistente lea la foto** de la factura (monto y comercio solos).
2. **Instalar en el celular** (8): casi no cuesta y la app se siente nativa.
3. **Cobro en línea** (9): le quita trabajo al dueño cada mes.
