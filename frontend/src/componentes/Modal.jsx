import { useEffect } from 'react'

// `ancho` es para lo que se lee como una lista (el detalle de una categoría):
// en 480px una fila con fecha, descripción y monto no cabe.
export default function Modal({ titulo, onCerrar, children, ancho = false }) {
  // Cerrar con Escape. El cleanup del useEffect quita el listener al
  // desmontar; sin eso se irian acumulando listeners cada vez que se abre.
  useEffect(() => {
    const alPresionar = (e) => {
      if (e.key === 'Escape') onCerrar()
    }
    window.addEventListener('keydown', alPresionar)
    return () => window.removeEventListener('keydown', alPresionar)
  }, [onCerrar])

  return (
    <div className="modal-fondo" onClick={onCerrar}>
      {/* stopPropagation: un clic dentro del modal no debe cerrarlo */}
      <div className={`modal ${ancho ? 'modal-ancho' : ''}`} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div className="modal-encabezado">
          <h2>{titulo}</h2>
          <button className="icono" onClick={onCerrar} aria-label="Cerrar">
            ×
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}
