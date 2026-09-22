# 37 · Recurrentes: tres detalles

Contexto, de las pruebas manuales sobre `recurrentes/`:

1. "Desde" es obligatorio en el formulario, pero si se deja vacío el backend
   lo guarda con la fecha de hoy sin avisar.
2. Al borrar una categoría o un medio que solo usa un **recurrente** (sin
   movimientos), el 409 dice "tiene movimientos". El `RESTRICT` viene de la
   FK de `recurrentes`, pero el handler de `categorias`/`medios` asume que
   fue la de `movimientos`.
3. Un recurrente **pausado** sigue mostrando sus pendientes pasados en "Por
   confirmar". Puede ser intencional; hay que decidirlo.

Decisión para el 3: pausado significa "no me lo recuerdes": los pendientes
generados antes de pausar se quedan (son plata que quizá salió) pero no se
generan nuevos y la sección los muestra agrupados bajo "Pausados" con
opción de omitirlos todos de una vez.

Qué hacer:

1. `recurrentes/handler.go`: `desde` obligatorio (error de campo) en crear;
   en editar, si viene vacío se conserva el anterior. Frontend igual.
2. `categorias/handler.go` y `medios/handler.go`: al capturar la violación
   de FK, mirar el nombre de la constraint (`pgconn.PgError.ConstraintName`)
   y responder "tiene movimientos" o "la usa un gasto recurrente" según
   corresponda. Test de integración para cada caso.
3. "Por confirmar": los pendientes de recurrentes pausados van en un grupo
   aparte con el rótulo "Pausado" y un botón "Omitir todos". Al reactivar,
   vuelven al grupo normal. Documentar en `docs/api.md` (Gastos
   recurrentes).
4. Si se hizo el prompt 08, revisar que crear un recurrente o cambiarle la
   fecha al pasado invalide `recurrentes` y `pendientes` sin recargar.

Commit: "Recurrentes: 'Desde' es obligatorio de verdad, y borrar avisa qué lo usa".
