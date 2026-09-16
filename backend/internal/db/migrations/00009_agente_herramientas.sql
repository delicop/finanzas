-- +goose Up

-- Que herramientas consulto el agente para armar esta respuesta.
--
-- Se guarda para poder decirlo en la app ("consulto tu resumen"): quien lee
-- una cifra tiene derecho a saber de donde salio. Ademas deja auditable, mes
-- despues, que fue lo que el agente miro.
--
-- JSONB con un arreglo de nombres. Podria ser TEXT[], pero jsonb se lee y se
-- escribe desde Go con el mismo encoding/json que todo lo demas.
ALTER TABLE agente_mensajes
    ADD COLUMN herramientas JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE agente_mensajes DROP COLUMN herramientas;
