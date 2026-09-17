import { useEffect, useState } from 'react'
import { notificacionesApi } from '../lib/api'
import { formatearFechaHora } from '../lib/formato'
import Modal from './Modal'

// El panel de avisos. Se abre desde la campana de la barra.
//
// Al abrirlo se marcan como leídos: los vio, no hay más que saber. El orden
// importa — primero se piden y se pintan, y solo después se apagan: si el
// listado falla, no pueden quedar marcados como leídos unos avisos que nadie
// llegó a ver.
export default function Notificaciones({ onCerrar, onLeidos }) {
  const [avisos, setAvisos] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const control = new AbortController()

    notificacionesApi
      .listar(control.signal)
      .then(async (datos) => {
        if (control.signal.aborted) return
        setAvisos(datos.avisos)

        if (datos.sin_leer > 0) {
          await notificacionesApi.marcarLeidas()
          onLeidos()
        }
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        setError(err.message)
      })
      .finally(() => {
        if (!control.signal.aborted) setCargando(false)
      })

    return () => control.abort()
    // Solo al abrir: el panel no se recarga solo mientras está en pantalla.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <Modal titulo="Avisos" onCerrar={onCerrar}>
      {error && <div className="alerta">{error}</div>}

      {cargando ? (
        <p className="tenue">Cargando...</p>
      ) : avisos.length === 0 ? (
        <p className="tenue">
          Todavía no hay avisos. Aquí van a llegar tu resumen de cada semana, el aviso del día en
          que alguien quedó de pagarte y los préstamos que lleves tiempo sin cobrar.
        </p>
      ) : (
        <ul className="lista-avisos">
          {avisos.map((a) => (
            <li key={a.id} className={a.leida_en ? 'aviso-leido' : ''}>
              <div className="aviso-arriba">
                <strong>{a.titulo}</strong>
                <span className="tenue">{formatearFechaHora(a.creada_en)}</span>
              </div>
              <p>{a.cuerpo}</p>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  )
}
