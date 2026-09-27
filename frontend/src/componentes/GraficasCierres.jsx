import { useEffect, useId, useState } from 'react'
import { formatearFechaCorta, formatearMonto } from '../lib/formato'

// Las gráficas de la sección Cierres.
//
// Todas se dibujan sobre los cierres que ya están filtrados en pantalla: si
// filtras por este mes, las gráficas son de este mes. No piden nada al
// servidor — lo que se ve es lo que hay.
//
// Donde una serie es plata que entra o que sale, lleva los colores del dinero
// (verde y rojo) y además un lugar fijo: ventas a la izquierda, lo que salió a
// la derecha; lo que sobró arriba del cero, lo que faltó abajo. En "Por local"
// ninguna línea es entrada ni salida, así que ahí no hay verde ni rojo: cada
// local lleva su tono, su trazo, su forma de punto y su nombre al final de la
// línea. Ninguna gráfica se lee solo por el color.

// Una línea que dice qué muestra cada gráfica, debajo de ella.
const PIES = {
  dia: 'Lo que se vendió y lo que salió de la caja cada día.',
  local: 'Las ventas de cada local, día por día.',
  comparar: 'Lo que vendió y lo que salió en cada local, en todo el rango.',
  descuadre: 'Cuánto sobró o faltó al contar la caja cada día.',
}

const VISTAS = [
  { clave: 'dia', etiqueta: 'Día a día' },
  { clave: 'local', etiqueta: 'Por local' },
  { clave: 'comparar', etiqueta: 'Local contra local' },
  { clave: 'descuadre', etiqueta: 'Descuadres' },
]

// El lienzo. Las medidas son del viewBox, no píxeles: el SVG se estira al
// ancho que haya y adentro todo sigue cuadrando.
const CAJA = { izq: 78, der: 806, arriba: 14, fondo: 248 }
CAJA.alto = CAJA.fondo - CAJA.arriba
CAJA.ancho = CAJA.der - CAJA.izq

const num = (v) => Number(v) || 0

// Lo que dice una columna: un título y unas líneas. De aquí salen las dos
// versiones, el globo para el que mira y la frase para el lector de pantalla,
// así que no pueden decir cosas distintas.
const enTexto = (dato) => [dato.titulo, ...dato.lineas].join('. ')

// La zona sensible de una columna o de un local. Con el ratón abre el globo;
// con el teclado se llega con Tab a la primera y las flechas recorren las
// demás: un año de días serían 365 paradas de Tab antes de llegar a la tabla.
function Zona({ dato, primera, globoProps, ...caja }) {
  return (
    <rect
      {...caja}
      className="zona"
      role="img"
      aria-label={enTexto(dato)}
      tabIndex={primera ? 0 : -1}
      {...globoProps(dato)}
    />
  )
}

function recorrerZonas(e) {
  const siguiente = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key]
  if (!siguiente && e.key !== 'Home' && e.key !== 'End') return
  const zonas = [...e.currentTarget.ownerSVGElement.querySelectorAll('.zona')]
  const i = zonas.indexOf(e.currentTarget)
  const destino =
    e.key === 'Home'
      ? 0
      : e.key === 'End'
        ? zonas.length - 1
        : Math.min(Math.max(i + siguiente, 0), zonas.length - 1)
  e.preventDefault()
  zonas[destino].focus()
}

// Un tope redondo para el eje: con 6.420.000 el eje llega a 7.000.000 y no a
// una cifra que nadie reconoce. Nunca por debajo de mil pesos: un rango todo en
// cero pintaba un eje de "$ 0" a "$ 1", y con montos de un dígito salían
// rótulos como "$ 0,5".
function escala(max) {
  max = Math.max(max, 1000)
  const magnitud = Math.pow(10, Math.floor(Math.log10(max)))
  const tope = Math.ceil(max / (magnitud / 2)) * (magnitud / 2)
  return { tope, pasos: [0, tope / 2, tope] }
}

// En el eje no caben los pesos completos.
function ejeCorto(n) {
  if (n === 0) return '$ 0'
  const abs = Math.abs(n)
  const signo = n < 0 ? '−' : ''
  if (abs >= 1000000) return `${signo}$ ${(abs / 1000000).toFixed(1).replace('.', ',')} M`
  if (abs >= 1000) return `${signo}$ ${Math.round(abs / 1000)} mil`
  return `${signo}$ ${Math.round(abs)}`
}

