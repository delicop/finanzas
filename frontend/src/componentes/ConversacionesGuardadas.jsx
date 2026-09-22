import { useEffect, useState } from 'react'
import { agenteApi } from '../lib/api'
import { descargarConversacion } from '../lib/descargarChat'
import { formatearFechaHora } from '../lib/formato'
import { hayVoz, useVoz } from '../lib/voz'
import Burbuja from './BurbujaMensaje'

// Las conversaciones que el usuario terminó. Se leen, se descargan y se
// borran, pero no se continúan: seguir escribiendo en una vieja haría que el
// asistente vuelva a leerla entera, que es justo lo que terminar evita.
//
// Ocupa el mismo panel del chat, con su propia cabecera y una flecha para
// volver.
export default function ConversacionesGuardadas({ onVolver }) {
  const [lista, setLista] = useState(null)
  const [abierta, setAbierta] = useState(null)
  const [error, setError] = useState('')
  // Una conversación guardada también se puede escuchar: es el mismo botón
  // debajo de cada respuesta. Lo que no tiene es el modo automático — aquí no
  // llega nada nuevo que leer.
  const voz = useVoz()

  useEffect(() => {
    const control = new AbortController()
    agenteApi
      .guardadas(control.signal)
      .then(setLista)
      .catch((err) => {
        if (err.name !== 'AbortError') setError(err.message)
      })
    return () => control.abort()
  }, [])

  async function abrir(id) {
    setError('')
    try {
      setAbierta(await agenteApi.guardada(id))
    } catch (err) {
      setError(err.message)
    }
  }

  async function borrar(g) {
    if (!confirm('¿Borrar esta conversación? No se puede deshacer.')) return
    try {
      await agenteApi.borrarGuardada(g.id)
      setLista((anteriores) => anteriores.filter((x) => x.id !== g.id))
      setAbierta(null)
    } catch (err) {
      setError(err.message)
    }
  }

  const titulo = abierta ? abierta.titulo || 'Conversación' : 'Conversaciones guardadas'

  return (
    <section className="wa">
      <header className="wa-cabecera">
        <button
          type="button"
          className="wa-icono"
          onClick={() => {
            voz.callar()
            if (abierta) setAbierta(null)
            else onVolver()
          }}
          aria-label={abierta ? 'Volver a la lista' : 'Volver al chat'}
        >
          ←
        </button>
        <div className="wa-contacto">
          <strong className="wa-titulo">{titulo}</strong>
          <span>
            {abierta
              ? formatearFechaHora(abierta.archivada_en)
              : lista
                ? `${lista.length} guardada${lista.length === 1 ? '' : 's'}`
                : ''}
          </span>
        </div>
        {abierta && (
          <>
            <button
              type="button"
              className="wa-icono"
              onClick={() => descargarConversacion(abierta.hilo, abierta.titulo)}
              aria-label="Descargar"
              title="Descargar"
            >
              ⤓
            </button>
            <button
              type="button"
              className="wa-icono"
              onClick={() => borrar(abierta)}
              aria-label="Borrar"
              title="Borrar"
            >
              🗑
            </button>
          </>
        )}
      </header>

      <div className="wa-hilo">
        {error && <p className="wa-sistema wa-error">{error}</p>}

        {abierta ? (
          <>
            <p className="wa-sistema">Conversación terminada. Solo se puede leer.</p>
            {abierta.hilo.map((m) => (
              <Burbuja
                key={m.id}
                mensaje={m}
                onLeer={hayVoz ? () => voz.alternarLectura(m.id, m.contenido) : undefined}
                leyendo={voz.leyendo === m.id}
              />
            ))}
          </>
        ) : lista === null ? (
          !error && <p className="wa-sistema">Cargando...</p>
        ) : lista.length === 0 ? (
          <p className="wa-sistema">
            Todavía no hay conversaciones guardadas. Cuando termines una, aparece aquí.
          </p>
        ) : (
          <ul className="wa-lista">
            {lista.map((g) => (
              <li key={g.id}>
                <button type="button" onClick={() => abrir(g.id)}>
                  <strong>{g.titulo || 'Conversación'}</strong>
                  <span>
                    {formatearFechaHora(g.archivada_en)} · {g.mensajes} mensaje
                    {g.mensajes === 1 ? '' : 's'}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}
