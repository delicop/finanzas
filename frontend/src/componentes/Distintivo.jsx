import { distintivo } from '../lib/distintivo'

// El circulito de color que acompaña el nombre de una categoría o de un medio
// de pago. Con dibujo si el nombre se reconoce ("Mercado" → 🛒), con la
// inicial si no. Es decorativo: el nombre siempre va escrito al lado, así que
// el lector de pantalla no tiene que oírlo dos veces.
export default function Distintivo({ nombre, clase = 'categoria', grande = false }) {
  const { dibujo, inicial, tono } = distintivo(nombre, clase)
  return (
    <span
      className={`distintivo dist-${tono} ${grande ? 'distintivo-grande' : ''} ${dibujo ? 'con-dibujo' : ''}`}
      aria-hidden="true"
    >
      {dibujo || inicial}
    </span>
  )
}
