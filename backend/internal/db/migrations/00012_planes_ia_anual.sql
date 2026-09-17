-- +goose Up

-- Dos cosas nuevas en los planes:
--
--   1. Si incluyen el asistente con IA. Cada mensaje del asistente le cuesta
--      plata al dueño del servidor, así que es algo que se vende, no algo que
--      se regala a todos.
--   2. Un precio anual, además del mensual. NULL = el plan no se vende por año.

ALTER TABLE planes
    ADD COLUMN incluye_ia   BOOLEAN       NOT NULL DEFAULT false,
    ADD COLUMN precio_anual NUMERIC(14,2),
    ADD CONSTRAINT planes_precio_anual_no_negativo
        CHECK (precio_anual IS NULL OR precio_anual >= 0);

-- Cómo paga CADA cliente su plan. Es del cliente y no del plan: el mismo plan
-- lo puede pagar uno por mes y otro por año.
--
-- La regla "anual solo si el plan tiene precio anual" cruza dos tablas y no
-- cabe en un CHECK: vive en Go (admin.AsignarPlan), en la misma consulta que
-- hace el cambio.
ALTER TABLE usuarios
    ADD COLUMN ciclo_pago TEXT NOT NULL DEFAULT 'mensual',
    ADD CONSTRAINT usuarios_ciclo_valido CHECK (ciclo_pago IN ('mensual', 'anual'));

-- Un pago ahora cubre un RANGO de meses: uno si es mensual, doce si es anual.
--
--   periodo      primer mes cubierto (dia 1)
--   cubre_hasta  primer mes que YA NO cubre (dia 1). Excluyente, como un
--                rango de Postgres: [periodo, cubre_hasta).
ALTER TABLE pagos
    ADD COLUMN ciclo       TEXT NOT NULL DEFAULT 'mensual',
    ADD COLUMN cubre_hasta DATE;

UPDATE pagos SET cubre_hasta = (periodo + interval '1 month')::date;

ALTER TABLE pagos
    ALTER COLUMN cubre_hasta SET NOT NULL,
    ADD CONSTRAINT pagos_ciclo_valido CHECK (ciclo IN ('mensual', 'anual')),
    -- cubre_hasta no se escribe a mano: sale del ciclo. Así un pago anual no
    -- puede quedar cubriendo 11 o 13 meses por un error de cuentas.
    ADD CONSTRAINT pagos_cobertura_coherente CHECK (
        cubre_hasta = (periodo + CASE WHEN ciclo = 'anual'
                                      THEN interval '12 months'
                                      ELSE interval '1 month' END)::date
    );

-- Antes la regla era "un pago por cliente y mes", con un índice único. Con
-- pagos anuales eso ya no alcanza: un pago de enero cubre hasta diciembre, y
-- el índice dejaría registrar otro en marzo. La regla correcta es "dos pagos
-- del mismo cliente no pueden cubrir el mismo mes", y eso es una restricción
-- de exclusión sobre rangos. La base lo impide, igual que antes: un doble clic
-- no puede inflar los ingresos.
--
-- btree_gist hace falta para mezclar "=" (usuario_id) con "&&" (rangos) en el
-- mismo índice. Los pagos huérfanos (usuario_id NULL) no chocan entre sí,
-- porque NULL = NULL no es verdadero: es justo lo que queremos.
CREATE EXTENSION IF NOT EXISTS btree_gist;

DROP INDEX pagos_usuario_periodo_key;

ALTER TABLE pagos
    ADD CONSTRAINT pagos_sin_solapar
    EXCLUDE USING gist (usuario_id WITH =, daterange(periodo, cubre_hasta) WITH &&);

-- +goose Down
ALTER TABLE pagos DROP CONSTRAINT pagos_sin_solapar;
-- Los anuales se pierden al bajar: con un solo mes por fila no hay forma de
-- representarlos sin inventar pagos que no existieron.
DELETE FROM pagos WHERE ciclo = 'anual';
CREATE UNIQUE INDEX pagos_usuario_periodo_key ON pagos (usuario_id, periodo);
ALTER TABLE pagos
    DROP CONSTRAINT pagos_cobertura_coherente,
    DROP CONSTRAINT pagos_ciclo_valido,
    DROP COLUMN cubre_hasta,
    DROP COLUMN ciclo;

ALTER TABLE usuarios
    DROP CONSTRAINT usuarios_ciclo_valido,
    DROP COLUMN ciclo_pago;

ALTER TABLE planes
    DROP CONSTRAINT planes_precio_anual_no_negativo,
    DROP COLUMN precio_anual,
    DROP COLUMN incluye_ia;
