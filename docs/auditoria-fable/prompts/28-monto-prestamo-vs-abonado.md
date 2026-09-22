# 28 · No se puede bajar el monto de un préstamo por debajo de lo ya abonado (ALTA)

Contexto: al editar un `preste` o `me_prestaron` que ya tiene abonos, el
backend acepta un monto menor a la suma de los abonos. Entonces el saldo
queda negativo y "Te deben" y "Recuperado" en el resumen salen mal. El
saldo es "la única verdad" según `docs/api.md` (Abonos y acuerdo de pago),
así que un saldo negativo rompe todo lo que cuelga de él.

Qué hacer:

1. En `movimientos/store.go` (`Actualizar`), dentro de la misma transacción
   y con el `FOR UPDATE` que ya usan los abonos: leer `SUM(abonos.monto)`
   del movimiento y rechazar si `nuevo_monto < abonado` con un error de
   dominio nuevo `ErrMontoMenorQueAbonado{Abonado}`.
2. El handler lo mapea a 409 con mensaje claro: "Ya te abonaron $X; el
   monto no puede ser menor. Borra abonos primero si te equivocaste".
3. Un `CHECK` no sirve (mira una fila), así que además agrega la regla al
   borrado de abonos por si algún día se permite reordenar: el saldo
   siempre es `monto − abonado ≥ 0`. Documenta en `docs/decisiones.md` por
   qué vive en Go y no en la base, igual que "al menos un admin".
4. Si el monto baja **exactamente** a lo abonado, el estado pasa a `pagado`
   solo; si sube, un `pagado` vuelve a `parcial`. Revisa que la
   recalculación de estado ya exista en `abonos.go` y reutilízala.
5. Frontend, `MovimientoForm.jsx`: mostrar el error del backend bajo el
   campo monto; opcional, un texto de ayuda "abonado hasta ahora: $X"
   cuando se edita un préstamo con abonos.
6. Test de integración: préstamo de 100 con abono de 60, editar a 50 → 409;
   editar a 60 → estado `pagado`; editar a 80 → `parcial` con saldo 20.

Commit: "Un préstamo no puede quedar por debajo de lo que ya te pagaron".
