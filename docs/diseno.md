# El sistema visual

La app está vestida con **Classical**, un sistema editorial que salió de
[Claude Design](https://claude.ai/design). El bundle original está en
`Proyecto visual agradable-handoff.zip`, en la raíz del repo.

Todo vive en un solo archivo: [`frontend/src/estilos.css`](../frontend/src/estilos.css).
Los tokens están arriba del todo; el resto del archivo solo los lee.

## La idea

Una página de papel. Fondo casi blanco, Cormorant Garamond sobre Lora,
columnas con aire y **filetes de un pixel en vez de rellenos**. El color es uno
solo —un oro— y se usa como **trazo**: bordes, subrayados, y las cifras que
piden una acción.

## Las cinco reglas que no se rompen

1. **Nada se rellena de color.** Ni los botones ni las fichas. El botón
   primario es un borde de oro sobre transparente. Si algo necesita destacar,
   se le pone el borde en oro, no el fondo.

2. **El oro es para lo que pide algo.** Una deuda con saldo, una cuota vencida,
   un cobro de hoy, lo que falta confirmar. Si se colorea todo, el color deja
   de señalar nada.

3. **No hay negrita.** El énfasis lo hacen el peso *semibold* de los títulos
   (`--font-heading-weight`) y las itálicas. Y cuanto más grande es el texto,
   más liviano se pone: las cifras de display van en el corte normal.

4. **Las cifras van tabulares.** Siempre. En una columna, los dígitos tienen
   que caer unos debajo de otros o la lista deja de leerse de un vistazo. Lo
   hacen `.num`, `.fig` y `.monto-valor`.

5. **Todo sale de los tokens.** `var(--color-*)`, `var(--space-*)`,
   `var(--radius-*)`, `var(--shadow-*)`. Nunca un hex ni un px que el token ya
   traiga. La escala de espacio es aireada a propósito (densidad 1,15×).

## Los colores del dinero

Classical es una paleta **mono**: un solo acento, el oro. El dinero es la
única excepción, y se la ganó — en una lista de cifras, distinguir de un
vistazo lo que entró de lo que salió es media app.

Pero el verde y el rojo **no son los de una interfaz cualquiera**: son un verde
de tinta (`#3d6b34`) y un rojo de lacre (`#9e3b26`), elegidos para el fondo
cálido de este sistema. Un verde fluorescente al lado del oro se ve fuera de
lugar. Sobre tinta los dos suben de tono, igual que la rampa del oro.

Lo que se conserva del rediseño es tan importante como el color: el **signo**
(`+`, `−`, `↔`) y la **etiqueta de tipo** van siempre, haya color o no. El
color acelera la lectura; no la sostiene. Quien no distinga verde de rojo sigue
leyendo exactamente lo mismo.

**El oro no es ni positivo ni negativo: es "atiéndeme".** Se queda para lo que
pide una acción — una deuda con saldo, una cuota vencida, un cobro de hoy. Si
se usa para decorar, deja de señalar.

Todo sale de **un solo bloque** de `estilos.css`, marcado
`LOS COLORES DEL DINERO`, y de sus dos líneas gemelas en el tema oscuro. Las
clases son `.positivo`, `.negativo`, `.advertencia` (el oro) y `.neutro` (un
traslado, que no suma ni resta).

> Nota de historia: la primera versión del rediseño quitó el verde y el rojo
> del todo, siguiendo la paleta mono al pie de la letra. En pantalla no
> funcionó: con todo en tinta, una lista de movimientos se lee mucho más
> lento. Los colores volvieron, en tono editorial.

## Los temas

El claro es el de base (`:root`); el oscuro redefine las variables en
`:root[data-tema='oscuro']`, que es el atributo que pone
[`TemaContext.jsx`](../frontend/src/lib/TemaContext.jsx).

Sobre tinta, **la rampa del oro se invierte**: el paso que en claro es el
oscuro (`--color-accent-700`) pasa a ser el claro. Si no, el texto en acento
quedaría ilegible sobre el fondo negro.

## Las tipografías

Se sirven **desde la app** (`frontend/public/fuentes/`), no desde Google.

Son dos archivos variables de ~37 KB —cada uno cubre sus dos pesos— con el
subconjunto latino, que es el que necesita el español. Están en la precarga del
service worker.

Dos razones para no usar el CDN: la app es una PWA que tiene que abrir sin
señal, y con las letras en un servidor ajeno se vería con la serif del sistema
justo cuando más importa; y así ninguna visita le cuenta a un tercero quién
abre sus finanzas.

Para actualizarlas, se bajan de nuevo con el subconjunto `latin` y se
reemplazan los dos `.woff2`. El `unicode-range` del `@font-face` tiene que
seguir coincidiendo.

## Cómo agregar una pantalla

Reusa lo que ya está antes de inventar una clase:

| Para | Usa |
|---|---|
| Una ficha | `.tarjeta` |
| Una cifra que se lee como cifra | `.fig` (o `.monto-valor` en la lista) |
| El rótulo en versalitas sobre un título | `.kicker` |
| Una etiqueta pequeña | `.etiqueta` + su `.tipo-*`, o `.estado` |
| Un grupo de opciones | `.grupo-tipos` + `.chip` (es un segmentado: van pegadas) |
| La cifra protagonista de la pantalla | `.metrica.principal` o `.destacado-oro` |
| Una proporción al pie de una cifra | `.barra-oro` |
| Un aviso | `.alerta` (filete de oro a la izquierda) |

El encabezado de cada pantalla es siempre igual: un `.kicker` con el dato que
se viene a buscar, y debajo el `<h1>`.

## Lo que quedó fuera

El bundle traía 25 pantallas y están todas. Lo que no se trajo del sistema:

- **Los iconos de Lucide.** La app sigue con emojis en la barra (🔔 🔑 📳). Es
  lo que el sistema pide cambiar, pero meter una librería de iconos por seis
  botones no compensa en una app que corre en una Raspberry.
- **La clase `.plate`** para fotografías. La única imagen de la app es la
  factura adjunta, y su visor ya usa el mismo tratamiento de paspartú a mano.
