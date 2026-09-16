-- +goose Up

-- Medios de pago / recaudo: efectivo, transferencia, Nequi, etc.
--
-- Es una tabla APARTE de categorias porque responden preguntas distintas:
--   categoria    -> "¿de qué negocio o rubro es esta plata?"
--   medio de pago-> "¿por dónde entró o salió?"
-- Mezclarlas obligaria a crear "Negocio 1 - efectivo", "Negocio 1 - transferencia"
-- y la lista se volveria inmanejable.
CREATE TABLE medios_pago (
    id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id     BIGINT      NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    nombre         TEXT        NOT NULL,
    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT medios_pago_nombre_no_vacio CHECK (btrim(nombre) <> '')
);

CREATE UNIQUE INDEX medios_pago_usuario_nombre_key
    ON medios_pago (usuario_id, lower(nombre));

-- NULL permitido a proposito: los movimientos que ya existen no tienen medio
-- de pago, y no seria correcto inventarles uno. Tampoco queremos obligar a
-- registrarlo siempre: si no se sabe, se deja vacio.
--
-- RESTRICT igual que en categorias: borrar un medio con movimientos falla,
-- para no perder el dato de por donde entro la plata.
ALTER TABLE movimientos
    ADD COLUMN medio_pago_id BIGINT REFERENCES medios_pago(id) ON DELETE RESTRICT;

CREATE INDEX movimientos_medio_pago_idx ON movimientos (medio_pago_id);

-- Arrancamos con los tres medios mas comunes para que la app sea usable
-- desde el primer dia. El usuario puede renombrarlos, borrarlos o agregar
-- los suyos (Nequi, Daviplata, Bancolombia...).
INSERT INTO medios_pago (usuario_id, nombre)
SELECT u.id, m.nombre
FROM usuarios u
CROSS JOIN (VALUES ('Efectivo'), ('Transferencia'), ('Otro')) AS m(nombre);

-- +goose Down
ALTER TABLE movimientos DROP COLUMN medio_pago_id;
DROP TABLE medios_pago;
