import { useEffect, useRef, useState } from 'react'
import { reducirImagen } from '../lib/imagen'

// Tipos que acepta el backend para una factura (ver movimientos/facturas.go).
export const TIPOS_FACTURA = 'image/jpeg,image/png,image/webp,image/heic,application/pdf'

// El botón del clip: abre la galería o la cámara del teléfono, achica la foto
// y la entrega. `children` es lo que se ve en el botón.
export function BotonAdjuntar({ onArchivo, className = '', titulo = 'Adjuntar factura', disabled, children }) {
  const entrada = useRef(null)
  const [preparando, setPreparando] = useState(false)

  async function alElegir(e) {
    const archivo = e.target.files?.[0]
    // Se limpia para que elegir el MISMO archivo otra vez vuelva a disparar
    // onChange (si no, el navegador lo ignora).
    e.target.value = ''
    if (!archivo) return
    setPreparando(true)
    try {
      onArchivo(await reducirImagen(archivo))
    } finally {
      setPreparando(false)
    }
  }

  return (
    <>
      <button
        type="button"
        className={className}
        onClick={() => entrada.current?.click()}
        disabled={disabled || preparando}
        aria-label={titulo}
        title={titulo}
      >
        {preparando ? '…' : children}
      </button>
      <input ref={entrada} type="file" accept={TIPOS_FACTURA} onChange={alElegir} hidden />
    </>
  )
}

// La vista previa de un archivo elegido: miniatura si es imagen, nombre si es
// PDF, y una X para quitarlo.
export function VistaAdjunto({ archivo, onQuitar, texto }) {
  const [url, setUrl] = useState(null)

  useEffect(() => {
    if (!archivo?.type?.startsWith('image/')) {
      setUrl(null)
      return
    }
    const temporal = URL.createObjectURL(archivo)
    setUrl(temporal)
    // La URL temporal ocupa memoria hasta que se suelta.
    return () => URL.revokeObjectURL(temporal)
  }, [archivo])

  if (!archivo) return null

  return (
    <div className="vista-adjunto">
      {url ? (
        <img src={url} alt="" className="vista-adjunto-img" />
      ) : (
        <span className="vista-adjunto-icono" aria-hidden="true">
          📄
        </span>
      )}
      <span className="vista-adjunto-texto">
        {texto && <strong>{texto}</strong>}
        <span className="tenue">{archivo.name}</span>
      </span>
      {onQuitar && (
        <button type="button" className="vista-adjunto-quitar" onClick={onQuitar} aria-label="Quitar la factura">
          ✕
        </button>
      )}
    </div>
  )
}
