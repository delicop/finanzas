-- +goose Up

-- El otro lado del préstamo: cuando el que queda debiendo eres tú.
--
-- Hasta aquí la app solo sabía de 'preste' (sale plata y alguien la debe). Un
-- negocio que te presta, un socio que te adelanta el arriendo — nada de eso
-- se podía registrar sin mentirle a los totales llamándolo "recibí".
--
--   preste        -> salió plata, TE LA DEBEN
--   me_prestaron  -> entró plata, TÚ LA DEBES
--
-- Son simétricos y comparten todo: a_quien, estado, cobrar_el y los abonos.
-- Lo único que cambia es el signo con que entran al balance.
ALTER TABLE movimientos DROP CONSTRAINT movimientos_tipo_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_tipo_valido
    CHECK (tipo IN ('recibi', 'pague', 'preste', 'me_prestaron', 'traslado'));

-- a_quien y estado ahora aplican a los DOS tipos de deuda. El nombre de la
-- restricción cambia porque ya no es "solo presté".
ALTER TABLE movimientos DROP CONSTRAINT movimientos_preste_completo;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_deuda_completa CHECK (
    (tipo IN ('preste', 'me_prestaron')
        AND a_quien IS NOT NULL AND btrim(a_quien) <> ''
        AND estado IS NOT NULL)
    OR (tipo NOT IN ('preste', 'me_prestaron')
        AND a_quien IS NULL AND estado IS NULL)
);

ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_medio_cobro_valido CHECK (
    medio_cobro_id IS NULL
    OR (tipo IN ('preste', 'me_prestaron') AND estado = 'pagado')
    OR (tipo = 'traslado')
);

-- La fecha acordada sirve igual en los dos sentidos: el día que te pagan y el
-- día que te toca pagar.
ALTER TABLE movimientos DROP CONSTRAINT movimientos_cobrar_el_solo_prestamo;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_cobrar_el_solo_deuda
    CHECK (cobrar_el IS NULL OR tipo IN ('preste', 'me_prestaron'));

-- Los índices parciales del dashboard y de los avisos tienen que ver los dos
-- tipos; un índice que solo mira 'preste' deja la mitad de las deudas fuera.
DROP INDEX movimientos_pendientes_idx;
CREATE INDEX movimientos_pendientes_idx
    ON movimientos (usuario_id, tipo)
    WHERE tipo IN ('preste', 'me_prestaron') AND estado <> 'pagado';

DROP INDEX movimientos_por_cobrar_idx;
CREATE INDEX movimientos_por_cobrar_idx
    ON movimientos (usuario_id, cobrar_el)
    WHERE tipo IN ('preste', 'me_prestaron') AND estado <> 'pagado' AND cobrar_el IS NOT NULL;

-- +goose Down
DELETE FROM movimientos WHERE tipo = 'me_prestaron';

DROP INDEX movimientos_por_cobrar_idx;
CREATE INDEX movimientos_por_cobrar_idx
    ON movimientos (usuario_id, cobrar_el)
    WHERE tipo = 'preste' AND estado = 'pendiente' AND cobrar_el IS NOT NULL;

DROP INDEX movimientos_pendientes_idx;
CREATE INDEX movimientos_pendientes_idx
    ON movimientos (usuario_id)
    WHERE tipo = 'preste' AND estado = 'pendiente';

ALTER TABLE movimientos DROP CONSTRAINT movimientos_cobrar_el_solo_deuda;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_cobrar_el_solo_prestamo
    CHECK (cobrar_el IS NULL OR tipo = 'preste');

ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_medio_cobro_valido CHECK (
    medio_cobro_id IS NULL
    OR (tipo = 'preste' AND estado = 'pagado')
    OR (tipo = 'traslado')
);

ALTER TABLE movimientos DROP CONSTRAINT movimientos_deuda_completa;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_preste_completo CHECK (
    (tipo = 'preste'  AND a_quien IS NOT NULL AND btrim(a_quien) <> '' AND estado IS NOT NULL)
 OR (tipo <> 'preste' AND a_quien IS NULL AND estado IS NULL)
);

ALTER TABLE movimientos DROP CONSTRAINT movimientos_tipo_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_tipo_valido
    CHECK (tipo IN ('recibi', 'pague', 'preste', 'traslado'));
