import { useState } from 'react'
import { formatearFechaCorta, formatearMonto } from '../lib/formato'

// Las gráficas de la sección Cierres.
//
// Todas se dibujan sobre los cierres que ya están filtrados en pantalla: si
// filtras por este mes, las gráficas son de este mes. No piden nada al
// servidor — lo que se ve es lo que hay.
//
// Los colores son los mismos del dinero en toda la app (verde lo que entra,
// rojo lo que sale) y en oscuro cambian a un paso más apagado: los tonos
// claros se despegan del fondo y dejan de distinguirse entre sí para quien no
// ve bien los colores. Están comprobados para daltonismo en los dos temas.

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

// Un tope redondo para el eje: con 6.420.000 el eje llega a 7.000.000 y no a
// una cifra que nadie reconoce.
function escala(max) {
  if (max <= 0) return { tope: 1, pasos: [0, 1] }
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
    const dia = mapa.get(c.fecha) ?? { fecha: c.fecha, ventas: 0, gastos: 0, descuadre: 0, tiendas: [] }
    dia.ventas += num(c.venta_tienda)
    dia.gastos += num(c.totales.salidas)
    dia.descuadre += num(c.totales.queda_diferencia)
    dia.tiendas.push({
      nombre: c.tienda_nombre,
      ventas: num(c.venta_tienda),
      gastos: num(c.totales.salidas),
      descuadre: num(c.totales.queda_diferencia),
    })
    mapa.set(c.fecha, dia)
  }
  return [...mapa.values()].sort((a, b) => a.fecha.localeCompare(b.fecha))
}

function porTienda(cierres) {
  const mapa = new Map()
  for (const c of cierres) {
    const t = mapa.get(c.tienda_id) ?? { id: c.tienda_id, nombre: c.tienda_nombre, ventas: 0, gastos: 0, hojas: 0 }
    t.ventas += num(c.venta_tienda)
    t.gastos += num(c.totales.salidas)
    t.hojas += 1
    mapa.set(c.tienda_id, t)
  }
  return [...mapa.values()].sort((a, b) => b.ventas - a.ventas)
}

export default function GraficasCierres({ cierres }) {
  const [vista, setVista] = useState('dia')
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

  const mostrar = (e, contenido) => {
    setGlobo({
      contenido,
      x: Math.min(e.clientX + 14, window.innerWidth - 250),
      y: Math.max(e.clientY - 12, 60),
    })
  }
  const esconder = () => setGlobo(null)
  const globoProps = (contenido) => ({
    onMouseEnter: (e) => mostrar(e, contenido),
    onMouseMove: (e) => mostrar(e, contenido),
    onMouseLeave: esconder,
  })

  return (
    <section className="tarjeta">
      <div className="encabezado-seccion">
        <h2>{actual?.etiqueta ?? 'Gráficas'}</h2>
        <span className="tenue">Con lo que estás viendo</span>
      </div>

      <div className="tabs-grafica" role="tablist">
        {vistas.map((v) => (
          <button
            key={v.clave}
            type="button"
            role="tab"
            aria-selected={vistaActual === v.clave}
            className={`secundario ${vistaActual === v.clave ? 'activo' : ''}`}
            onClick={() => setVista(v.clave)}
          >
            {v.etiqueta}
          </button>
        ))}
      </div>

      <div className="lienzo-grafica">
        {vistaActual === 'dia' && <DiaADia dias={dias} globoProps={globoProps} />}
        {vistaActual === 'local' && <PorLocal dias={dias} tiendas={tiendas} globoProps={globoProps} />}
        {vistaActual === 'comparar' && <LocalContraLocal tiendas={tiendas} globoProps={globoProps} />}
        {vistaActual === 'descuadre' && <Descuadres dias={dias} globoProps={globoProps} />}
      </div>

      <Leyenda vista={vistaActual} tiendas={tiendas} />

      {globo && (
        <div className="globo-grafica" style={{ left: globo.x, top: globo.y }} role="status">
          {globo.contenido}
        </div>
      )}
    </section>
  )
}

