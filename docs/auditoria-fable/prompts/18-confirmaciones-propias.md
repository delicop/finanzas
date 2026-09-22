# 18 · Reemplazar confirm() y alert() nativos

Contexto: hay 16 usos de `confirm()`/`alert()` en `frontend/src/` (más
`AvisosCelular.jsx` ~23). En iOS instalada como PWA el diálogo nativo sale
sin título y no sigue el tema de la app. Ya existe un componente excelente
para el caso difícil, `ConfirmarBorrado` en `Clientes.jsx` ~533, y el
`Modal` genérico.

Qué hacer:

1. Un hook `useConfirmar()` (en `componentes/Confirmar.jsx` con su
   provider) que devuelva una función `confirmar({ titulo, mensaje,
   textoBoton, peligroso })` que resuelve `true/false`. Renderiza con
   `Modal`.
2. Un `useAviso()` equivalente para los `alert()`, o mejor: un toast
   discreto abajo, si el diseño de `docs/diseno.md` lo permite.
3. Reemplaza los 16 usos. Los de borrar deben usar `peligroso: true`
   (botón rojo).
4. `ConfirmarBorrado` se queda como está: pedir el correo es a propósito.

Commit: "Las confirmaciones se ven como el resto de la app, también en el teléfono".
