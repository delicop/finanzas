# 34 · Los filtros Desde/Hasta no se pueden escribir con el teclado

Contexto: en `Movimientos.jsx` los `<input type="date">` de Desde y Hasta
llaman a `cambiarFiltro` en cada `onChange`. Al escribir a mano, el
navegador emite valores intermedios (año "0002"), el filtro se aplica con
esa fecha imposible, la lista se recarga y el campo se vacía. Solo funciona
el calendario.

Qué hacer:

1. Estado local para las dos fechas; aplicar al filtro solo en `onBlur`, al
   presionar Enter, o cuando el valor tenga un año de cuatro cifras
   razonable (`>= 2000`). El calendario también dispara `onChange` con un
   valor completo, así que se puede aplicar de inmediato si `value` cumple
   `/^\d{4}-\d{2}-\d{2}$/` y el año es ≥ 2000.
2. Si `desde > hasta`, no aplicar y mostrar "Desde no puede ser después de
   Hasta" bajo el campo.
3. Reutiliza esto en `ModalExportar.jsx` y en el filtro de fechas de
   `Recurrentes.jsx` si lo tienen: un componente `RangoFechas` con esa
   lógica dentro.
4. Si ya se hizo el prompt 16 (debounce), que la fecha use el mismo
   mecanismo de "aplicar con replace en la URL".

Commit: "Las fechas de los filtros se pueden escribir a mano".
