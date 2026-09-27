-- +goose Up

-- Fuera la casilla de "saldo en Nequi".
--
-- En la hoja de papel ese numero va escrito a mano en NOVEDADES: no es una
-- casilla del formato, es una nota del dia. Tenerlo como campo obligaba a
-- llenarlo siempre y lo separaba de lo que de verdad es.
--
-- Al soltar la columna, Postgres se lleva con ella el CHECK que la nombraba
-- — y ese CHECK cuidaba TODAS las casillas, no solo esta. Por eso se vuelve
-- a crear enseguida, sin ella: si no, las demas quedarian sin su regla y
-- nadie se enteraria hasta ver una hoja con numeros negativos.
ALTER TABLE cierres DROP COLUMN saldo_nequi;

ALTER TABLE cierres
    ADD CONSTRAINT cierres_montos_no_negativos CHECK (
        qr_banco >= 0 AND qr_tienda >= 0
        AND datafono_reporte >= 0 AND datafono_tienda >= 0
        AND efectivo_billete >= 0 AND efectivo_moneda >= 0 AND efectivo_tienda >= 0
        AND venta_tienda >= 0
    );

-- +goose Down
ALTER TABLE cierres DROP CONSTRAINT cierres_montos_no_negativos;

ALTER TABLE cierres
    ADD COLUMN saldo_nequi NUMERIC(14,2) NOT NULL DEFAULT 0;

ALTER TABLE cierres
    ADD CONSTRAINT cierres_montos_no_negativos CHECK (
        qr_banco >= 0 AND qr_tienda >= 0
        AND datafono_reporte >= 0 AND datafono_tienda >= 0
        AND efectivo_billete >= 0 AND efectivo_moneda >= 0 AND efectivo_tienda >= 0
        AND venta_tienda >= 0 AND saldo_nequi >= 0
    );
