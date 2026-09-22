# 30 · Los movimientos con fecha futura no cuentan en "Tienes"

Contexto: `flujosSQL` en `movimientos/fondos.go` (~44-95) y el resumen
(`resumen.go`) suman todo sin mirar la fecha. Un ingreso registrado para
diciembre ya cuenta hoy: se puede gastar en septiembre plata que no ha
llegado. Además los traslados automáticos que arma "cubrir faltante"
llevan la fecha del gasto: un gasto del 01/08 cubierto con plata que entró
el 21/09 deja a Transferencia en negativo si se mira al 01/08, aunque la
guardia global no lo vea.

Decisión tomada: **"Tienes" y la guardia de fondos solo cuentan hasta hoy.**
Lo futuro es "programado": se ve, se lista, pero no es plata.

Qué hacer:

1. `flujosSQL` y las sumas de saldo por medio del resumen filtran
   `m.fecha <= $hoy` (hoy en Colombia, `HoyEnColombia()` ya existe). Pasa
   la fecha como parámetro, no `CURRENT_DATE`, para que los tests puedan
   fijarla.
2. Registrar un **gasto** con fecha futura no pasa por la guardia de fondos
   (todavía no sale nada), pero sí se valida al llegar el día: la tarea de
   fondo de avisos (o una nueva de "programados") revisa cada mañana los
   movimientos cuya fecha acaba de llegar y, si dejan un medio en rojo,
   crea un aviso "Hoy sale X de Nequi y no alcanza" en vez de bloquear.
   Si prefieres lo simple: prohibir gastos con fecha futura y dejar solo
   ingresos. Escoge lo primero; los recurrentes ya cubren "lo que viene".
3. Traslado automático de "cubrir faltante": su fecha es la del gasto que
   cubre. Mantén eso (la plata salió ese día), pero el saldo por fecha ya no
   importa porque solo se mira hasta hoy. Documenta el detalle.
4. Resumen y lista: los movimientos futuros llevan una marca visible
   "Programado" (chip discreto) y no entran en "Recibido"/"Pagado" del
   periodo hasta que llegue su fecha. Que el dashboard muestre una línea
   "Programado: +$X / −$Y" si hay alguno.
5. Tests de integración con fecha fija: un ingreso de mañana no sube
   "Tienes" hoy; un gasto hoy que solo se cubre con ese ingreso → 409.
6. `docs/api.md` (Resumen y "Ningún medio queda en negativo") y
   `docs/decisiones.md`.

Commit: "La plata que llega mañana no se puede gastar hoy".
