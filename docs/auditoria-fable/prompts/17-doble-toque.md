# 17 · Guardar contra el doble toque en los botones sueltos

Contexto: los `<form>` sí usan `disabled={guardando}`, pero los botones que
no están en un formulario no tienen guardia: `Movimientos.jsx` ~121-129
(eliminar), `Recurrentes.jsx` ~75-93 (alternar activo), `Clientes.jsx`
~49-74 (cambiar estado, asignar plan), `Planes.jsx` ~43-59 y `Negocio.jsx`
~92-101. En el teléfono un doble toque manda dos `DELETE` o dos `PUT`.
`Movimientos.jsx` ~45 ya tiene un patrón bueno (`cambiandoEstado` por id).

Qué hacer:

1. Si ya está TanStack Query (prompt 08): usa `mutation.isPending` y
   deshabilita el botón. Listo.
2. Si no: un hook `useOcupado()` que devuelva `{ ocupado(id), correr(id, fn) }`
   y aplícalo en los cinco sitios.
3. El botón deshabilitado debe verse deshabilitado (ya hay estilo para
   `:disabled` en `estilos.css`; compruébalo).
4. Las pruebas manuales lograron **guardar un pago tres veces** con tres
   clics por código en el modal de abonos, es decir, también dentro de un
   formulario con `disabled={guardando}`: el estado se pone después del
   primer `await`. Pon la guardia **antes** de cualquier `await` (un `ref`
   síncrono `enviando.current`) en `ModalAbonos`, `ModalCobro`,
   `MovimientoForm` y `CubrirFaltante`.
5. Red de seguridad en el backend para los abonos y los pagos del negocio:
   una cabecera opcional `Idempotency-Key` (uuid que genera el frontend al
   abrir el formulario). El store guarda la clave con el id creado en una
   tabla pequeña con expiración de 24 h; si llega la misma clave, responde
   el mismo 201 sin crear nada. Test de integración: tres POST con la misma
   clave crean un solo abono.

Commit: "Tocar dos veces un botón ya no repite la acción".
