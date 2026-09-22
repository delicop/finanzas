# 19 · Modal con dialog nativo y formularios accesibles

Contexto: `frontend/src/componentes/Modal.jsx` (~15-26) pone `role="dialog"`
y `aria-modal` pero no atrapa el foco, no pone foco inicial, no lo devuelve
al cerrar, no enlaza `aria-labelledby` con el `<h2>` y no bloquea el scroll
del fondo. Filas clicables sin teclado: `Clientes.jsx` ~200-207 y ~139-142
(`<tr onClick>` sin `tabIndex`/`role`/`onKeyDown`). En
`MovimientoForm.jsx` ~179-180 un `<label htmlFor="tipo">` apunta a un
`<div>`, y los chips de tipo no tienen `aria-pressed` ni `role="radiogroup"`.
Los errores de campo (`.error-campo`) no usan `aria-describedby` ni
`aria-invalid`.

Qué hacer:

1. `Modal` sobre `<dialog>` con `showModal()`/`close()`: focus trap, Escape
   e `inert` del fondo vienen gratis. Foco inicial en el primer campo o en el
   título; al cerrar, devolver el foco al elemento que abrió. `overflow:
   hidden` en `body` mientras esté abierto. `aria-labelledby` al título.
   Ajusta el CSS del `::backdrop` para que se vea igual que hoy.
2. Los chips de tipo: `<fieldset><legend>Tipo</legend>` y cada chip con
   `aria-pressed`. Quitar el `htmlFor` roto.
3. Errores de campo: `aria-invalid` en el input y `aria-describedby` al
   mensaje.
4. Filas clicables: un `<button>` dentro de la celda del nombre en vez de
   `onClick` en la fila, o `tabIndex=0` + `onKeyDown` Enter/Espacio si el
   diseño exige la fila entera.
5. Pasa `eslint-plugin-jsx-a11y` (prompt 07) y deja en cero lo que marque.

Commit: "Los modales y formularios se pueden usar con teclado y lector de pantalla".
