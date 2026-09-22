# 05 · Borrar un abono debe respetar el movimiento de la URL

Contexto: la ruta es `DELETE /api/movimientos/{id}/abonos/{abonoID}`, pero en
`backend/internal/movimientos/handler_abonos.go` (`BorrarAbono`, ~líneas
80-92) solo se lee `abonoID`. El store borra cualquier abono del usuario
aunque el `{id}` de la URL apunte a otro movimiento. No hay fuga entre
usuarios (el store filtra por `usuario_id`), pero la URL miente y un bug
del frontend podría borrar el abono equivocado.

Qué hacer:

1. Lee también `{id}` y pásalo a `store.BorrarAbono(ctx, usuarioID,
   movimientoID, abonoID)`.
2. En el SQL, exige `movimiento_id = $2 AND id = $3`. Si no coincide,
   devuelve el mismo `ErrAbonoNoEncontrado` (o el que exista) → 404.
3. Agrega un caso a `integracion_test.go` (o al test de abonos que exista):
   borrar un abono con el id de otro movimiento debe dar 404 y no borrar nada.
4. Revisa que `ModalAbonos.jsx` ya manda el id correcto (debería).

Verifica con `TEST_DATABASE_URL=... go test ./internal/movimientos/...`
(ver `docs/pruebas.md`).

Commit: "Borrar un abono exige que sea del movimiento de la URL".
