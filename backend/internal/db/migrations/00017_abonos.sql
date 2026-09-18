-- +goose Up

-- Pagos parciales y acuerdos de pago.
--
-- Hasta aquí una deuda era todo o nada: 'pendiente' o 'pagado'. En la vida
-- real casi nunca es así — "te doy 50 esta semana y el resto el otro mes" —, y
-- sin poder anotar el abono la única salida era marcarlo pagado (y perder la
-- cuenta de lo que falta) o dejarlo pendiente (y que el dashboard siga
-- diciendo que te deben el total).

-- Cada plata que entra (o sale) a cuenta de una deuda.
CREATE TABLE abonos (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    -- CASCADE: un abono no existe sin su deuda. Borrar el préstamo borra sus
    -- abonos, que es lo correcto: son parte del mismo hecho, no un movimiento
    -- de plata independiente. En el resumen entran a través del préstamo.
    movimiento_id BIGINT NOT NULL REFERENCES movimientos(id) ON DELETE CASCADE,

    -- NUMERIC como todo el dinero de la app. Nunca float.
    monto NUMERIC(14,2) NOT NULL,
    fecha DATE NOT NULL,

    -- Por dónde entró (si te pagaron) o salió (si pagaste). RESTRICT igual
    -- que en movimientos: borrar un medio con abonos falla, para no perder
    -- el dato de por dónde se movió la plata.
    --
    -- Opcional: los abonos que salen de migrar los préstamos ya cobrados
    -- heredan el medio_cobro_id que tuvieran, y varios no tenían ninguno.
    medio_id BIGINT REFERENCES medios_pago(id) ON DELETE RESTRICT,

    nota TEXT NOT NULL DEFAULT '',

    creado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT abonos_monto_positivo CHECK (monto > 0)
);

-- La consulta de siempre: "los abonos de esta deuda, en orden".
CREATE INDEX abonos_movimiento_idx ON abonos (movimiento_id, fecha, id);
CREATE INDEX abonos_usuario_idx    ON abonos (usuario_id, fecha DESC);
CREATE INDEX abonos_medio_idx      ON abonos (medio_id);

-- El estado intermedio. 'parcial' = ya abonó algo pero falta.
--
-- No es un campo que el usuario escriba: lo recalcula la app cada vez que se
-- agrega o se borra un abono, dentro de la misma transacción.
ALTER TABLE movimientos DROP CONSTRAINT movimientos_estado_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_estado_valido
    CHECK (estado IS NULL OR estado IN ('pagado', 'pendiente', 'parcial'));

-- Los préstamos que YA estaban cobrados se convierten en un abono por el
-- total. Así queda un solo camino para "cuánto se ha devuelto de esto": la
-- suma de sus abonos. Sin esta migración habría dos fuentes de verdad (el
-- flag 'pagado' y la tabla) y tarde o temprano se contradicen.
INSERT INTO abonos (usuario_id, movimiento_id, monto, fecha, medio_id, nota)
SELECT usuario_id, id, monto, fecha, medio_cobro_id, 'Cobro registrado antes de los abonos'
FROM movimientos
WHERE tipo = 'preste' AND estado = 'pagado';

-- Y sueltan el medio_cobro_id, que a partir de aquí es SOLO el destino de un
-- traslado. Por dónde volvió un préstamo lo dice su abono.
UPDATE movimientos SET medio_cobro_id = NULL WHERE tipo <> 'traslado';

ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_medio_cobro_solo_traslado
    CHECK (medio_cobro_id IS NULL OR tipo = 'traslado');

-- --------------------------------------------------------------------------
-- El acuerdo de pago: en cuántas cuotas y para cuándo cada una.
-- --------------------------------------------------------------------------
--
-- Las cuotas son el CALENDARIO, no la plata. Lo que se debe de verdad sale de
-- monto - suma de abonos; las cuotas solo dicen para cuándo se había quedado
-- de pagar cada pedazo, que es lo que necesitan los avisos.
--
-- Se guardan una por una (y no como "6 cuotas cada 30 días") porque un acuerdo
-- real se corre: la tercera se pasa para el 15 y las demás siguen igual. Con
-- una regla calculada no hay dónde anotar esa excepción.
CREATE TABLE cuotas (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    usuario_id    BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    movimiento_id BIGINT NOT NULL REFERENCES movimientos(id) ON DELETE CASCADE,

    numero   INT           NOT NULL,
    vence_el DATE          NOT NULL,
    monto    NUMERIC(14,2) NOT NULL,

    creada_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cuotas_monto_positivo CHECK (monto > 0),
    CONSTRAINT cuotas_numero_positivo CHECK (numero > 0)
);

-- No puede haber dos cuotas número 3 del mismo préstamo.
CREATE UNIQUE INDEX cuotas_movimiento_numero_key ON cuotas (movimiento_id, numero);

-- La consulta de los avisos: "¿qué cuotas ya vencieron?".
CREATE INDEX cuotas_vencimiento_idx ON cuotas (usuario_id, vence_el);

-- --------------------------------------------------------------------------
-- Los avisos nuevos que salen de todo esto.
-- --------------------------------------------------------------------------
ALTER TABLE notificaciones DROP CONSTRAINT notificaciones_tipo_valido;
ALTER TABLE notificaciones ADD CONSTRAINT notificaciones_tipo_valido
    CHECK (tipo IN (
        'resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes', 'cobro_del_dia',
        -- Una cuota del acuerdo llegó a su fecha y todavía no está cubierta.
        'cuota_vencida',
        -- El día en que TÚ quedaste de pagar algo que te prestaron.
        'pago_del_dia',
        -- Llevas tiempo debiendo.
        'deudas_propias'
    ));

-- +goose Down
ALTER TABLE notificaciones DROP CONSTRAINT notificaciones_tipo_valido;
DELETE FROM notificaciones WHERE tipo IN ('cuota_vencida', 'pago_del_dia', 'deudas_propias');
ALTER TABLE notificaciones ADD CONSTRAINT notificaciones_tipo_valido
    CHECK (tipo IN ('resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes', 'cobro_del_dia'));

DROP TABLE cuotas;

-- Se devuelve el medio de cobro al préstamo desde su último abono, y los
-- parciales vuelven a 'pendiente': el esquema viejo no sabe decir "a medias".
UPDATE movimientos m
SET medio_cobro_id = (
        SELECT a.medio_id FROM abonos a
        WHERE a.movimiento_id = m.id AND a.medio_id IS NOT NULL
        ORDER BY a.fecha DESC, a.id DESC LIMIT 1
    )
WHERE m.tipo = 'preste' AND m.estado = 'pagado';

UPDATE movimientos SET estado = 'pendiente' WHERE estado = 'parcial';

ALTER TABLE movimientos DROP CONSTRAINT movimientos_medio_cobro_solo_traslado;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_medio_cobro_valido CHECK (
    medio_cobro_id IS NULL
    OR (tipo IN ('preste', 'me_prestaron') AND estado = 'pagado')
    OR (tipo = 'traslado')
);

ALTER TABLE movimientos DROP CONSTRAINT movimientos_estado_valido;
ALTER TABLE movimientos ADD CONSTRAINT movimientos_estado_valido
    CHECK (estado IS NULL OR estado IN ('pagado', 'pendiente'));

DROP TABLE abonos;
