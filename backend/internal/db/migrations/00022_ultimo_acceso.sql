-- +goose Up

-- Cuándo fue la última vez que esta cuenta abrió la app.
--
-- El panel ya decía cuántos movimientos tiene cada cliente, pero no si
-- todavía lo usa: mil movimientos de hace ocho meses y mil de esta semana se
-- veían exactamente igual. Para cobrar (o para llamar a alguien antes de que
-- se vaya en silencio) lo que importa es la segunda cifra.
--
-- Va en usuarios y no en una tabla de eventos porque la pregunta es "¿cuándo
-- fue la última vez?", no "¿qué hizo cada día?". Una fila por cuenta, que se
-- pisa, no crece nunca y no hay que limpiar. El día que haga falta el detalle
-- (a qué hora entra, desde dónde) eso es otra tabla y otra decisión.
--
-- Arranca en NULL: de las visitas de antes de hoy no hay registro, y fingir
-- una fecha sería peor que no tenerla. El panel lo muestra como "sin registro"
-- y se va llenando solo.
ALTER TABLE usuarios ADD COLUMN ultimo_acceso TIMESTAMPTZ;

-- Para ordenar el panel por "quién lleva más tiempo sin aparecer", que es como
-- se mira esta columna. NULLS LAST en el índice para que esa consulta no tenga
-- que ordenar en memoria.
CREATE INDEX usuarios_ultimo_acceso_idx ON usuarios (ultimo_acceso DESC NULLS LAST);

-- +goose Down
DROP INDEX usuarios_ultimo_acceso_idx;
ALTER TABLE usuarios DROP COLUMN ultimo_acceso;
