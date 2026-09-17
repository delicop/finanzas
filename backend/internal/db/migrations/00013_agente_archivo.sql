-- +goose Up

-- Terminar una conversacion sin borrarla.
--
-- Hasta aqui la unica forma de "limpiar" el chat era borrarlo entero. Ahora al
-- terminar, la conversacion se archiva: deja de ser la que abre la app (y la
-- que se le manda al modelo, que asi arranca de cero y cuesta menos), pero se
-- puede volver a leer o descargar.
--
--   archivada_en NULL  -> es la conversacion abierta
--   archivada_en fecha -> esta guardada
ALTER TABLE agente_conversaciones
    ADD COLUMN archivada_en TIMESTAMPTZ,
    -- Para reconocerla en la lista: la primera pregunta, recortada. Se llena
    -- al archivar, cuando ya se sabe de que trato.
    ADD COLUMN titulo TEXT NOT NULL DEFAULT '';

-- Solo una conversacion abierta por usuario. Lo impide la base: dos pestañas
-- que escriben a la vez no pueden abrir dos hilos distintos.
--
-- Antes de crear el indice se archivan las que sobren: hasta ahora cada
-- usuario podia tener varias y la app solo miraba la mas reciente.
UPDATE agente_conversaciones c
SET archivada_en = c.actualizada_en
WHERE c.id <> (SELECT c2.id FROM agente_conversaciones c2
               WHERE c2.usuario_id = c.usuario_id
               ORDER BY c2.actualizada_en DESC, c2.id DESC
               LIMIT 1);

CREATE UNIQUE INDEX agente_conversaciones_una_abierta
    ON agente_conversaciones (usuario_id)
    WHERE archivada_en IS NULL;

-- La lista de guardadas, de la mas reciente a la mas vieja.
CREATE INDEX agente_conversaciones_archivadas_idx
    ON agente_conversaciones (usuario_id, archivada_en DESC)
    WHERE archivada_en IS NOT NULL;

-- +goose Down
DROP INDEX agente_conversaciones_archivadas_idx;
DROP INDEX agente_conversaciones_una_abierta;
ALTER TABLE agente_conversaciones
    DROP COLUMN titulo,
    DROP COLUMN archivada_en;
