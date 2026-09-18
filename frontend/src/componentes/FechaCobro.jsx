import { faltaParaCobrar, formatearFecha } from '../lib/formato'

// Cuándo quedó de pagarse una deuda, con color según lo que falte: rojo si ya
// se pasó, ámbar si es hoy, verde si es en esta semana.
//
// `propia` invierte la frase ("le pagas el viernes" en vez de "te paga el
// viernes"). Es la misma fecha y el mismo color, pero la acción es la
// contraria, y decirla al revés no confunde un poco: confunde del todo.
export default function FechaCobro({ fecha, conFecha = false, propia = false }) {
  if (!fecha) return null
  const { texto, tono } = faltaParaCobrar(fecha, undefined, propia)
  const titulo = propia ? `Pagar el ${formatearFecha(fecha)}` : `Cobrar el ${formatearFecha(fecha)}`
  return (
    <span className={`fecha-cobro ${tono ? `cobro-${tono}` : ''}`} title={titulo}>
      {texto}
      {conFecha && tono && <span className="tenue"> ({formatearFecha(fecha)})</span>}
    </span>
  )
}
