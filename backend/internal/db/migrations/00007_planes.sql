-- +goose Up

-- El lado NEGOCIO de la app: a quien le cobras, cuanto, y quien ya pago.
--
-- Ojo con la confusion facil: estas tablas NO son las finanzas de los clientes
-- (esas son movimientos/categorias/medios_pago, y cada cliente ve las suyas).
-- Estas son las finanzas del dueno del servidor, y solo las ve un admin.

CREATE TABLE planes (
    id             BIGINT        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nombre         TEXT          NOT NULL,
    -- NUMERIC igual que en movimientos: el precio es plata, no un float.
    precio_mensual NUMERIC(14,2) NOT NULL,
    activo         BOOLEAN       NOT NULL DEFAULT true,
    creado_en      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ   NOT NULL DEFAULT now(),

    CONSTRAINT planes_nombre_no_vacio    CHECK (btrim(nombre) <> ''),
    CONSTRAINT planes_precio_no_negativo CHECK (precio_mensual >= 0)
);

-- Sobre lower(nombre): impide "Pro" y "pro" a la vez. No lleva usuario_id
-- porque los planes son del servidor, no de nadie en particular.
CREATE UNIQUE INDEX planes_nombre_key ON planes (lower(nombre));

-- El plan del cliente. NULL a proposito: los usuarios que ya existen no tienen
-- ninguno, y un admin tampoco necesita uno (no se cobra a si mismo).
--
-- RESTRICT y no CASCADE: borrar un plan que alguien esta usando debe fallar.
-- Si se borrara en cascada, los clientes quedarian sin plan en silencio y
-- dejarias de cobrarles sin enterarte.
ALTER TABLE usuarios
    ADD COLUMN plan_id BIGINT REFERENCES planes(id) ON DELETE RESTRICT;

CREATE INDEX usuarios_plan_idx ON usuarios (plan_id);

-- Los cobros, un renglon por cliente y mes.
CREATE TABLE pagos (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- SET NULL y no CASCADE: si borras a un cliente, la plata que YA te pago
    -- no desaparece de tus cuentas. Un cliente se va, pero los ingresos de
    -- marzo siguen siendo los ingresos de marzo.
    usuario_id BIGINT REFERENCES usuarios(id) ON DELETE SET NULL,

    -- Copias historicas, no referencias. Si manana le subes el precio al plan
    -- Pro, los pagos viejos tienen que seguir diciendo lo que de verdad se
    -- cobro ese mes; con un JOIN al plan actual, el historico se reescribiria
    -- solo cada vez que cambias una tarifa. Ademas es lo que deja el registro
    -- legible cuando el cliente ya no existe.
    cliente_email TEXT          NOT NULL,
    plan_nombre   TEXT          NOT NULL,
    monto         NUMERIC(14,2) NOT NULL,

    -- Mes cobrado, siempre normalizado al dia 1. Guardarlo como DATE y no como
    -- texto "2026-03" permite ordenar y filtrar por rangos con los operadores
    -- normales de fecha.
    periodo   DATE NOT NULL,
    pagado_en DATE NOT NULL,
    nota      TEXT NOT NULL DEFAULT '',

    creado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT pagos_monto_positivo CHECK (monto > 0),
    CONSTRAINT pagos_periodo_dia_uno CHECK (extract(day from periodo) = 1)
);

-- Un cliente no puede pagar dos veces el mismo mes: la base lo impide, no la
-- capa Go. Un doble clic en "registrar pago" no puede inflar los ingresos.
-- Solo aplica a los pagos con dueno; los huerfanos (cliente borrado) quedan
-- fuera del indice porque usuario_id es NULL, que es justo lo que queremos.
CREATE UNIQUE INDEX pagos_usuario_periodo_key ON pagos (usuario_id, periodo);

-- El panel siempre pregunta "¿quien pago ESTE mes?".
CREATE INDEX pagos_periodo_idx ON pagos (periodo DESC);

-- +goose Down
DROP TABLE pagos;
DROP INDEX usuarios_plan_idx;
ALTER TABLE usuarios DROP COLUMN plan_id;
DROP TABLE planes;
