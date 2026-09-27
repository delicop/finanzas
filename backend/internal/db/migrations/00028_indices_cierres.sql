-- +goose Up

-- Toda consulta de cierres filtra primero por el usuario del token, y
-- ningun indice empezaba por ahi: la lista global (/api/cierres) recorria
-- la tabla completa, con las filas de todos los clientes del servidor.
-- La fecha va detras porque la lista siempre ordena por ella.
CREATE INDEX cierres_usuario_fecha_idx ON cierres (usuario_id, fecha DESC);

-- Redundante con cierres_tienda_fecha_key, que es UNIQUE sobre las mismas
-- dos columnas: un btree se recorre en los dos sentidos, asi que el unico
-- ya sirve para "del mas nuevo al mas viejo" (ver su comentario en la 25).
-- Este solo costaba escrituras.
DROP INDEX cierres_tienda_fecha_idx;

-- +goose Down
CREATE INDEX cierres_tienda_fecha_idx ON cierres (tienda_id, fecha DESC);
DROP INDEX cierres_usuario_fecha_idx;
