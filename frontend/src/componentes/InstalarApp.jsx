import { useState } from 'react'
import { useInstalarApp } from '../lib/pwa'
import Modal from './Modal'

// El botón "Instalar" de la barra. Solo aparece cuando tiene sentido:
//
// - Chrome/Edge (Android, Windows): cuando el navegador avisa que se puede.
//   Abre su diálogo de instalar.
// - iPhone: Safari no tiene ese aviso. El botón explica los dos toques que hay
//   que dar (Compartir › Agregar a inicio).
// - Ya instalada, o un navegador que no instala: no aparece.
export default function InstalarApp() {
  const { disponible, instalar, esIOS } = useInstalarApp()
  const [explicando, setExplicando] = useState(false)

  if (!disponible && !esIOS) return null

  return (
    <>
      <button
        className="boton-tema"
        onClick={disponible ? instalar : () => setExplicando(true)}
        title="Instalar la app en este dispositivo"
        aria-label="Instalar la app en este dispositivo"
      >
        📲
      </button>

      {explicando && (
        <Modal titulo="Instalar en el iPhone" onCerrar={() => setExplicando(false)}>
          <ol className="pasos-instalar">
            <li>
              Toca el botón <strong>Compartir</strong> de Safari (el cuadrado con la flecha hacia
              arriba).
            </li>
            <li>
              Elige <strong>Agregar a inicio</strong>.
            </li>
            <li>La app queda con su ícono en la pantalla, como cualquier otra.</li>
          </ol>
          <div className="acciones-modal">
            <button type="button" onClick={() => setExplicando(false)}>
              Entendido
            </button>
          </div>
        </Modal>
      )}
    </>
  )
}

// Una franja arriba cuando no hay señal. La app instalada abre sin conexión
// (el service worker guarda la pantalla), pero los datos vienen del servidor:
// mejor decirlo que dejar que cada pantalla muestre su propio error.
export function AvisoSinConexion({ enLinea }) {
  if (enLinea) return null
  return (
    <div className="alerta aviso sin-conexion" role="status">
      Sin conexión. Las cifras no se pueden actualizar hasta que vuelva la señal.
    </div>
  )
}
