import { Link } from 'react-router-dom'
import { abrirTutorial } from '../lib/eventos'
import Icono from './Icono'

// La lista de primeros pasos del Resumen.
//
// El tutorial explica; esta lista acompaña. Se marca sola con lo que la
// persona ya hizo —no con lo que leyó— y siempre dice cuál es el SIGUIENTE
// paso, con su botón. Cuando los tres están hechos desaparece: ya no hace
// falta y ocuparía la parte más valiosa de la pantalla.
export default function PrimerosPasos({ categorias, medios, onRegistrar }) {
  const hayMovimientos = categorias.some((c) => c.movimientos > 0)

  const pasos = [
    {
      hecho: categorias.length > 0,
      titulo: 'Crea tus categorías',
      texto: '¿De qué es la plata? Casa, Mercado, Transporte, Sueldo...',
      accion: <Link to="/categorias">Crear categorías</Link>,
    },
    {
      hecho: medios.length > 0,
      titulo: 'Crea tus medios de pago',
      texto: '¿Dónde está la plata? Efectivo, Nequi, tu cuenta del banco...',
      accion: <Link to="/medios-pago">Crear medios</Link>,
    },
    {
      hecho: hayMovimientos,
      titulo: 'Anota tu primer movimiento',
      texto: 'Algo que recibiste o pagaste hoy. Con eso ya ves tu resumen.',
      accion: <button onClick={onRegistrar}>Nuevo movimiento</button>,
    },
  ]

  const hechos = pasos.filter((p) => p.hecho).length
  if (hechos === pasos.length) return null

  // El siguiente es el primero sin hacer: solo ese lleva botón, para que no
  // quede duda de por dónde seguir.
  const siguiente = pasos.findIndex((p) => !p.hecho)

  return (
    <section className="tarjeta primeros-pasos">
      <div className="primeros-pasos-encabezado">
        <div>
          <span className="kicker">
            {hechos} de {pasos.length} listos
          </span>
          <h2>Primeros pasos</h2>
        </div>
        <button className="enlace" onClick={abrirTutorial}>
          <Icono nombre="ayuda" tamano={16} /> Ver la guía
        </button>
      </div>

      <div className="primeros-pasos-barra" aria-hidden="true">
        <span style={{ width: `${(hechos / pasos.length) * 100}%` }} />
      </div>

      <ol className="primeros-pasos-lista">
        {pasos.map((p, i) => (
          <li
            key={p.titulo}
            className={p.hecho ? 'hecho' : i === siguiente ? 'siguiente' : ''}
          >
            <span className="primeros-pasos-numero">
              {p.hecho ? <Icono nombre="listo" tamano={16} /> : i + 1}
            </span>
            <div className="primeros-pasos-texto">
              <strong>{p.titulo}</strong>
              {!p.hecho && <span className="tenue">{p.texto}</span>}
            </div>
            {i === siguiente && <div className="primeros-pasos-accion">{p.accion}</div>}
          </li>
        ))}
      </ol>
    </section>
  )
}
