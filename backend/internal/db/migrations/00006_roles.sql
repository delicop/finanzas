-- +goose Up

-- Roles y estado de la cuenta.
--
-- Hasta aqui la app era de un solo usuario, pero el esquema ya estaba listo
-- para varios: toda tabla de dominio tiene usuario_id y todas las consultas
-- filtran por el. Lo unico que faltaba era saber QUIEN administra a quien.
--
-- Solo dos roles: 'admin' (el dueno del servidor) y 'usuario'. No hay
-- jerarquias intermedias porque no hay a quien darselas.
ALTER TABLE usuarios
    ADD COLUMN rol    TEXT    NOT NULL DEFAULT 'usuario',
    ADD COLUMN activo BOOLEAN NOT NULL DEFAULT true;

ALTER TABLE usuarios
    ADD CONSTRAINT usuarios_rol_valido CHECK (rol IN ('admin', 'usuario'));

-- El usuario que ya existia es el dueno del servidor: se vuelve admin.
-- min(id) y no "todos": si manana hay varios, solo el primero hereda el rol.
UPDATE usuarios SET rol = 'admin' WHERE id = (SELECT min(id) FROM usuarios);

-- 'activo' en vez de borrar: un usuario borrado se lleva por CASCADE todos
-- sus movimientos, y ademas dejaria huerfanas sus facturas en el disco.
-- Desactivar corta el acceso sin destruir el historial de plata.
--
-- La regla "siempre tiene que quedar al menos un admin activo" NO se puede
-- expresar como CHECK: es una condicion entre filas, no dentro de una. Vive
-- en Go (admin.Store), dentro de la misma transaccion que hace el cambio.

CREATE INDEX usuarios_creado_idx ON usuarios (creado_en DESC);

-- +goose Down
DROP INDEX usuarios_creado_idx;
ALTER TABLE usuarios DROP CONSTRAINT usuarios_rol_valido;
ALTER TABLE usuarios DROP COLUMN activo, DROP COLUMN rol;