function Leyenda({ vista, tiendas }) {
  if (vista === 'local') {
    return (
      <div className="leyenda-grafica">
        {tiendas.map((t, i) => (
          <span key={t.id}>
            <i className={`muestra-grafica linea serie-${i % 4}`} />
            {t.nombre}
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
        <i className="muestra-grafica gastos" /> Gastos
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
      <g key={valor}>
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
    <>
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
    </>
  )
}

/* ------------------------------ A: día a día --------------------------- */

function DiaADia({ dias, globoProps }) {
  const esc = escala(Math.max(...dias.map((d) => d.ventas), 0))
  const paso = CAJA.ancho / Math.max(dias.length, 1)
  const ancho = Math.max(3, Math.min(14, (paso - 8) / 2))

  return (
    <svg viewBox="0 0 820 300" role="img" aria-label="Ventas y gastos de cada día">
      <EjeY escalado={esc} />
      {dias.map((d, i) => {
        const centro = CAJA.izq + i * paso + paso / 2
        const altoV = (d.ventas / esc.tope) * CAJA.alto
        const altoG = (d.gastos / esc.tope) * CAJA.alto
        return (
          <g key={d.fecha}>
            {/* 2px de aire entre las dos: pegadas se leen como una sola. */}
            <path d={barraArriba(centro - ancho - 1, CAJA.fondo - altoV, ancho, altoV)} className="marca ventas" />
            <path d={barraArriba(centro + 1, CAJA.fondo - altoG, ancho, altoG)} className="marca gastos" />
            <rect
              x={CAJA.izq + i * paso}
              y={CAJA.arriba}
              width={paso}
              height={CAJA.alto}
              className="zona"
              {...globoProps(
                <>
                  <strong>{formatearFechaCorta(d.fecha)}</strong>
                  <br />
                  Ventas {formatearMonto(d.ventas)}
                  <br />
                  Gastos {formatearMonto(d.gastos)}
                  <br />
                  Quedó {formatearMonto(d.ventas - d.gastos)}
                </>,
              )}
            />
          </g>
        )
      })}
      <EjeDias dias={dias} paso={paso} />
    </svg>
  )
}

/* ------------------------------ C: por local --------------------------- */

function PorLocal({ dias, tiendas, globoProps }) {
  const enX = (i, paso) => CAJA.izq + i * paso + paso / 2
  const paso = CAJA.ancho / Math.max(dias.length, 1)

  // Una serie por tienda, con el día en blanco cuando esa tienda no cerró:
  // unir dos días salteados dibujaría una venta que no existió.
  const series = tiendas.map((t) => ({
    ...t,
    puntos: dias.map((d) => {
      const suyo = d.tiendas.find((x) => x.nombre === t.nombre)
      return suyo ? suyo.ventas : null
    }),
  }))

  const esc = escala(Math.max(...series.flatMap((s) => s.puntos.map((p) => p ?? 0)), 0))
  const enY = (v) => CAJA.fondo - (v / esc.tope) * CAJA.alto

  return (
    <svg viewBox="0 0 820 300" role="img" aria-label="Ventas de cada local, día por día">
      <EjeY escalado={esc} />
      {series.map((s, indice) => {
        const tramos = []
        let tramo = []
        s.puntos.forEach((v, i) => {
          if (v === null) {
            if (tramo.length) tramos.push(tramo)
            tramo = []
          } else {
            tramo.push([enX(i, paso), enY(v)])
          }
        })
        if (tramo.length) tramos.push(tramo)

        return (
          <g key={s.id} className={`serie-${indice % 4}`}>
            {tramos.map((t, i) => (
              <path
                key={i}
                d={t.map(([x, y], j) => `${j === 0 ? 'M' : 'L'}${x} ${y}`).join(' ')}
                className="linea-serie"
              />
            ))}
            {s.puntos.map((v, i) =>
              v === null ? null : (
                <circle key={i} cx={enX(i, paso)} cy={enY(v)} r="4.5" className="punto-serie" />
              ),
            )}
          </g>
        )
      })}

      {dias.map((d, i) => (
        <rect
          key={d.fecha}
          x={CAJA.izq + i * paso}
          y={CAJA.arriba}
          width={paso}
          height={CAJA.alto}
          className="zona"
          {...globoProps(
            <>
              <strong>{formatearFechaCorta(d.fecha)}</strong>
              {d.tiendas.map((t) => (
                <span key={t.nombre}>
                  <br />
                  {t.nombre} {formatearMonto(t.ventas)}
                </span>
              ))}
            </>,
          )}
        />
      ))}
      <EjeDias dias={dias} paso={paso} />
    </svg>
  )
}

/* -------------------------- E: local contra local ---------------------- */

function LocalContraLocal({ tiendas, globoProps }) {
  const izq = 118
  const der = 690
  const ancho = der - izq
  const esc = escala(Math.max(...tiendas.map((t) => t.ventas), 0))
  const alto = 40 + tiendas.length * 74

  const barra = (valor, arriba, clase) => {
    const largo = Math.max((valor / esc.tope) * ancho, 2)
    const r = Math.min(4, largo)
    return (
      <>
        <path
          className={`marca ${clase}`}
          d={`M${izq} ${arriba} L${izq + largo - r} ${arriba} Q${izq + largo} ${arriba} ${izq + largo} ${arriba + r} L${izq + largo} ${arriba + 14 - r} Q${izq + largo} ${arriba + 14} ${izq + largo - r} ${arriba + 14} L${izq} ${arriba + 14} Z`}
        />
        <text x={izq + largo + 10} y={arriba + 11} className="valor-barra">
          {formatearMonto(valor)}
        </text>
      </>
    )
  }

  return (
    <svg viewBox={`0 0 820 ${alto}`} role="img" aria-label="Ventas y gastos de cada local">
      {tiendas.map((t, i) => {
        const y = 28 + i * 74
        return (
          <g key={t.id}>
            <text x={izq - 12} y={y + 14} textAnchor="end" className="nombre-barra">
              {t.nombre}
            </text>
            {barra(t.ventas, y, 'ventas')}
            {barra(t.gastos, y + 18, 'gastos')}
            <rect
              x={izq}
              y={y - 8}
              width={ancho + 100}
              height={48}
              className="zona"
              {...globoProps(
                <>
                  <strong>{t.nombre}</strong>
                  <br />
                  Ventas {formatearMonto(t.ventas)}
                  <br />
                  Gastos {formatearMonto(t.gastos)}
                  <br />
                  Quedó {formatearMonto(t.ventas - t.gastos)} · {t.hojas} cierre
                  {t.hojas === 1 ? '' : 's'}
                </>,
              )}
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
    <svg viewBox="0 0 820 260" role="img" aria-label="Diferencia de cada cierre">
      {/* Aquí el cero va en la mitad: el dato tiene dos lados (sobró o faltó)
          y esa línea es justo la que hay que mirar. */}
      {[
        [tope, caja.arriba],
        [0, medio],
        [-tope, caja.fondo],
      ].map(([valor, y]) => (
        <g key={valor}>
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
              >
                {formatearMonto(d.descuadre)}
              </text>
            )}
            <rect
              x={caja.izq + i * paso}
              y={caja.arriba}
              width={paso}
              height={caja.alto}
              className="zona"
              {...globoProps(
                <>
                  <strong>{formatearFechaCorta(d.fecha)}</strong>
                  <br />
                  {cuadra ? (
                    'Cuadró exacto'
                  ) : (
                    <>
                      {d.descuadre < 0 ? 'Faltaron ' : 'Sobraron '}
                      {formatearMonto(Math.abs(d.descuadre))}
                    </>
                  )}
                  {d.tiendas.length > 1 &&
                    d.tiendas.map((t) => (
                      <span key={t.nombre}>
                        <br />
                        {t.nombre} {formatearMonto(t.descuadre)}
                      </span>
                    ))}
                </>,
              )}
            />
          </g>
        )
      })}

      <EjeDias dias={dias} paso={paso} caja={caja} />
    </svg>
  )
}
