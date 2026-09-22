# 20 · Partir estilos.css y limpiar lo muerto

Contexto: `frontend/src/estilos.css` tiene 3543 líneas. Hay sistema de
tokens (`:root` ~49-134, modo oscuro ~137-195) pero conviven dos
vocabularios: los tokens nuevos `--color-*`, `--space-*` y los alias
españoles viejos (`--fondo`, `--superficie`, `--borde`, `--texto`,
`--tenue`, `--acento`, `--ambar`, ~101-131) con uso desigual
(`--superficie`/`--fondo` no se usan; `--ambar` es índigo). El archivo creció
por "tandas" (~2958, ~3260) que redefinen selectores anteriores:
`.modal-fondo` (1071 y 1099), `.barra` (346 y 2742), `.wa-panel` (2119 y
2382), `.campana-punto`, `.ayuda-campo`, `.acciones-encabezado`,
`.atajos-cobro`, `.uso-movil`. Clases sin uso en JSX: `.chat-guardado`,
`.detalle-neto`, `.lista-deudores`, `.monto-contraparte`, `.wa-cajon`,
`.wa-escribiendo`, `.wa-sugerencias`, `.exito`, `.md` (~150 líneas). 132
`font-size` en px sin escala. `#fff` fijo en `.visor-pdf` (~1142) e
`.interruptor` (~2680), que en oscuro quedan blancos. El breakpoint 720
está repetido en cinco `@media` (coincide con `useEsMovil.js`).

Qué hacer, en este orden y en commits separados:

1. **Borrar las clases muertas.** Verifica cada una con grep en `src/`.
2. **Consolidar cada selector redefinido en un solo bloque**, conservando
   la regla que gana hoy (la última). Comprueba visualmente las pantallas
   afectadas en claro y oscuro, escritorio y teléfono.
3. **Un solo vocabulario de tokens.** Deja `--color-*` etc. y reemplaza los
   alias viejos; si `--tenue` y `--borde` se usan mucho, renómbralos a
   `--color-texto-tenue`/`--color-borde` con buscar y reemplazar.
4. **Escala tipográfica**: `--texto-xs/sm/base/lg/xl` y reemplaza los px.
5. Corrige los dos `#fff` con el token de superficie.
6. **Partir el archivo**: `estilos/tokens.css`, `base.css`, `layout.css`,
   `componentes/*.css` por componente, `paginas/*.css`. Importa todo desde
   `main.jsx`; Vite lo concatena igual.
7. Agrega `stylelint` a CI (prompt 07) con la regla de no-duplicate-selectors.

Sigue `docs/diseno.md` al pie de la letra y actualízalo con la nueva
organización de archivos.

Commits: uno por paso, por ejemplo "Estilos: fuera 150 líneas que nada usaba".
