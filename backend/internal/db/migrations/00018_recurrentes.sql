-- +goose Up

-- Los gastos (y los ingresos) que se repiten: el arriendo, el internet, la
-- cuota del local, el sueldo.
--
-- La decisión de fondo: la app NO crea el movimiento sola. Cada vez que toca,
-- deja una OCURRENCIA pendiente y un aviso, y el usuario confirma con un clic
-- (pudiendo corregir el monto antes, que es lo que pasa con el recibo de la
-- luz todos los meses).
--
-- Por qué no automático: si el arriendo se registra solo el día 1 y ese mes no
-- se pagó, o se pagó distinto, el balance queda mal sin que nadie se entere.
-- Un gasto inventado es peor que un gasto olvidado — el olvidado se nota, el
-- inventado no.
CREATE TABLE recurrentes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    -- RESTRICT como en movimientos: borrar una categoría con recurrentes
    -- falla, para no dejar plantillas apuntando a la nada.
    categoria_id  BIGINT NOT NULL REFERENCES categorias(id)  ON DELETE RESTRICT,
    medio_pago_id BIGINT NOT NULL REFERENCES medios_pago(id) ON DELETE RESTRICT,

    -- Solo gastos e ingresos. Un préstamo recurrente no existe: cada préstamo
    -- es un acuerdo distinto, con su fecha y su persona.
    tipo        TEXT          NOT NULL,
    monto       NUMERIC(14,2) NOT NULL,
    descripcion TEXT          NOT NULL DEFAULT '',

    -- Cada cuánto y en qué día:
    --   mensual   -> dia = día del mes (1..31; si el mes no lo tiene, el último)
    --   quincenal -> dia = primer día del mes; la segunda cae 15 días después
    --   semanal   -> dia = día de la semana (1 = lunes ... 7 = domingo)
    frecuencia TEXT NOT NULL,
    dia        INT  NOT NULL,

    -- Desde cuándo aplica y hasta cuándo (hasta NULL = sin fecha de fin).
    desde DATE NOT NULL,
    hasta DATE,

    -- Pausar sin borrar: el historial de ocurrencias se conserva.
    activo BOOLEAN NOT NULL DEFAULT true,

    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT recurrentes_tipo_valido       CHECK (tipo IN ('recibi', 'pague')),
    CONSTRAINT recurrentes_monto_positivo    CHECK (monto > 0),
    CONSTRAINT recurrentes_frecuencia_valida CHECK (frecuencia IN ('mensual', 'quincenal', 'semanal')),
    CONSTRAINT recurrentes_hasta_coherente   CHECK (hasta IS NULL OR hasta >= desde),

    -- El rango del día depende de la frecuencia, y la base lo sabe: un
    -- "semanal el día 23" no significa nada.
    CONSTRAINT recurrentes_dia_valido CHECK (
        (frecuencia IN ('mensual', 'quincenal') AND dia BETWEEN 1 AND 31)
        OR (frecuencia = 'semanal' AND dia BETWEEN 1 AND 7)
    )
);

CREATE INDEX recurrentes_usuario_idx ON recurrentes (usuario_id, activo);

-- Cada vez que a un recurrente le toca, nace una ocurrencia pendiente.
CREATE TABLE recurrentes_ocurrencias (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    usuario_id    BIGINT NOT NULL REFERENCES usuarios(id)   ON DELETE CASCADE,
    recurrente_id BIGINT NOT NULL REFERENCES recurrentes(id) ON DELETE CASCADE,

    -- El día que le tocaba. Es la mitad de la clave: junto con recurrente_id
    -- es lo que impide que la tarea genere dos veces lo mismo.
    fecha DATE NOT NULL,

    estado TEXT NOT NULL DEFAULT 'pendiente',

    -- El movimiento que salió de confirmarla. SET NULL y no CASCADE: si el
    -- usuario borra el movimiento, la ocurrencia sigue existiendo como
    -- "esto ya lo resolví" y la tarea no la vuelve a proponer.
    movimiento_id BIGINT REFERENCES movimientos(id) ON DELETE SET NULL,

    creada_en   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resuelta_en TIMESTAMPTZ,

    CONSTRAINT recurrentes_ocurrencias_estado_valido
        CHECK (estado IN ('pendiente', 'confirmada', 'descartada')),

    CONSTRAINT recurrentes_ocurrencias_resuelta_coherente CHECK (
        (estado = 'pendiente' AND resuelta_en IS NULL)
        OR (estado <> 'pendiente' AND resuelta_en IS NOT NULL)
    )
);

-- ESTA es la regla de idempotencia: una ocurrencia por recurrente y fecha.
-- La tarea puede correr cada hora sin proponer el arriendo veinte veces.
CREATE UNIQUE INDEX recurrentes_ocurrencias_key
    ON recurrentes_ocurrencias (recurrente_id, fecha);

-- Lo que pinta la app: "¿qué tengo por confirmar?".
CREATE INDEX recurrentes_ocurrencias_pendientes_idx
    ON recurrentes_ocurrencias (usuario_id, fecha)
    WHERE estado = 'pendiente';

ALTER TABLE notificaciones DROP CONSTRAINT notificaciones_tipo_valido;
ALTER TABLE notificaciones ADD CONSTRAINT notificaciones_tipo_valido
    CHECK (tipo IN (
        'resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes', 'cobro_del_dia',
        'cuota_vencida', 'pago_del_dia', 'deudas_propias',
        -- Toca el arriendo: confírmalo o córrelo.
        'recurrente_pendiente'
    ));

-- +goose Down
ALTER TABLE notificaciones DROP CONSTRAINT notificaciones_tipo_valido;
DELETE FROM notificaciones WHERE tipo = 'recurrente_pendiente';
ALTER TABLE notificaciones ADD CONSTRAINT notificaciones_tipo_valido
    CHECK (tipo IN (
        'resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes', 'cobro_del_dia',
        'cuota_vencida', 'pago_del_dia', 'deudas_propias'
    ));

DROP TABLE recurrentes_ocurrencias;
DROP TABLE recurrentes;
