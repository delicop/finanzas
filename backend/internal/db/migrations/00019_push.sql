-- +goose Up

-- Las notificaciones que llegan al celular con la app CERRADA.
--
-- Hasta aquí los avisos vivían solo en la campana: si el usuario no abría la
-- app, no se enteraba de que hoy le pagaban. Web Push cambia eso: el navegador
-- mantiene un canal con su propio servicio (Google, Apple, Mozilla) y nosotros
-- le mandamos el mensaje ahí.
--
-- Qué es cada cosa que se guarda aquí, porque los nombres no ayudan:
--
--   endpoint -> la URL del servicio de push a la que se le manda el mensaje.
--               Es única por navegador y por dispositivo: el mismo usuario en
--               el celular y en el computador son dos filas.
--   p256dh   -> la llave pública del navegador. Con ella se CIFRA el mensaje,
--               para que el servicio de push (que hace de cartero) no pueda
--               leer de qué se trata.
--   auth     -> un secreto compartido que entra en ese mismo cifrado.
--
-- Nada de esto identifica al usuario ante nadie más: son credenciales que el
-- propio navegador generó para este sitio.
CREATE TABLE push_suscripciones (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    endpoint TEXT NOT NULL,
    p256dh   TEXT NOT NULL,
    auth     TEXT NOT NULL,

    creada_en    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ultimo_envio TIMESTAMPTZ,

    -- Cuántas veces seguidas falló. Un endpoint muerto (el usuario desinstaló
    -- la app, limpió el navegador) responde 404 o 410 y se borra de una; esto
    -- es para los fallos raros, que no deben dejar la fila reintentando para
    -- siempre.
    fallos INT NOT NULL DEFAULT 0,

    CONSTRAINT push_endpoint_no_vacio CHECK (btrim(endpoint) <> '')
);

-- El endpoint es único en el mundo: si el mismo navegador vuelve a
-- suscribirse, es la MISMA suscripción y se actualiza, no se duplica.
CREATE UNIQUE INDEX push_suscripciones_endpoint_key ON push_suscripciones (endpoint);

-- La consulta del envío: "los dispositivos de este usuario".
CREATE INDEX push_suscripciones_usuario_idx ON push_suscripciones (usuario_id);

-- +goose Down
DROP TABLE push_suscripciones;
