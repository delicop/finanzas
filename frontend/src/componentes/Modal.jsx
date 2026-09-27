import { useEffect, useRef } from 'react'

// Referencia estable para el caso sin `alIntentarCerrar`: si el valor por
// defecto fuera una arrow function inline, cada render crearía una nueva y el
// useEffect de Escape se desuscribiría y volvería a suscribir sin necesidad.
const siempreCerrar = () => true

// `ancho` es para lo que se lee como una lista (el detalle de una categoría):
// en 480px una fila con fecha, descripción y monto no cabe.
//
// `alIntentarCerrar` deja que quien usa el modal frene el cierre: la hoja del
// cierre de caja la usa para preguntar antes de perder un formulario largo a
// medio llenar. Sin la prop, el modal cierra derecho como siempre.
export default function Modal({
  titulo,
  onCerrar,
  children,
  ancho = false,
  alIntentarCerrar = siempreCerrar,
}) {
  // Cerrar con Escape. El cleanup del useEffect quita el listener al
  // desmontar; sin eso se irian acumulando listeners cada vez que se abre.
  useEffect(() => {
    const alPresionar = (e) => {
      if (e.key === 'Escape' && alIntentarCerrar()) onCerrar()
    }
    window.addEventListener('keydown', alPresionar)
    return () => window.removeEventListener('keydown', alPresionar)
  }, [onCerrar, alIntentarCerrar])

  // Dónde EMPEZÓ el clic. El fondo cierra el modal, pero solo si el clic
  // nació ahí: al seleccionar un texto de un campo y soltar el botón por
  // fuera, o al volver de un desplegable, el navegador manda un clic cuyo
  // final cae en el fondo — y el formulario a medio llenar se perdía.
  const nacioEnElFondo = useRef(false)

  return (
    <div
      className="modal-fondo"
      onMouseDown={(e) => {
        nacioEnElFondo.current = e.target === e.currentTarget
      }}
      onClick={(e) => {
        // Las dos condiciones: el clic termina en el fondo (y no en algo de
        // adentro que ya se quitó de la pantalla) Y empezó ahí mismo.
        if (e.target === e.currentTarget && nacioEnElFondo.current && alIntentarCerrar()) onCerrar()
        nacioEnElFondo.current = false
      }}
    >
      <div className={`modal ${ancho ? 'modal-ancho' : ''}`} role="dialog" aria-modal="true">
        <div className="modal-encabezado">
          <h2>{titulo}</h2>
          <button className="icono" onClick={() => alIntentarCerrar() && onCerrar()} aria-label="Cerrar">
            ×
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}