// Solo el día del mes: en un rango largo, "14/09/2026" catorce veces no cabe.
const soloDia = (iso) => iso.slice(8, 10)

// Barra con las puntas de arriba redondeadas y el pie pegado al cero. Una
// barra redondeada por los cuatro lados flota y miente sobre dónde empieza.
function barraArriba(x, y, ancho, alto, radio = 4) {
  if (alto <= 0.5) return `M${x} ${y - 1} h${ancho} v1.5 h${-ancho} Z`
  const r = Math.min(radio, ancho / 2, alto)
  return `M${x} ${y + alto} L${x} ${y + r} Q${x} ${y} ${x + r} ${y} L${x + ancho - r} ${y} Q${x + ancho} ${y} ${x + ancho} ${y + r} L${x + ancho} ${y + alto} Z`
}

function barraAbajo(x, y, ancho, alto, radio = 4) {
  if (alto <= 0.5) return `M${x} ${y} h${ancho} v1.5 h${-ancho} Z`
  const r = Math.min(radio, ancho / 2, alto)
  return `M${x} ${y} L${x + ancho} ${y} L${x + ancho} ${y + alto - r} Q${x + ancho} ${y + alto} ${x + ancho - r} ${y + alto} L${x + r} ${y + alto} Q${x} ${y + alto} ${x} ${y + alto - r} Z`
}

// Junta los cierres por día. Vienen del más nuevo al más viejo y una gráfica
// se lee al revés, así que aquí se ordenan.
function porDia(cierres) {
  const mapa = new Map()
  for (const c of cierres) {
    const dia = mapa.get(c.fecha) ?? { fecha: c.fecha, ventas: 0, salidas: 0, descuadre: 0, tiendas: [] }
    dia.ventas += num(c.venta_tienda)
    dia.salidas += num(c.totales.salidas)
    dia.descuadre += num(c.totales.queda_diferencia)
    dia.tiendas.push({
      nombre: c.tienda_nombre,
      ventas: num(c.venta_tienda),
      salidas: num(c.totales.salidas),
      descuadre: num(c.totales.queda_diferencia),
    })
    mapa.set(c.fecha, dia)
  }
  return [...mapa.values()].sort((a, b) => a.fecha.localeCompare(b.fecha))
}

function porTienda(cierres) {
  const mapa = new Map()
  for (const c of cierres) {
    const t = mapa.get(c.tienda_id) ?? { id: c.tienda_id, nombre: c.tienda_nombre, ventas: 0, salidas: 0, hojas: 0 }
    t.ventas += num(c.venta_tienda)
    t.salidas += num(c.totales.salidas)
    t.hojas += 1
    mapa.set(c.tienda_id, t)
  }
  return [...mapa.values()].sort((a, b) => b.ventas - a.ventas)
}

