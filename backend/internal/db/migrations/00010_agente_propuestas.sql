-- +goose Up

-- Lo que el agente PREPARA pero no ejecuta.
--
-- El modelo no escribe en la base: deja una propuesta, la app la muestra en
-- una tarjeta con los campos editables y solo cuando el usuario confirma se
-- crea el movimiento. Un modelo de lenguaje oye "cuarenta y cinco mil" y
-- escribe 45.000 casi siempre; ese "casi" es la razon de que esta tabla
-- exista.
CREATE TABLE agente_propuestas (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id      BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    conversacion_id BIGINT NOT NULL REFERENCES agente_conversaciones(id) ON DELETE CASCADE,

    tipo TEXT NOT NULL,

    -- Lo que pinta la tarjeta: ids y nombres ya resueltos contra los datos del
    -- usuario. Es jsonb y no columnas porque cada tipo de propuesta lleva sus
    -- campos, y la unica que los interpreta es la app.
    datos JSONB NOT NULL,

    estado      TEXT NOT NULL DEFAULT 'pendiente',
    creada_en   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resuelta_en TIMESTAMPTZ,

    -- Que movimiento nacio de esta propuesta. SET NULL: si el movimiento se
    -- borra, la propuesta sigue contando que un dia se confirmo.
    movimiento_id BIGINT REFERENCES movimientos(id) ON DELETE SET NULL,

    CONSTRAINT agente_propuestas_tipo_valido
        CHECK (tipo IN ('movimiento', 'marcar_pagado')),

    CONSTRAINT agente_propuestas_estado_valido
        CHECK (estado IN ('pendiente', 'confirmada', 'descartada')),

    -- Una propuesta resuelta tiene fecha de resolucion y una pendiente no.
    -- En Go seria facil olvidarlo en una rama; aqui no hay forma.
    CONSTRAINT agente_propuestas_resuelta_con_fecha
        CHECK ((estado =  'pendiente' AND resuelta_en IS NULL)
            OR (estado <> 'pendiente' AND resuelta_en IS NOT NULL))
);

-- La consulta de cada carga del chat: "¿que le quedo pendiente de confirmar?".
CREATE INDEX agente_propuestas_pendientes_idx
    ON agente_propuestas (usuario_id, estado, creada_en DESC);

-- +goose Down
DROP TABLE agente_propuestas;
