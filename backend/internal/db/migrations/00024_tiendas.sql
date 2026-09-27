-- +goose Up

-- Las tiendas del cliente: los locales desde donde mueve su plata.
--
-- Igual que el asistente con IA, es algo que se VENDE y no algo que traiga
-- toda cuenta: el plan dice si la seccion existe para ese cliente. Quien no la
-- tiene ni siquiera ve el menu, y el backend le responde 403 si escribe la URL
-- a mano. La casilla vive en el plan y no en el usuario por lo mismo que
-- incluye_ia: se le cambia a un plan y aplica de una a todos sus clientes.
ALTER TABLE planes
    ADD COLUMN incluye_tiendas BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE tiendas (
    id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- CASCADE igual que en medios_pago y categorias: las tiendas no son
    -- registros de dinero, son una lista del cliente. Si la cuenta se va, se
    -- van con ella.
    usuario_id     BIGINT      NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    nombre         TEXT        NOT NULL,
    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT tiendas_nombre_no_vacio CHECK (btrim(nombre) <> '')
);

-- El nombre es unico DENTRO de cada cuenta, no en toda la base: dos clientes
-- distintos pueden llamarle "Principal" a la suya. lower() para que "Centro" y
-- "centro" no convivan en la misma lista.
CREATE UNIQUE INDEX tiendas_usuario_nombre_key ON tiendas (usuario_id, lower(nombre));

-- +goose Down
DROP TABLE tiendas;
ALTER TABLE planes DROP COLUMN incluye_tiendas;