export default function GraficasCierres({ cierres }) {
  const [vista, setVista] = useState('dia')
  const base = useId()
  // El globo del cursor: uno solo para las cuatro gráficas.
  const [globo, setGlobo] = useState(null)

  const dias = porDia(cierres)
  const tiendas = porTienda(cierres)

  // Comparar locales no tiene sentido con uno solo: sería una barra sola.
  const vistas = tiendas.length > 1 ? VISTAS : VISTAS.filter((v) => v.clave !== 'comparar')
  // Si la vista elegida deja de existir (se filtró por una sola tienda con
  // "Local contra local" abierto), se cae a la de siempre sin tocar el estado:
  // cambiarlo durante el dibujo es pedirle a React que vuelva a empezar.
  const vistaActual = vistas.some((v) => v.clave === vista) ? vista : 'dia'
  const actual = vistas.find((v) => v.clave === vistaActual)

  // El globo se queda donde entró el cursor: seguirlo en cada movimiento era
  // volver a dibujar las cuatro gráficas por cada píxel. Desde el teclado no
  // hay cursor, así que se pone encima de la columna enfocada.
  const mostrar = (e, dato) => {
    let x = e.clientX
    let y = e.clientY
    if (typeof x !== 'number' || (x === 0 && y === 0)) {
      const caja = e.currentTarget.getBoundingClientRect()
      x = caja.left + caja.width / 2
      y = caja.top
    }
    setGlobo({
      dato,
      x: Math.min(x + 14, window.innerWidth - 250),
      y: Math.max(y - 12, 60),
    })
  }
  const esconder = () => setGlobo(null)
  const globoProps = (dato) => ({
    onMouseEnter: (e) => mostrar(e, dato),
    onMouseLeave: esconder,
    onFocus: (e) => mostrar(e, dato),
    onBlur: esconder,
    // En el teléfono no hay "salir con el ratón": el toque abre y tocar
    // afuera cierra (abajo).
    onClick: (e) => mostrar(e, dato),
    onKeyDown: recorrerZonas,
  })

  // Ahora que el globo se abre sin ratón, también se cierra sin él: Escape,
  // o tocar cualquier cosa que no sea una columna.
  const hayGlobo = globo !== null
  useEffect(() => {
    if (!hayGlobo) return
    const conTecla = (e) => e.key === 'Escape' && setGlobo(null)
    const conToque = (e) => !e.target.closest?.('.zona') && setGlobo(null)
    document.addEventListener('keydown', conTecla)
    document.addEventListener('pointerdown', conToque)
    return () => {
      document.removeEventListener('keydown', conTecla)
      document.removeEventListener('pointerdown', conToque)
    }
  }, [hayGlobo])

  // Las pestañas se recorren con las flechas, como promete el role="tablist".
  const idPestana = (clave) => `${base}-pestana-${clave}`
  const idPanel = `${base}-panel`
  const moverPestana = (e) => {
    const i = vistas.findIndex((v) => v.clave === vistaActual)
    const destino = {
      ArrowRight: (i + 1) % vistas.length,
      ArrowLeft: (i - 1 + vistas.length) % vistas.length,
      Home: 0,
      End: vistas.length - 1,
    }[e.key]
    if (destino === undefined) return
    e.preventDefault()
    setVista(vistas[destino].clave)
    document.getElementById(idPestana(vistas[destino].clave))?.focus()
  }

  return (
    <section className="tarjeta">
      <div className="encabezado-seccion">
        <h2>{actual?.etiqueta ?? 'Gráficas'}</h2>
        <span className="tenue">Con lo que estás viendo</span>
      </div>

      <div className="tabs-grafica" role="tablist" aria-label="Gráficas" onKeyDown={moverPestana}>
        {vistas.map((v) => (
          <button
            key={v.clave}
            id={idPestana(v.clave)}
            type="button"
            role="tab"
            aria-selected={vistaActual === v.clave}
            aria-controls={idPanel}
            tabIndex={vistaActual === v.clave ? 0 : -1}
            className={`secundario ${vistaActual === v.clave ? 'activo' : ''}`}
            onClick={() => setVista(v.clave)}
          >
            {v.etiqueta}
          </button>
        ))}
      </div>

      <div id={idPanel} role="tabpanel" aria-labelledby={idPestana(vistaActual)}>
        {/* La gráfica es para ver la forma; para leer las cifras una por una
            está la tabla de abajo, y la figura lo dice. */}
        <figure className="figura-grafica">
          <div className="lienzo-grafica">
            {vistaActual === 'dia' && <DiaADia dias={dias} globoProps={globoProps} />}
            {vistaActual === 'local' && <PorLocal dias={dias} tiendas={tiendas} globoProps={globoProps} />}
            {vistaActual === 'comparar' && <LocalContraLocal tiendas={tiendas} globoProps={globoProps} />}
            {vistaActual === 'descuadre' && <Descuadres dias={dias} globoProps={globoProps} />}
          </div>
          <Leyenda vista={vistaActual} tiendas={tiendas} />
          <figcaption className="tenue">
            {PIES[vistaActual]} Las mismas cifras, cierre por cierre, están en la tabla de abajo.
          </figcaption>
        </figure>
      </div>

      {/* Fuera del árbol de accesibilidad: el lector ya leyó la misma frase en
          la columna enfocada, y oírla dos veces no ayuda. */}
      {globo && (
        <div className="globo-grafica" style={{ left: globo.x, top: globo.y }} aria-hidden="true">
          <strong>{globo.dato.titulo}</strong>
          {globo.dato.lineas.map((linea, i) => (
            <span key={i}>
              <br />
              {linea}
            </span>
          ))}
        </div>
      )}
    </section>
  )
}

