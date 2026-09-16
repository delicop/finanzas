import { useEffect, useState } from 'react'
import { descargarFactura } from '../lib/api'
import Modal from './Modal'

// Muestra la factura de un movimiento.
//
// La imagen o el PDF no se pueden cargar con <img src="/api/..."> porque ese
// request lo dispara el navegador y no lleva el header Authorization.
// Por eso se baja con fetch (que sí manda el token) y se convierte en una
// URL temporal de memoria (blob:).
export default function VisorFactura({ movimiento, onCerrar }) {
  const [recurso, setRecurso] = useState(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let urlCreada = null
    let cancelado = false

    descargarFactura(movimiento.id)
      .then((r) => {
        if (cancelado) {
          // El modal se cerró antes de que llegara: liberamos y no tocamos el estado.
          URL.revokeObjectURL(r.url)
          return
        }
        urlCreada = r.url
        setRecurso(r)
      })
      .catch((err) => !cancelado && setError(err.message))

    // Liberar el blob es obligatorio: si no, el archivo se queda en memoria
    // del navegador hasta que se recargue la página.
    return () => {
      cancelado = true
      if (urlCreada) URL.revokeObjectURL(urlCreada)
    }
  }, [movimiento.id])

  return (
    <Modal titulo={movimiento.factura?.nombre ?? 'Factura'} onCerrar={onCerrar}>
      {error && <div className="alerta">{error}</div>}

      {!recurso && !error && <p className="tenue">Cargando factura...</p>}

      {recurso &&
        (recurso.tipo === 'application/pdf' ? (
          <object data={recurso.url} type="application/pdf" className="visor-pdf">
            {/* Si el navegador no puede incrustar el PDF (pasa en varios
                móviles), al menos ofrecemos abrirlo en otra pestaña. */}
            <p className="tenue">
              Tu navegador no puede mostrar el PDF aquí.{' '}
              <a href={recurso.url} target="_blank" rel="noreferrer">
                Ábrelo en una pestaña nueva
              </a>
              .
            </p>
          </object>
        ) : (
          <img src={recurso.url} alt={movimiento.factura?.nombre ?? 'Factura'} className="visor-imagen" />
        ))}

      {recurso && (
        <div className="acciones-modal">
          <a className="boton-enlace" href={recurso.url} download={movimiento.factura?.nombre}>
            Descargar
          </a>
        </div>
      )}
    </Modal>
  )
}
