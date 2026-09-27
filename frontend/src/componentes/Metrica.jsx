import { formatearMonto } from '../lib/formato'

// Una cifra del encabezado: la del Resumen y la de las Tiendas, para que las
// dos pantallas se lean igual.
//
// `signo` va aparte del monto porque es lo que carga el significado: en este
// sistema el color ya no distingue lo que entra de lo que sale, así que el +
// y el − tienen que estar siempre, no solo cuando se ven bien.
//
// `barra` es una proporción de 0 a 1 para el filete de abajo, y es opcional:
// sin ella la ficha no pinta filete. No es una cifra que alguien lea, es el
// ancho de una línea, así que se puede calcular en el navegador.
export default function Metrica({ titulo, monto, nota, signo, barra, clase = '', principal = false }) {
  return (
    <div className={`tarjeta metrica ${principal ? 'principal' : ''}`}>
      <span>{titulo}</span>
      <strong className={clase}>
        {signo ? `${signo} ` : ''}
        {formatearMonto(monto)}
      </strong>
      {barra !== undefined && (
        <div className="barra-oro">
          <i style={{ width: `${Math.round(Math.min(1, Math.max(0, barra)) * 100)}%` }} />
        </div>
      )}
      {nota && <span className="metrica-nota">{nota}</span>}
    </div>
  )
}