// Más de cuatro líneas cruzándose ya no se leen, y la quinta repetiría el
// tono y el trazo de la primera. Van los cuatro locales que más vendieron (ya
// vienen ordenados así) y el resto junto en una línea aparte que lo dice.
const MAX_LOCALES = 4

function seriesDeLocales(tiendas) {
  const series = tiendas.slice(0, MAX_LOCALES).map((t, i) => ({
    id: t.id,
    nombre: t.nombre,
    clase: `serie-${i}`,
    forma: i,
    nombres: [t.nombre],
  }))
  const resto = tiendas.slice(MAX_LOCALES)
  if (resto.length) {
    series.push({
      id: 'resto',
      nombre: resto.length === 1 ? resto[0].nombre : `Otros ${resto.length} locales`,
      clase: 'serie-resto',
      forma: 'resto',
      nombres: resto.map((t) => t.nombre),
    })
  }
  return series
}

// La muestra de la leyenda es un pedazo de la misma línea, con su trazo.
function MuestraLinea({ clase }) {
  return (
    <svg className={`muestra-linea ${clase}`} viewBox="0 0 24 8" aria-hidden="true">
      <line x1="2" y1="4" x2="22" y2="4" className="linea-serie" />
    </svg>
  )
}

function Leyenda({ vista, tiendas }) {
  if (vista === 'local') {
    return (
      <div className="leyenda-grafica">
        {seriesDeLocales(tiendas).map((s) => (
          <span key={s.id}>
            <MuestraLinea clase={s.clase} />
            {s.nombre}
          </span>
        ))}
      </div>
    )
  }
  if (vista === 'descuadre') {
    return (
      <div className="leyenda-grafica">
        <span>
          <i className="muestra-grafica gastos" /> Faltó
        </span>
        <span>
          <i className="muestra-grafica tercera" /> Sobró
        </span>
      </div>
    )
  }
  return (
    <div className="leyenda-grafica">
      <span>
        <i className="muestra-grafica ventas" /> Ventas
      </span>
      <span>
        <i className="muestra-grafica gastos" /> Salió
      </span>
    </div>
  )
}

// El eje de la izquierda con sus líneas guía, recesivas: la tinta fuerte es
// para los datos.
function EjeY({ escalado, caja = CAJA }) {
  return escalado.pasos.map((valor) => {
    const y = caja.fondo - (valor / escalado.tope) * caja.alto
    return (
      <g key={valor} aria-hidden="true">
        <line
          x1={caja.izq}
          y1={y}
          x2={caja.der}
          y2={y}
          className={valor === 0 ? 'eje-cero' : 'eje-guia'}
        />
        <text x={caja.izq - 10} y={y + 4} textAnchor="end" className="eje-rotulo">
          {ejeCorto(valor)}
        </text>
      </g>
    )
  })
}

// Los días de abajo. Con muchos, se saltan rótulos en vez de encimarlos.
function EjeDias({ dias, paso, caja = CAJA, desplazado = 0 }) {
  const salto = Math.ceil(dias.length / 16)
  return (
    <g aria-hidden="true">
      {dias.map((d, i) =>
        i % salto === 0 ? (
          <text
            key={d.fecha}
            x={caja.izq + i * paso + paso / 2}
            y={caja.fondo + 20 + desplazado}
            textAnchor="middle"
            className="eje-rotulo"
          >
            {soloDia(d.fecha)}
          </text>
        ) : null,
      )}
      {dias.length > 0 && (
        <text x={caja.der} y={caja.fondo + 38 + desplazado} textAnchor="end" className="eje-rotulo">
          {formatearFechaCorta(dias[0].fecha)} — {formatearFechaCorta(dias[dias.length - 1].fecha)}
        </text>
      )}
    </g>
  )
}

/* ------------------------------ A: día a día --------------------------- */

