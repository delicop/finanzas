# 31 · La ventana "¿De dónde salió la plata?" se enreda

Contexto: `frontend/src/componentes/CubrirFaltante.jsx` (214 líneas) y
`validarCubrir` en `movimientos/handler.go` (~450-517) más `fondos.go`.
Las pruebas encontraron cinco fallas en el mismo flujo:

1. Si el medio de origen elegido tampoco tiene plata, la ventana pasa a
   preguntar por **ese** medio y ofrece como origen el mismo medio del pago
   (cascada). En el pago de una deuda además ofrece usar dos veces los
   $90.000 de Efectivo. Solo se sale con Cancelar. No se guardó nada mal,
   pero el usuario queda atrapado. Capturas: `30_cascada.png`,
   `31_cascada2.png`, `32_rec_cascada.png` del reporte de pruebas.
2. Si se cambia de opción después de un primer intento fallido, la pantalla
   muestra un plan y se guarda otro (una vez con $80.000 de diferencia): el
   estado del componente guarda el `cubrir` anterior.
3. "Los pasé de otro medio" ofrece medios que están en $0 y a veces cuenta
   dos veces la misma plata.
4. Deja elegir "me lo prestó" **la misma persona** a la que se le está
   pagando.
5. Esto no es una falla pero pesa: la ventana no muestra cuánto hay en cada
   medio al elegir.

Qué hacer:

1. Backend manda en el 409 `falta_plata` la lista de medios con su saldo
   actual (ya manda cuánto hay y cuánto falta; agrega `medios: [{id,
   nombre, saldo}]`), excluyendo el medio del pago.
2. `CubrirFaltante` se vuelve **un solo paso, sin cascada**: el usuario ve
   los medios con saldo positivo (los de $0 no se ofrecen), reparte el
   faltante entre ellos con un input por medio (o "usar todo de este"), y
   la suma se valida en el cliente antes de mandar: no puede superar el
   saldo de cada uno ni quedarse corta. Si nada alcanza, las opciones son
   "me lo prestaron" (con `a_quien` ≠ la persona a la que se paga) o
   Cancelar.
3. Resetear el estado del componente cada vez que se abre y cada vez que
   cambia la opción: el `cubrir` que se envía se arma **en el submit** a
   partir del estado visible, nunca de una variable guardada de un intento
   anterior. Muestra un resumen textual "Van $X de Nequi y $Y de Efectivo"
   justo encima del botón, y que ese texto salga del mismo objeto que se
   envía.
4. Backend, `validarCubrir`: rechazar un origen igual al medio del pago,
   rechazar repetir el mismo medio dos veces, rechazar `a_quien` igual a la
   contraparte del movimiento, y verificar que la suma de las partes sea
   exactamente el faltante. La guardia de fondos ya evita el doble conteo
   final; estas validaciones son para que el mensaje sea claro y no un
   409 genérico.
5. Tests: unitarios de `validarCubrir` para los cuatro rechazos; uno de
   integración donde el origen tampoco alcanza → 409 con mensaje que lo
   diga, sin cascada.
6. Actualiza `docs/api.md` ("Ningún medio queda en negativo") con el nuevo
   formato de `falta_plata`.

Commit: "Cubrir un faltante es un solo paso: se ve cuánto hay en cada medio y no se enreda".
