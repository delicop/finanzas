-- +goose Up
CREATE TABLE usuarios (
    id            BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         TEXT        NOT NULL,
    nombre        TEXT        NOT NULL DEFAULT '',
    password_hash TEXT        NOT NULL,
    creado_en     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- El email se guarda siempre en minusculas (lo normaliza la capa Go),
-- y el indice unico garantiza que no existan dos cuentas con el mismo correo.
CREATE UNIQUE INDEX usuarios_email_key ON usuarios (email);

-- +goose Down
DROP TABLE usuarios;
