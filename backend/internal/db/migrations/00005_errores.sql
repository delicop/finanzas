-- +goose Up

-- Bitacora de errores del servidor.
--
-- Por que en la base de datos y no solo en los logs de Docker: la app va a
-- vivir en una Raspberry. Entrar por SSH a leer "docker compose logs" desde
-- el celular no es practico, y los logs se pierden al reconstruir la imagen.
-- Aqui quedan guardados y se leen desde la propia app.
--
-- Solo se registran los errores 500 (fallas del servidor) y los panicos.
-- Los 4xx son errores del usuario (un monto mal escrito) y no son fallas.
CREATE TABLE errores (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ocurrido_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Donde paso: "movimientos: creando", "factura: guardando archivo"...
    contexto TEXT NOT NULL,
    -- El error tecnico real. No se le muestra al usuario final en el momento
    -- de la falla, pero aqui si lo necesitamos para poder arreglarlo.
    mensaje TEXT NOT NULL,

    ruta       TEXT NOT NULL DEFAULT '',
    metodo     TEXT NOT NULL DEFAULT '',
    -- El id que chi le pone a cada peticion: permite cruzar esta fila con
    -- la linea correspondiente en los logs de Docker.
    request_id TEXT NOT NULL DEFAULT '',

    -- ON DELETE SET NULL: si se borra el usuario, el error no se pierde.
    usuario_id BIGINT REFERENCES usuarios(id) ON DELETE SET NULL,

    -- Un panico es mas grave que un error normal: se guarda la traza.
    es_panico BOOLEAN NOT NULL DEFAULT false,
    traza     TEXT    NOT NULL DEFAULT '',

    resuelto    BOOLEAN NOT NULL DEFAULT false,
    resuelto_en TIMESTAMPTZ,

    CONSTRAINT errores_resuelto_coherente CHECK (
        (resuelto AND resuelto_en IS NOT NULL) OR (NOT resuelto AND resuelto_en IS NULL)
    )
);

-- Indice parcial: lo que se consulta casi siempre son los pendientes.
CREATE INDEX errores_pendientes_idx
    ON errores (ocurrido_en DESC)
    WHERE NOT resuelto;

CREATE INDEX errores_fecha_idx ON errores (ocurrido_en DESC);

-- +goose Down
DROP TABLE errores;
