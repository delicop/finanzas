-- +goose Up

-- El cierre y su tienda tienen que ser de la MISMA cuenta. Hoy lo garantiza
-- la CTE que inserta, pero un UPDATE a mano o un bug futuro podria cruzarlos,
-- y el listado de tiendas sumaria las cifras de un cierre ajeno. Con la llave
-- compuesta, Postgres lo rechaza siempre.
ALTER TABLE tiendas ADD CONSTRAINT tiendas_id_usuario_key UNIQUE (id, usuario_id);

ALTER TABLE cierres
    DROP CONSTRAINT cierres_tienda_id_fkey,
    ADD CONSTRAINT cierres_tienda_usuario_fkey
        FOREIGN KEY (tienda_id, usuario_id) REFERENCES tiendas (id, usuario_id);

-- +goose Down
ALTER TABLE cierres
    DROP CONSTRAINT cierres_tienda_usuario_fkey,
    ADD CONSTRAINT cierres_tienda_id_fkey
        FOREIGN KEY (tienda_id) REFERENCES tiendas (id);

ALTER TABLE tiendas DROP CONSTRAINT tiendas_id_usuario_key;
