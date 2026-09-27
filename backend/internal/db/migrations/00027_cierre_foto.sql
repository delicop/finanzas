-- +goose Up

-- La foto de la hoja firmada.
--
-- Las mismas tres columnas que lleva una factura en movimientos, y por lo
-- mismo: el cierre que vale es el papel que firmo el responsable, y tenerlo
-- adjunto es lo que permite revisar una cifra rara meses despues sin ir a
-- buscar la carpeta. Los archivos viven en el mismo almacen que las facturas;
-- aqui solo se guarda por donde encontrarlos.
--
-- Vacio = sin foto. TEXT y no NULL para no tener que preguntar dos cosas
-- ("¿existe? ¿y no esta vacio?") cada vez que se lee.
ALTER TABLE cierres
    ADD COLUMN foto_ruta   TEXT NOT NULL DEFAULT '',
    ADD COLUMN foto_nombre TEXT NOT NULL DEFAULT '',
    ADD COLUMN foto_tipo   TEXT NOT NULL DEFAULT '';

-- +goose Down

-- Al soltar las columnas se pierden las rutas, y los archivos se quedan en el
-- almacen sin nada que diga de que cierre eran. No los borra a proposito: un
-- Down no deberia tocar el disco, porque entonces no habria vuelta atras.
-- Quien deshaga esto tiene que limpiar la carpeta a mano.
ALTER TABLE cierres
    DROP COLUMN foto_ruta,
    DROP COLUMN foto_nombre,
    DROP COLUMN foto_tipo;
