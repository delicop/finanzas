import { faltaParaCobrar, formatearFecha } from '../lib/formato'

// Cuándo quedó de pagar alguien, con color según lo que falte: rojo si ya se
// pasó, ámbar si es hoy, verde si es en esta semana.
export default function FechaCobro({ fecha, conFecha = false }) {
  if (!fecha) return null
  const { texto, tono } = faltaParaCobrar(fecha)
  return (
    <span className={`fecha-cobro ${tono ? `cobro-${tono}` : ''}`} title={`Cobrar el ${formatearFecha(fecha)}`}>
      {texto}
      {conFecha && tono && <span className="tenue"> ({formatearFecha(fecha)})</span>}
    </span>
  )
}
