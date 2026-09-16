-- +goose Up

-- Los avisos que la app le deja al usuario: su resumen de la semana, los
-- prestamos que lleva meses sin cobrar, y al dueno del servidor a quien le
-- falta cobrarle el mes.
CREATE TABLE notificaciones (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    tipo   TEXT NOT NULL,
    titulo TEXT NOT NULL,
    cuerpo TEXT NOT NULL,

    -- El periodo al que corresponde el aviso: "2026-W38" para el resumen
    -- semanal, "2026-09" para los mensuales.
    --
    -- Junto con el indice unico de abajo, es lo que hace que la tarea pueda
    -- correr cuantas veces quiera sin repetir un aviso. Es idempotencia por
    -- construccion: mucho mas confiable que guardar "la ultima vez que corri",
    -- que se desincroniza en cuanto el servidor se reinicia a destiempo.
    clave TEXT NOT NULL,

    leida_en  TIMESTAMPTZ,
    creada_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT notificaciones_tipo_valido
        CHECK (tipo IN ('resumen_semanal', 'prestamos_pendientes', 'cobros_del_mes'))
);

-- Un aviso por usuario, tipo y periodo. Este indice ES la regla.
CREATE UNIQUE INDEX notificaciones_clave_key
    ON notificaciones (usuario_id, tipo, clave);

-- La consulta de cada carga de la app: "¿cuantas sin leer tiene?".
CREATE INDEX notificaciones_usuario_idx
    ON notificaciones (usuario_id, creada_en DESC);

-- +goose Down
DROP TABLE notificaciones;
