-- +goose Up

-- El asistente ya puede preparar un abono.
--
-- Hasta aquí solo sabía dos cosas: registrar un movimiento nuevo y dar por
-- cobrado un préstamo entero. "Carlos me abonó 50 mil" no tenía dónde caer, y
-- la única salida era marcar el préstamo completo como pagado — que es
-- justamente el error que los pagos parciales vienen a evitar.
ALTER TABLE agente_propuestas DROP CONSTRAINT agente_propuestas_tipo_valido;
ALTER TABLE agente_propuestas ADD CONSTRAINT agente_propuestas_tipo_valido
    CHECK (tipo IN ('movimiento', 'marcar_pagado', 'abono'));

-- +goose Down
DELETE FROM agente_propuestas WHERE tipo = 'abono';
ALTER TABLE agente_propuestas DROP CONSTRAINT agente_propuestas_tipo_valido;
ALTER TABLE agente_propuestas ADD CONSTRAINT agente_propuestas_tipo_valido
    CHECK (tipo IN ('movimiento', 'marcar_pagado'));