function DiaADia({ dias, globoProps }) {
  // El tope cubre las dos series: un día que salió más de lo que entró es
  // justo el que uno quiere ver, y con el tope de las ventas se salía del lienzo.
  const esc = escala(Math.max(...dias.flatMap((d) => [d.ventas, d.salidas]), 0))
  const paso = CAJA.ancho / Math.max(dias.length, 1)
  const ancho = Math.max(3, Math.min(14, (paso - 8) / 2))
  // Por si algún día se agrega una serie y el tope se desincroniza: mejor una
  // barra recortada al tope, que el eje sigue diciendo dónde está, que una que
  // se monta sobre el título.
  const alto = (v) => Math.min((v / esc.tope) * CAJA.alto, CAJA.alto)

  return (
    <svg viewBox="0 0 820 300" role="group" aria-label="Ventas y lo que salió de cada día">
      <EjeY escalado={esc} />
      {dias.map((d, i) => {
        const centro = CAJA.izq + i * paso + paso / 2
        const altoV = alto(d.ventas)
        const altoG = alto(d.salidas)
        return (
          <g key={d.fecha}>
            {/* 2px de aire entre las dos: pegadas se leen como una sola. */}
            <path d={barraArriba(centro - ancho - 1, CAJA.fondo - altoV, ancho, altoV)} className="marca ventas" />
            <path d={barraArriba(centro + 1, CAJA.fondo - altoG, ancho, altoG)} className="marca gastos" />
            <Zona
              x={CAJA.izq + i * paso}
              y={CAJA.arriba}
              width={paso}
              height={CAJA.alto}
              primera={i === 0}
              globoProps={globoProps}
              dato={{
                titulo: formatearFechaCorta(d.fecha),
                lineas: [
                  `Ventas ${formatearMonto(d.ventas)}`,
                  `Salió ${formatearMonto(d.salidas)}`,
                  `Debería quedar ${formatearMonto(d.ventas - d.salidas)}`,
                ],
              }}
            />
          </g>
        )
      })}
      <EjeDias dias={dias} paso={paso} />
    </svg>
  )
}

/* ------------------------------ C: por local --------------------------- */

// El punto de cada serie tiene su forma: círculo, cuadrado, rombo y
// triángulo. Con el trazo, es la segunda señal para quien no distingue tonos.
function Punto({ forma, x, y }) {
  if (forma === 1) return <rect x={x - 4} y={y - 4} width="8" height="8" className="punto-serie" />
  if (forma === 2) return <path d={`M${x} ${y - 5.5} L${x + 5.5} ${y} L${x} ${y + 5.5} L${x - 5.5} ${y} Z`} className="punto-serie" />
  if (forma === 3) return <path d={`M${x} ${y - 5.5} L${x + 5} ${y + 4} L${x - 5} ${y + 4} Z`} className="punto-serie" />
  return <circle cx={x} cy={y} r={forma === 'resto' ? 3 : 4.5} className="punto-serie" />
}

// Los nombres al final de las líneas no se pueden montar uno encima de otro:
// se ordenan por altura y se separan lo justo, sin salirse del lienzo.
function separarRotulos(rotulos, arriba, fondo, aire = 15) {
  const orden = [...rotulos].sort((a, b) => a.y - b.y)
  orden.forEach((r, i) => {
    r.yRotulo = Math.max(r.y, arriba + 4, i ? orden[i - 1].yRotulo + aire : -Infinity)
  })
  for (let i = orden.length - 1; i >= 0; i--) {
    const techo = i === orden.length - 1 ? fondo : orden[i + 1].yRotulo - aire
    orden[i].yRotulo = Math.min(orden[i].yRotulo, techo)
  }
  return rotulos
}

// Un nombre largo no cabe en el margen: se corta con puntos suspensivos, y el
// completo sigue en la leyenda y en el globo.
const recortar = (texto, max = 17) => (texto.length > max ? `${texto.slice(0, max - 1)}…` : texto)

