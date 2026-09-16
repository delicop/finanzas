-- +goose Up

CREATE TABLE categorias (
    id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id     BIGINT      NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    nombre         TEXT        NOT NULL,
    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT categorias_nombre_no_vacio CHECK (btrim(nombre) <> '')
);

-- Indice unico sobre lower(nombre): impide "Negocio 1" y "negocio 1" a la vez.
-- La restriccion incluye usuario_id para que el dia que haya mas de un usuario
-- cada uno pueda tener sus propios nombres sin chocar.
CREATE UNIQUE INDEX categorias_usuario_nombre_key
    ON categorias (usuario_id, lower(nombre));

CREATE TABLE movimientos (
    id             BIGINT        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id     BIGINT        NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    -- RESTRICT (no CASCADE): borrar una categoria con movimientos falla a
    -- proposito. En una app de plata, borrar registros de dinero "sin querer"
    -- por borrar una categoria seria un desastre. La API traduce ese fallo
    -- a un 409 con un mensaje claro.
    categoria_id   BIGINT        NOT NULL REFERENCES categorias(id) ON DELETE RESTRICT,

    tipo           TEXT          NOT NULL,
    -- NUMERIC = decimal exacto. NUNCA usar float para dinero:
    -- en binario 0.1 no existe exacto y los totales terminan descuadrados.
    monto          NUMERIC(14,2) NOT NULL,
    fecha          DATE          NOT NULL,
    descripcion    TEXT          NOT NULL DEFAULT '',

    -- Solo aplican cuando tipo = 'preste'.
    a_quien        TEXT,
    estado         TEXT,

    -- Factura adjunta (opcional). Guardamos la ruta relativa dentro de /uploads,
    -- el nombre original (para la descarga) y el tipo real detectado.
    factura_ruta   TEXT,
    factura_nombre TEXT,
    factura_tipo   TEXT,

    creado_en      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ   NOT NULL DEFAULT now(),

    CONSTRAINT movimientos_tipo_valido   CHECK (tipo IN ('recibi', 'pague', 'preste')),
    CONSTRAINT movimientos_monto_positivo CHECK (monto > 0),
    CONSTRAINT movimientos_estado_valido  CHECK (estado IS NULL OR estado IN ('pagado', 'pendiente')),

    -- La regla de negocio vive TAMBIEN en la base de datos, no solo en Go:
    -- 'preste' obliga a_quien + estado; los otros dos tipos los prohiben.
    -- Asi ningun bug futuro (ni un UPDATE manual por psql) puede dejar
    -- un prestamo sin dueno o un gasto con estado "pendiente" sin sentido.
    CONSTRAINT movimientos_preste_completo CHECK (
        (tipo = 'preste'  AND a_quien IS NOT NULL AND btrim(a_quien) <> '' AND estado IS NOT NULL)
     OR (tipo <> 'preste' AND a_quien IS NULL AND estado IS NULL)
    )
);

-- El listado siempre ordena por fecha descendente y filtra por usuario.
CREATE INDEX movimientos_usuario_fecha_idx ON movimientos (usuario_id, fecha DESC, id DESC);
CREATE INDEX movimientos_categoria_idx     ON movimientos (categoria_id);

-- Indice PARCIAL: solo indexa las filas que de verdad consultamos seguido
-- (los prestamos pendientes del dashboard). Ocupa una fraccion de un indice normal.
CREATE INDEX movimientos_pendientes_idx
    ON movimientos (usuario_id)
    WHERE tipo = 'preste' AND estado = 'pendiente';

-- +goose Down
DROP TABLE movimientos;
DROP TABLE categorias;
