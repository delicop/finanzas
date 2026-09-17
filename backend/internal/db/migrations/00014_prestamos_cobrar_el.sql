-- +goose Up

-- Cuándo quedó de pagar quien se llevó la plata.
--
-- Es opcional: no todo préstamo tiene fecha ("me lo devuelve cuando pueda").
-- Cuando la tiene, ese día la app avisa (notificacion 'cobro_del_dia').
ALTER TABLE movimientos ADD COLUMN cobrar_el DATE;

-- Como a_quien y estado, solo existe para los préstamos. Y no puede ser antes
-- del día en que se prestó: sería un error de digitación, no un acuerdo.
ALTER TABLE movimientos
    ADD CONSTRAINT movimientos_cobrar_el_solo_prestamo
        CHECK (cobrar_el IS NULL OR tipo = 'preste'),
    ADD CONSTRAINT movimientos_cobrar_el_despues_del_prestamo
        CHECK (cobrar_el IS NULL OR cobrar_el >= fecha);

-- La consulta de los avisos: "¿qué préstamos pendientes vencen hoy?".
CREATE INDEX movimientos_por_cobrar_idx
    ON movimientos (usuario_id, cobrar_el)
    WHERE tipo = 'preste' AND estado = 'pendiente' AND cobrar_el IS NOT NULL;

ALTER TABLE notificaciones DROP CONSTRAINT notificaciones_tipo_valido;
ALTER TABLE notificaciones ADD CONSTRAINT notificaciones_tipo_valido
    CHECK (tipo IN ('resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes', 'cobro_del_dia'));

-- +goose Down
DELETE FROM notificaciones WHERE tipo = 'cobro_del_dia';
ALTER TABLE notificaciones DROP CONSTRAINT notificaciones_tipo_valido;
ALTER TABLE notificaciones ADD CONSTRAINT notificaciones_tipo_valido
    CHECK (tipo IN ('resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes'));

DROP INDEX movimientos_por_cobrar_idx;
ALTER TABLE movimientos
    DROP CONSTRAINT movimientos_cobrar_el_despues_del_prestamo,
    DROP CONSTRAINT movimientos_cobrar_el_solo_prestamo,
    DROP COLUMN cobrar_el;
