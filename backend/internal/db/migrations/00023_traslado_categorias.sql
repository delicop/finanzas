-- +goose Up

-- Un traslado ahora también puede mover plata de una CATEGORÍA a otra: de
-- "Casa" a "Trabajo", con o sin cambiar de medio.
--
--   categoria_id         -> de qué categoría SALE
--   categoria_destino_id -> a qué categoría ENTRA (NULL = la misma)
--
-- Sigue siendo un solo movimiento y sigue sin cambiar el balance general: lo
-- que una categoría pierde, la otra lo gana. Lo que sí cambia es el balance de
-- cada categoría (ver resumen.go).
ALTER TABLE movimientos
    ADD COLUMN categoria_destino_id BIGINT REFERENCES categorias(id) ON DELETE RESTRICT;

CREATE INDEX movimientos_categoria_destino_idx ON movimientos (categoria_destino_id);

-- Solo un traslado tiene categoría destino, y nunca es la misma de origen:
-- "de Casa a Casa" se guarda como NULL, que dice lo mismo sin dos formas de
-- decirlo.
ALTER TABLE movimientos ADD CONSTRAINT movimientos_categoria_destino_valida CHECK (
    categoria_destino_id IS NULL
    OR (tipo = 'traslado' AND categoria_destino_id <> categoria_id)
);

-- Antes el destino tenía que ser otro medio. Ahora basta con que cambie algo:
-- el medio, la categoría o las dos. "De Nequi a Nequi" sin cambiar de
-- categoría sigue siendo un error de digitación.
ALTER TABLE movimientos DROP CONSTRAINT movimientos_traslado_completo;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_traslado_completo CHECK (
    tipo <> 'traslado'
    OR (medio_pago_id IS NOT NULL
        AND medio_cobro_id IS NOT NULL
        AND (medio_pago_id <> medio_cobro_id OR categoria_destino_id IS NOT NULL))
);

-- +goose Down
DELETE FROM movimientos WHERE tipo = 'traslado' AND medio_pago_id = medio_cobro_id;

ALTER TABLE movimientos DROP CONSTRAINT movimientos_traslado_completo;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_traslado_completo CHECK (
    tipo <> 'traslado'
    OR (medio_pago_id IS NOT NULL
        AND medio_cobro_id IS NOT NULL
        AND medio_pago_id <> medio_cobro_id)
);

ALTER TABLE movimientos DROP CONSTRAINT movimientos_categoria_destino_valida;
DROP INDEX movimientos_categoria_destino_idx;
ALTER TABLE movimientos DROP COLUMN categoria_destino_id;
