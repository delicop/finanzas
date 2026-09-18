# El sistema visual

Todo vive en un solo archivo: [`frontend/src/estilos.css`](../frontend/src/estilos.css).
Los tokens están arriba del todo; el resto del archivo solo los lee.

## La idea

**Moderno y frío.** Grises azulados, fichas que se apoyan encima, esquinas
redondas y un índigo como color de marca. Elegante, pero de app — no de
documento.

La organización de las pantallas viene de un rediseño hecho en
[Claude Design](https://claude.ai/design) (el bundle original está en
`Proyecto visual agradable-handoff.zip`). El acomodo se conservó; la piel es
otra.

## Las seis reglas

1. **El fondo NO es blanco; las fichas SÍ.** Ese par de tonos de diferencia
   (`--color-bg` gris azulado contra `--color-surface` blanco) es lo que hace
   que una ficha flote. La sombra solo lo termina — si se quita el contraste de
   fondo y se compensa con más sombra, se ve sucio.

2. **El volumen dice qué se toca.** Los botones principales van **rellenos**;
   los campos se **hunden** un tono respecto de la ficha que los contiene. Un
   contorno vacío se lee como una opción más; un bloque de color se lee como
   "esto es lo que vas a hacer".

3. **Las sombras salen del color del texto**, `rgba(27,27,40,…)`, en dos capas:
   una pegada y una difusa. Una sombra café o neutra sobre un gris azulado se
   ve sucia.

4. **Un solo color de marca.** El índigo (`--color-accent`) es para acciones,
   foco y lo que pide atención. Todo lo demás sale de la rampa neutra. Si se
   colorea de más, el color deja de señalar.

5. **Nada de tonos de tierra.** Todo el sistema es frío. Es la lección que
   costó dos intentos: un fondo café con el verde y el rojo del dinero se
   ensucia, y en modo claro se ve peor.

6. **Todo sale de los tokens.** `var(--color-*)`, `var(--space-*)`,
   `var(--radius-*)`, `var(--shadow-*)`. Nunca un hex ni un px que el token ya
   traiga. En particular, **el texto encima de un relleno de color sale de
   `--sobre-acento`**, no de un blanco fijo: de noche el acento es claro y el
   blanco encima no se lee.

## Los colores del dinero

Dos colores, y ninguno es de semáforo:

| | Light | Dark | Qué es |
|---|---|---|---|
| `.positivo` | `#0b8365` menta | `#4fd1a5` | lo que entra |
| `.negativo` | `#d81f2f` rojo | `#ff4451` | lo que sale |
| `.advertencia` | índigo | índigo | **lo que pide una acción** |
| `.neutro` | gris | gris | un traslado, que no suma ni resta |

Índigo, menta y rojo están **a la misma distancia entre sí en el círculo de
color**, así que ninguno se confunde con otro aunque caigan en la misma fila.
En claro los dos del dinero bajan de tono para contrastar contra el blanco de
las fichas; en oscuro suben.

El **signo** (`+`, `−`, `↔`) y la **etiqueta de tipo** van siempre, con color o
sin él. El color acelera la lectura; no la sostiene. Quien no distinga verde de
rojo sigue leyendo lo mismo.

Y ojo: el índigo **no es "positivo" ni "negativo"**. Es "atiéndeme" — una deuda
con saldo, una cuota vencida, un cobro de hoy. (La variable se llama `--ambar`
por razones históricas; el nombre quedó de una paleta anterior.)

Todo sale de un bloque marcado `LOS COLORES DEL DINERO` y de sus dos líneas
gemelas en el tema oscuro.

## Los temas

El claro es el de base (`:root`); el oscuro redefine las variables en
`:root[data-tema='oscuro']`, que es el atributo que pone
[`TemaContext.jsx`](../frontend/src/lib/TemaContext.jsx).

De noche el fondo es un **casi negro azulado** y las fichas quedan un par de
tonos por encima para que sigan flotando. Los tres colores suben de
luminosidad: los de día, sobre fondo oscuro, se hunden. La rampa del acento se
**invierte** — el paso que en claro es el oscuro pasa a ser el claro.

## Las tipografías

**Fraunces** para los títulos y las cifras grandes, **Figtree** para todo lo
demás. La serif da el carácter; la sans hace que se sienta una app y no un
libro.

Se sirven **desde la app** (`frontend/public/fuentes/`), no desde Google: son
dos archivos variables con el subconjunto latino, y están en la precarga del
service worker. La app es una PWA que tiene que abrir sin señal, y con las
letras en un servidor ajeno se vería con la del sistema justo cuando más
importa.

Para actualizarlas: se bajan de nuevo con el subconjunto `latin` y se
reemplazan los `.woff2`. El `unicode-range` del `@font-face` tiene que seguir
coincidiendo.

## Cómo agregar una pantalla

Reusá lo que ya está antes de inventar una clase:

| Para | Usá |
|---|---|
| Una ficha | `.tarjeta` |
| Una cifra que se lee como cifra | `.fig` (o `.monto-valor` en la lista) |
| El rótulo pequeño sobre un título | `.kicker` |
| Una etiqueta | `.etiqueta` + su `.tipo-*`, o `.estado` |
| Un grupo de opciones | `.grupo-tipos` + `.chip` (es un segmentado) |
| La cifra protagonista | `.metrica.principal` o `.destacado-oro` |
| Una proporción al pie de una cifra | `.barra-oro` |
| Un aviso | `.alerta` (o `.alerta.aviso`) |

El encabezado de cada pantalla es siempre igual: un `.kicker` con el dato que
se viene a buscar, y debajo el `<h1>`.

## Historia

Vale la pena saber por dónde pasó, para no repetir el camino:

1. **Primera versión (Classical).** Papel casi blanco, Cormorant Garamond sobre
   Lora, cero rellenos, filetes de un pixel, oro como único color. Muy formal y
   **muy plano**.
2. **Se quitaron el verde y el rojo** del dinero, siguiendo la paleta mono al
   pie de la letra. En pantalla no funcionó: con todo en tinta, una lista de
   movimientos se lee mucho más lento. Volvieron.
3. **Se cambió la piel** conservando la organización —que era lo bueno del
   rediseño—: más volumen, tipografía menos solemne y una paleta de tierra
   (arena, barro, salvia, vino).
4. **La paleta de tierra tampoco funcionó.** El café no cuadraba con el verde y
   el rojo del dinero: los tres juntos se veían sucios, y en modo claro peor.
   Las letras y las formas de ese paso sí quedaron, y son las de hoy.
5. **Versión actual.** Misma organización, mismas letras, mismas formas; paleta
   fría de grises azulados con índigo, menta y rojo.
