import { useEffect, useRef, useState } from 'react'
import { avisarMovimientoGuardado } from '../lib/eventos'
import ChatAsistente from './ChatAsistente'

// La burbuja verde de la esquina, como el botón de WhatsApp en una página web.
//
// Es la ÚNICA entrada al asistente: antes había una pestaña en el menú y un
// botón en el Resumen que abrían lo mismo. Flotante, además, se puede usar
// desde cualquier pantalla sin salir de ella.
export default function BurbujaAsistente() {
  const [abierto, setAbierto] = useState(false)

  return (
    <>
      {!abierto && (
        <button
          type="button"
          className="burbuja-asistente"
          onClick={() => setAbierto(true)}
          aria-label="Hablar con el asistente"
          title="Hablar con el asistente"
        >
          <span aria-hidden="true">💬</span>
        </button>
      )}

      {abierto && <PanelChat onCerrar={() => setAbierto(false)} />}
    </>
  )
}

// El chat en un panel encima de la pantalla: completo en el teléfono (como
// abrir una conversación de WhatsApp) y como columna a la derecha en
// escritorio. Escape y el clic afuera lo cierran.
function PanelChat({ onCerrar }) {
  // En una ref para que el efecto corra UNA sola vez: onCerrar llega como
  // función nueva en cada render, y si fuera dependencia el efecto guardaría
  // "hidden" como overflow anterior y la página quedaría sin scroll al cerrar.
  const cerrar = useRef(onCerrar)
  cerrar.current = onCerrar

  useEffect(() => {
    const alPresionar = (e) => {
      if (e.key === 'Escape') cerrar.current()
    }
    window.addEventListener('keydown', alPresionar)
    // Sin esto, en el teléfono el scroll del dedo mueve la pantalla de atrás.
    const antes = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', alPresionar)
      document.body.style.overflow = antes
    }
  }, [])

  return (
    <div className="wa-fondo" onClick={onCerrar}>
      <div
        className="wa-panel"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Chat con el asistente"
      >
        {/* Cada tarjeta confirmada avisa: la pantalla de atrás se recarga y
            al cerrar el chat las cifras ya están al día. */}
        <ChatAsistente onCerrar={onCerrar} onGuardado={avisarMovimientoGuardado} />
      </div>
    </div>
  )
}
