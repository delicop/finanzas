-- +goose Up

-- El chat con el asistente. Una fila por conversacion; la ultima de cada
-- usuario es la que abre la app.
CREATE TABLE agente_conversaciones (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- CASCADE, no RESTRICT: una conversacion no es un registro de dinero.
    -- Si la cuenta se va, su chat se va con ella y no queda huerfano.
    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    creada_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizada_en TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "la ultima conversacion de este usuario" es la consulta de cada carga.
CREATE INDEX agente_conversaciones_usuario_idx
    ON agente_conversaciones (usuario_id, actualizada_en DESC);

CREATE TABLE agente_mensajes (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversacion_id BIGINT NOT NULL REFERENCES agente_conversaciones(id) ON DELETE CASCADE,

    -- Desnormalizado a proposito. Se podria sacar con un JOIN a la
    -- conversacion, pero tenerlo aqui permite que TODA consulta de mensajes
    -- filtre por usuario_id igual que el resto de la app, incluido el conteo
    -- del limite diario, que cruza todas las conversaciones de una persona.
    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    rol       TEXT NOT NULL,
    contenido TEXT NOT NULL,

    -- Lo que reporto el proveedor. Sirve para estimar el costo por usuario
    -- sin tener que entrar al panel de quien vende el modelo.
    tokens_entrada INT NOT NULL DEFAULT 0,
    tokens_salida  INT NOT NULL DEFAULT 0,

    creado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Los roles validos los fija la base, no solo Go: un rol inventado
    -- rompería el historial que se le manda al modelo.
    CONSTRAINT agente_mensajes_rol_valido CHECK (rol IN ('usuario', 'agente'))
);

-- Leer el hilo en orden. Por id y no por creado_en: dos mensajes del mismo
-- segundo tienen que salir siempre en el mismo orden.
CREATE INDEX agente_mensajes_conversacion_idx
    ON agente_mensajes (conversacion_id, id);

-- Una fila por mensaje que el usuario gasta. Es el contador del limite, y es
-- una tabla APARTE del historial a proposito: "empezar de cero" borra la
-- conversacion, y si el contador viviera en agente_mensajes se reiniciaria con
-- ella. El techo de costo se saltaria con un clic en un boton de la app.
--
-- Guarda solo el cuando, nunca el que: para contar no hace falta el texto, y
-- asi borrar el hilo si borra de verdad todo lo que se escribio.
CREATE TABLE agente_consumo (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    creado_en  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- El conteo del limite: cuantos gasto este usuario en las ultimas 24 horas.
CREATE INDEX agente_consumo_usuario_fecha_idx
    ON agente_consumo (usuario_id, creado_en DESC);

-- +goose Down
DROP TABLE agente_consumo;
DROP TABLE agente_mensajes;
DROP TABLE agente_conversaciones;
