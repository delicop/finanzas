# Propuesta: varios usuarios en una sola cuenta

Fecha: 21 de septiembre de 2026

## Por qué esto no está en la versión actual

Lo que se pidió y se entregó al principio fue una app de finanzas para **una
sola persona**: registrar ingresos, gastos, préstamos, deudas, traslados entre
medios de pago, recurrentes y facturas.

Que otras personas entren a la misma cuenta, cada una con permisos sobre
ciertas categorías, y que se puedan enviar plata entre ellas **no hacía parte
de ese pedido**. Por eso no se construyó. Es una funcionalidad nueva, y este
documento describe qué incluye, cuánto cuesta y en qué condiciones se entrega.

## Qué se va a hacer

La app queda como una **copia dedicada** para este cliente, con un único dueño.
Sobre esa base se agrega:

1. **Colaboradores.** El dueño crea usuarios para otras personas, cada una con
   su propio correo y contraseña. Puede desactivarlos o cambiarles la clave
   cuando quiera.
2. **Permisos por categoría.** El dueño decide sobre qué categorías puede
   trabajar cada colaborador. Un colaborador solo ve y modifica los ingresos,
   gastos y demás movimientos de sus categorías. El dueño ve todo.
3. **Envíos de plata entre personas.** Cada persona tiene su propia caja. Se
   puede enviar dinero de una caja a otra, y quien lo recibe confirma que le
   llegó. Se puede ver el saldo de cada persona.
4. **Registro de quién hizo qué.** Cada movimiento muestra quién lo registró y
   quién lo editó por última vez. Se puede filtrar por persona.
5. **Cuenta única.** Se retira de esta copia todo lo que servía para manejar
   varios clientes independientes (panel de clientes, planes y cobros), porque
   aquí no aplica.

## Opciones y precios

| Opción | Qué incluye | Precio (pago único) |
|---|---|---|
| **Sin asistente** | Los cinco puntos de arriba | $1.500.000 – $1.800.000 |
| **Con asistente (IA)** | Lo anterior, más el chat con el asistente adaptado a varios usuarios | $2.400.000 – $2.800.000 |

Si se elige **con asistente**, estos ajustes van incluidos:

- **Chat privado.** Cada persona tiene su propio historial de conversación y
  nadie ve el de los demás.
- **Respeta los permisos.** El asistente solo responde con datos de las
  categorías que esa persona tiene permitidas.
- **Avisos respetando permisos.** Los resúmenes y recordatorios automáticos
  también respetan esos permisos.

## Condiciones

- **No hay mensualidad.** Es un pago único.
- **El servidor lo compra el cliente,** y queda a su nombre.
- **El asistente (si se elige) usa una cuenta de DeepSeek del cliente.** El
  cliente crea la cuenta, le recarga saldo y ese consumo lo paga directamente.
- **Garantía de un mes** desde la entrega:
  - **Cubre:** errores, es decir cualquier cosa de lo acordado que no funcione
    como debe.
  - **No cubre:** funcionalidades nuevas ni cambios sobre lo acordado.
  - **Después del mes:** cualquier arreglo o cambio se cotiza aparte.

## Qué se entrega

- **La app instalada y funcionando** en el servidor del cliente.
- **Copia de seguridad automática diaria** de la base de datos.
- **Reinicio automático** de la app si el servidor se reinicia o algo se cae.
- **Todos los accesos a nombre del cliente:** servidor, dominio, usuario dueño
  de la app y, si aplica, la cuenta de DeepSeek.
- **Una guía corta de uso:** cómo entrar, cómo crear colaboradores y asignar
  permisos, y a quién contactar si algo falla.
