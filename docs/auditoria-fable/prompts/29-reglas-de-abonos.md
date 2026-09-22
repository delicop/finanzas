# 29 · Reglas de los abonos: fecha, signo y mensajes

Contexto: en `ValidarAbono` (`movimientos/handler_abonos.go` ~98-128) se
aceptan tres cosas que no deberían pasar:

- Un abono con fecha **anterior** a la del préstamo.
- Un monto **negativo** se guarda como positivo sin avisar (`dinero.Normalizar`
  o el frontend le quita el signo).
- Confirmar con monto 0 responde el genérico "Datos inválidos" en vez de
  decir qué campo.

Qué hacer:

1. Fecha: `ValidarAbono` recibe también la fecha del movimiento (el store
   ya lo carga con `FOR UPDATE`) y rechaza `fecha < fecha_prestamo` con
   "El abono no puede ser anterior al préstamo (DD/MM/AAAA)". Lo mismo en
   `ValidarAcuerdo` para la primera cuota.
2. Signo: un monto negativo o cero es error de campo ("El monto debe ser
   mayor que cero"), nunca se normaliza. Revisa `InputMonto` y
   `entradaAMonto` en `lib/formato.js`: si el componente quita el "−", que
   no lo haga; que lo rechace.
3. Mensajes: recorre los `httpx.Error(w, 400, "Datos inválidos")` del
   backend y cámbialos por `httpx.ErrorCampos` (o el helper que exista)
   con el campo y la razón. El frontend ya sabe pintar `campos`
   (`ApiError.campos`).
4. Tests unitarios de `ValidarAbono` para los tres casos, y uno en
   `formato.test.js` (prompt 07) de que `entradaAMonto('-5')` no devuelve 5.

Commit: "Un abono no puede ser anterior al préstamo ni tener monto negativo".
