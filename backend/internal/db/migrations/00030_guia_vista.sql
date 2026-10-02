-- +goose Up

-- "Ya vi la guía de bienvenida".
--
-- La guía se abre sola la primera vez que alguien entra. Esto estaba guardado
-- solo en el navegador (localStorage), y por eso volvía a salir una y otra
-- vez: el mismo usuario en el celular y en el computador son dos navegadores
-- distintos, y la app abierta por la IP de la red local no comparte nada con
-- la abierta por el dominio. Es un dato de la PERSONA, no del aparato, así que
-- vive aquí.
--
-- Guarda la fecha y no un booleano: así se puede responder después "¿cuándo
-- entró por primera vez?" sin una columna más.
ALTER TABLE usuarios ADD COLUMN guia_vista_en TIMESTAMPTZ;

-- Las cuentas que ya existen no necesitan la guía: llevan meses usando la app
-- y recibirla ahora sería ruido.
UPDATE usuarios SET guia_vista_en = now();

-- +goose Down
ALTER TABLE usuarios DROP COLUMN guia_vista_en;
