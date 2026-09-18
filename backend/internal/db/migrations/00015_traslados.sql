-- +goose Up

-- Pasar plata de un medio a otro: del efectivo a la cuenta, de Nequi al banco.
--
-- Es UN solo movimiento, no dos. Con dos ("pagué" en el origen y "recibí" en
-- el destino) los totales de recibido y pagado se inflarían con plata que
-- nunca entró ni salió de verdad, y habría que mantener las dos filas
-- sincronizadas al editar o borrar.
--
-- Las columnas que ya existen dicen exactamente lo que hace falta:
--
--   medio_pago_id  -> de dónde SALE
--   medio_cobro_id -> a dónde ENTRA
--
-- Un traslado no cambia cuánta plata tienes, solo dónde está. Por eso no entra
-- en la fórmula del balance (ver resumen.go): mueve los dos saldos por medio
-- y el total queda igual.
ALTER TABLE movimientos DROP CONSTRAINT movimientos_tipo_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_tipo_valido
    CHECK (tipo IN ('recibi', 'pague', 'preste', 'traslado'));

-- Hasta ahora medio_cobro_id solo existía en un préstamo cobrado. Ahora
-- también es el destino de un traslado.
ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_solo_pagado;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_medio_cobro_valido CHECK (
    medio_cobro_id IS NULL
    OR (tipo = 'preste'   AND estado = 'pagado')
    OR (tipo = 'traslado')
);

-- Un traslado sin origen o sin destino no es un traslado, y uno "de Nequi a
-- Nequi" es un error de digitación que dejaría el saldo igual y la lista
-- llena de ruido. Las dos reglas viven en la base, no solo en Go.
ALTER TABLE movimientos ADD CONSTRAINT movimientos_traslado_completo CHECK (
    tipo <> 'traslado'
    OR (medio_pago_id IS NOT NULL
        AND medio_cobro_id IS NOT NULL
        AND medio_pago_id <> medio_cobro_id)
);

-- +goose Down
DELETE FROM movimientos WHERE tipo = 'traslado';

ALTER TABLE movimientos DROP CONSTRAINT movimientos_traslado_completo;
ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_medio_cobro_solo_pagado CHECK (
    medio_cobro_id IS NULL
    OR (tipo = 'preste' AND estado = 'pagado')
);

ALTER TABLE movimientos DROP CONSTRAINT movimientos_tipo_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_tipo_valido
    CHECK (tipo IN ('recibi', 'pague', 'preste'));
