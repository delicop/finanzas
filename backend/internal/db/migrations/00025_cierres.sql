-- +goose Up

-- El cierre de caja de una tienda: la hoja que se llena al final del dia.
--
-- Es la hoja de papel, tal cual: lo que dice el banco contra lo que conto la
-- tienda, en cada forma de cobro, y las listas de lo que salio (compras,
-- gastos, descuentos, vales) y de los pagos por Nequi.
--
-- Las DIFERENCIAS y los TOTALES no se guardan: se calculan al leer. Guardar
-- una resta es guardar dos veces el mismo dato, y el dia que una de las dos
-- cifras se corrija, la resta vieja quedaria mintiendo.
--
-- No tiene nada que ver con la tabla movimientos: un cierre es el arqueo de un
-- local, no un movimiento de las finanzas personales del cliente.
CREATE TABLE cierres (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- usuario_id ademas de tienda_id: todas las consultas filtran por el
    -- usuario del token, igual que en el resto de la app. Es lo que impide
    -- que con un id ajeno se lea el cierre de otro.
    usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,

    -- Sin CASCADE ni RESTRICT, a proposito: con la regla por defecto (NO
    -- ACTION) borrar una tienda que tiene cierres FALLA, que es lo que
    -- queremos — esas hojas son registros de plata y no pueden irse en
    -- silencio. Y como NO ACTION se revisa al final de la sentencia, borrar
    -- al USUARIO sigue funcionando: sus cierres se van por usuario_id en la
    -- misma sentencia que borra sus tiendas. Con RESTRICT eso ultimo
    -- reventaria, porque se revisa de inmediato.
    tienda_id BIGINT NOT NULL REFERENCES tiendas(id),

    fecha       DATE NOT NULL,
    responsable TEXT NOT NULL DEFAULT '',

    -- QR / Nequi: lo que reporta el banco contra lo que conto la tienda.
    qr_banco  NUMERIC(14,2) NOT NULL DEFAULT 0,
    qr_tienda NUMERIC(14,2) NOT NULL DEFAULT 0,

    -- Datafono: lo que reporta el datafono contra lo que conto la tienda.
    datafono_reporte NUMERIC(14,2) NOT NULL DEFAULT 0,
    datafono_tienda  NUMERIC(14,2) NOT NULL DEFAULT 0,

    -- Efectivo: los billetes y las monedas se cuentan aparte porque asi se
    -- cuenta la caja. Su suma es el total contado, y va contra lo que dice la
    -- tienda que deberia haber.
    efectivo_billete NUMERIC(14,2) NOT NULL DEFAULT 0,
    efectivo_moneda  NUMERIC(14,2) NOT NULL DEFAULT 0,
    efectivo_tienda  NUMERIC(14,2) NOT NULL DEFAULT 0,

    -- La venta total segun la tienda, para contrastarla con la suma de las
    -- tres formas de cobro.
    venta_tienda NUMERIC(14,2) NOT NULL DEFAULT 0,

    saldo_nequi NUMERIC(14,2) NOT NULL DEFAULT 0,
    novedades   TEXT NOT NULL DEFAULT '',

    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Ninguna casilla de la hoja puede ser negativa: lo que salio va en las
    -- listas de abajo, no como un numero en rojo aqui arriba.
    CONSTRAINT cierres_montos_no_negativos CHECK (
        qr_banco >= 0 AND qr_tienda >= 0
        AND datafono_reporte >= 0 AND datafono_tienda >= 0
        AND efectivo_billete >= 0 AND efectivo_moneda >= 0 AND efectivo_tienda >= 0
        AND venta_tienda >= 0 AND saldo_nequi >= 0
    )
);

-- Un dia, una hoja. Lo impide la base y no la capa Go: dos cierres del mismo
-- dia en la misma tienda serian dos verdades distintas sobre la misma caja.
CREATE UNIQUE INDEX cierres_tienda_fecha_key ON cierres (tienda_id, fecha);

-- La pantalla siempre pregunta "los cierres de ESTA tienda, del mas nuevo al
-- mas viejo".
CREATE INDEX cierres_tienda_fecha_idx ON cierres (tienda_id, fecha DESC);

-- Las listas de la hoja: pagos por Nequi, compras, gastos, descuentos y vales.
--
-- Una sola tabla con una columna `grupo` y no cinco tablas iguales: son la
-- misma cosa (una descripcion y un valor) y cambiarlas en cinco sitios es
-- cambiarlas mal en alguno. El total de cada lista lo suma Postgres al leer.
CREATE TABLE cierre_lineas (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- CASCADE: una linea sin su cierre no significa nada.
    cierre_id BIGINT NOT NULL REFERENCES cierres(id) ON DELETE CASCADE,

    grupo       TEXT          NOT NULL,
    descripcion TEXT          NOT NULL,
    monto       NUMERIC(14,2) NOT NULL,

    -- El orden en que se escribieron: la hoja se lee como se llena.
    orden INT NOT NULL DEFAULT 0,

    CONSTRAINT cierre_lineas_grupo_valido CHECK (
        grupo IN ('pago_nequi', 'compra', 'gasto', 'descuento', 'vale')
    ),
    CONSTRAINT cierre_lineas_descripcion_no_vacia CHECK (btrim(descripcion) <> ''),
    CONSTRAINT cierre_lineas_monto_no_negativo CHECK (monto >= 0)
);

CREATE INDEX cierre_lineas_cierre_idx ON cierre_lineas (cierre_id, grupo, orden);

-- +goose Down
DROP TABLE cierre_lineas;
DROP TABLE cierres;