function PorLocal({ dias, tiendas, globoProps }) {
  // A la derecha queda aire para el nombre de cada local al final de su línea.
  const caja = { izq: 78, der: 676, arriba: 14, fondo: 248 }
  caja.alto = caja.fondo - caja.arriba
  caja.ancho = caja.der - caja.izq
  const paso = caja.ancho / Math.max(dias.length, 1)
  const enX = (i) => caja.izq + i * paso + paso / 2

  // Una serie por tienda, con el día en blanco cuando esa tienda no cerró:
  // unir dos días salteados dibujaría una venta que no existió. La de "los
  // demás" suma los que sí cerraron ese día.
  const series = seriesDeLocales(tiendas).map((s) => ({
    ...s,
    puntos: dias.map((d) => {
      const suyos = d.tiendas.filter((x) => s.nombres.includes(x.nombre))
      return suyos.length ? suyos.reduce((suma, x) => suma + x.ventas, 0) : null
    }),
  }))

  const esc = escala(Math.max(...series.flatMap((s) => s.puntos.map((p) => p ?? 0)), 0))
  const enY = (v) => caja.fondo - (v / esc.tope) * caja.alto

  const rotulos = separarRotulos(
    series
      .map((s) => {
        const ultimo = s.puntos.findLastIndex((v) => v !== null)
        return ultimo < 0 ? null : { id: s.id, x: enX(ultimo), y: enY(s.puntos[ultimo]) }
      })
      .filter(Boolean),
    caja.arriba,
    caja.fondo,
  )
  const rotuloDe = (id) => rotulos.find((r) => r.id === id)

  return (
    <svg viewBox="0 0 820 300" role="group" aria-label="Ventas de cada local, día por día">
      <EjeY escalado={esc} caja={caja} />
      {series.map((s) => {
        const tramos = []
        let tramo = []
        s.puntos.forEach((v, i) => {
          if (v === null) {
            if (tramo.length) tramos.push(tramo)
            tramo = []
          } else {
            tramo.push([enX(i), enY(v)])
          }
        })
        if (tramo.length) tramos.push(tramo)
        const rotulo = rotuloDe(s.id)

        return (
          <g key={s.id} className={s.clase}>
            {tramos.map((t, i) => (
              <path
                key={i}
                d={t.map(([x, y], j) => `${j === 0 ? 'M' : 'L'}${x} ${y}`).join(' ')}
                className="linea-serie"
              />
            ))}
            {s.puntos.map((v, i) => (v === null ? null : <Punto key={i} forma={s.forma} x={enX(i)} y={enY(v)} />))}
            {rotulo && (
              <text x={caja.der + 12} y={rotulo.yRotulo + 4} className="rotulo-serie" aria-hidden="true">
                {recortar(s.nombre)}
              </text>
            )}
          </g>
        )
      })}

      {dias.map((d, i) => (
        <Zona
          key={d.fecha}
          x={caja.izq + i * paso}
          y={caja.arriba}
          width={paso}
          height={caja.alto}
          primera={i === 0}
          globoProps={globoProps}
          dato={{
            titulo: formatearFechaCorta(d.fecha),
            lineas: d.tiendas.map((t) => `${t.nombre} ${formatearMonto(t.ventas)}`),
          }}
        />
      ))}
      <EjeDias dias={dias} paso={paso} caja={caja} />
    </svg>
  )
}

/* -------------------------- E: local contra local ---------------------- */

function LocalContraLocal({ tiendas, globoProps }) {
  // A la derecha de der quedan 160 de aire para el rótulo: "−$ 999.999.999.999,99"
  // mide unos 125 a este tamaño de letra, más los 10 de separación.
  const izq = 118
  const der = 660
  const ancho = der - izq
  const esc = escala(Math.max(...tiendas.flatMap((t) => [t.ventas, t.salidas]), 0))
  const alto = 40 + tiendas.length * 74

  const barra = (valor, arriba, clase) => {
    const largo = Math.min(Math.max((valor / esc.tope) * ancho, 2), ancho)
    const r = Math.min(4, largo)
    return (
      <>
        <path
          className={`marca ${clase}`}
          d={`M${izq} ${arriba} L${izq + largo - r} ${arriba} Q${izq + largo} ${arriba} ${izq + largo} ${arriba + r} L${izq + largo} ${arriba + 14 - r} Q${izq + largo} ${arriba + 14} ${izq + largo - r} ${arriba + 14} L${izq} ${arriba + 14} Z`}
        />
        <text x={izq + largo + 10} y={arriba + 11} className="valor-barra" aria-hidden="true">
          {formatearMonto(valor)}
        </text>
      </>
    )
  }

  return (
    <svg viewBox={`0 0 820 ${alto}`} role="group" aria-label="Ventas y lo que salió de cada local">
      {tiendas.map((t, i) => {
        const y = 28 + i * 74
        return (
          <g key={t.id}>
            <text x={izq - 12} y={y + 14} textAnchor="end" className="nombre-barra" aria-hidden="true">
              {t.nombre}
            </text>
            {barra(t.ventas, y, 'ventas')}
            {barra(t.salidas, y + 18, 'gastos')}
            <Zona
              x={izq}
              y={y - 8}
              width={ancho + 140}
              height={48}
              primera={i === 0}
              globoProps={globoProps}
              dato={{
                titulo: t.nombre,
                lineas: [
                  `Ventas ${formatearMonto(t.ventas)}`,
                  `Salió ${formatearMonto(t.salidas)}`,
                  `Debería quedar ${formatearMonto(t.ventas - t.salidas)} · ${t.hojas} cierre${t.hojas === 1 ? '' : 's'}`,
                ],
              }}
            />
          </g>
        )
      })}
    </svg>
  )
}

