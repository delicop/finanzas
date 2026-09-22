import { ETIQUETAS_TIPO, estiloMonto } from '../lib/formato'
import Icono from './Icono'

// La etiqueta de tipo de un movimiento ("Pagué", "Recibí"…) con una flecha
// adelante que dice hacia dónde fue la plata.
//
// La flecha toma el color del monto (menta si entra, rojo si sale, índigo si
// es una deuda que sigue abierta, gris si es un traslado), así que el tipo y
// el monto de una fila siempre se leen igual. La etiqueta en sí no cambia de
// color: si toda la pastilla fuera verde o roja, la lista entera gritaría.
const FLECHA = {
  recibi: 'entra',
  me_prestaron: 'entra',
  pague: 'sale',
  preste: 'sale',
  traslado: 'traslado',
}

export default function EtiquetaTipo({ m, texto }) {
  const { clase } = estiloMonto(m)
  return (
    <span className={`etiqueta tipo-${m.tipo}`}>
      <Icono nombre={FLECHA[m.tipo] || 'sale'} tamano={13} className={`icono-tipo ${clase}`} />
      {texto || ETIQUETAS_TIPO[m.tipo]}
    </span>
  )
}
