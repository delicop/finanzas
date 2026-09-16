-- +goose Up

-- Por donde te DEVOLVIERON un prestamo.
--
-- Es un dato distinto de medio_pago_id: prestas en efectivo y te pueden pagar
-- por transferencia. Sin esta columna no habria forma de saber que la plata
-- salio de un lado y volvio a otro, y el saldo de cada medio quedaria mal.
--
--   medio_pago_id  -> por donde SALIO la plata al prestarla
--   medio_cobro_id -> por donde VOLVIO cuando te pagaron
ALTER TABLE movimientos
    ADD COLUMN medio_cobro_id BIGINT REFERENCES medios_pago(id) ON DELETE RESTRICT;

CREATE INDEX movimientos_medio_cobro_idx ON movimientos (medio_cobro_id);

-- Solo tiene sentido en un prestamo ya cobrado. Si un movimiento vuelve a
-- "pendiente", la capa Go limpia esta columna y el CHECK lo garantiza.
ALTER TABLE movimientos
    ADD CONSTRAINT movimientos_medio_cobro_solo_pagado CHECK (
        medio_cobro_id IS NULL
        OR (tipo = 'preste' AND estado = 'pagado')
    );

-- +goose Down
ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_solo_pagado;
ALTER TABLE movimientos DROP COLUMN medio_cobro_id;