/* ---------------------------- F: los descuadres ------------------------ */

function Descuadres({ dias, globoProps }) {
  const caja = { izq: 78, der: 806, arriba: 16, fondo: 206 }
  caja.alto = caja.fondo - caja.arriba
  caja.ancho = caja.der - caja.izq

  const tope = Math.max(...dias.map((d) => Math.abs(d.descuadre)), 1)
  const medio = caja.arriba + caja.alto / 2
  const mitad = caja.alto / 2
  const paso = caja.ancho / Math.max(dias.length, 1)
  const ancho = Math.max(3, Math.min(22, paso - 10))

  return (
    <svg viewBox="0 0 820 260" role="group" aria-label="Diferencia de cada cierre">
      {/* Aquí el cero va en la mitad: el dato tiene dos lados (sobró o faltó)
          y esa línea es justo la que hay que mirar. */}
      {[
        [tope, caja.arriba],
        [0, medio],
        [-tope, caja.fondo],
      ].map(([valor, y]) => (
        <g key={valor} aria-hidden="true">
          <line x1={caja.izq} y1={y} x2={caja.der} y2={y} className={valor === 0 ? 'eje-cero' : 'eje-guia'} />
          <text x={caja.izq - 10} y={y + 4} textAnchor="end" className="eje-rotulo">
            {ejeCorto(valor)}
          </text>
        </g>
      ))}

      {dias.map((d, i) => {
        const x = caja.izq + i * paso + (paso - ancho) / 2
        const alto = (Math.abs(d.descuadre) / tope) * mitad
        const cuadra = Math.round(d.descuadre) === 0
        return (
          <g key={d.fecha}>
            {!cuadra && (
              <path
                d={
                  d.descuadre > 0
                    ? barraArriba(x, medio - alto, ancho, alto)
                    : barraAbajo(x, medio, ancho, alto)
                }
                className={`marca ${d.descuadre > 0 ? 'tercera' : 'gastos'}`}
              />
            )}
            {!cuadra && dias.length <= 20 && (
              <text
                x={x + ancho / 2}
                y={d.descuadre > 0 ? medio - alto - 8 : medio + alto + 16}
                textAnchor="middle"
                className="valor-barra fuerte"
                aria-hidden="true"
              >
                {formatearMonto(d.descuadre)}
              </text>
            )}
            <Zona
              x={caja.izq + i * paso}
              y={caja.arriba}
              width={paso}
              height={caja.alto}
              primera={i === 0}
              globoProps={globoProps}
              dato={{
                titulo: formatearFechaCorta(d.fecha),
                lineas: [
                  cuadra
                    ? 'Cuadró exacto'
                    : `${d.descuadre < 0 ? 'Faltaron' : 'Sobraron'} ${formatearMonto(Math.abs(d.descuadre))}`,
                  ...(d.tiendas.length > 1
                    ? d.tiendas.map((t) => `${t.nombre} ${formatearMonto(t.descuadre)}`)
                    : []),
                ],
              }}
            />
          </g>
        )
      })}

      <EjeDias dias={dias} paso={paso} caja={caja} />
    </svg>
  )
}
