import Icono from './Icono'

// Lo que se ve cuando una lista todavía no tiene nada.
//
// Un dibujo, una frase que dice qué va aquí y el botón para empezar. Antes
// era una línea gris de texto, y quien entraba por primera vez tenía que
// buscar arriba cuál botón tocar. El botón va aquí mismo, donde está la vista.
export default function Vacio({ icono, titulo, children, accion, className = '' }) {
  return (
    <div className={`vacio ${className}`}>
      <span className="vacio-icono">
        <Icono nombre={icono} tamano={30} />
      </span>
      <h3 className="vacio-titulo">{titulo}</h3>
      {children && <p className="vacio-texto">{children}</p>}
      {accion && <div className="vacio-accion">{accion}</div>}
    </div>
  )
}
