import { useEffect, useRef, useState } from 'react'
import { agenteApi, categoriasApi, mediosApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { formatearHora, formatearMonto } from '../lib/formato'
import PropuestaAgente from '../componentes/PropuestaAgente'

// Ideas para arrancar cuando el hilo está vacío.
const SUGERENCIAS = [
  '¿Cuánto llevo gastado este mes?',
  '¿Quién me debe plata?',
  '¿Dónde tengo la plata?',
]

// Cómo se le nombran al usuario las herramientas que consultó el agente. El
// backend manda el nombre técnico; aquí se traduce, y lo que no esté en la
// lista se muestra tal cual en vez de desaparecer.
const NOMBRES_HERRAMIENTAS = {
  resumen: 'tu resumen',
  listar_movimientos: 'tus movimientos',
  listar_categorias: 'tus categorías',
  listar_medios_pago: 'tus medios de pago',
}

// El mismo techo que valida el backend. Repetirlo aquí no es duplicar la
// regla: es avisarle al usuario antes de que escriba 2001 caracteres y reciba
// un error. La que manda sigue siendo la del servidor.
const MAX_CARACTERES = 2000

// Debajo de esto se le avisa al usuario cuántos mensajes le quedan.
const AVISAR_RESTANTES = 10

export default function Agente() {
  const { soloLectura } = useAuth()

  const [mensajes, setMensajes] = useState([])
  const [texto, setTexto] = useState('')
  const [cargando, setCargando] = useState(true)
  const [enviando, setEnviando] = useState(false)
  const [error, setError] = useState('')
  const [restantes, setRestantes] = useState(null)

  // Lo que el asistente dejó preparado y todavía no se guarda. Vienen del
  // servidor (no solo del último mensaje) para que una tarjeta sin confirmar
  // sobreviva a recargar la página: si desapareciera, el usuario creería que
  // se guardó algo.
  const [propuestas, setPropuestas] = useState([])
  const [listas, setListas] = useState({ categorias: [], medios: [] })
  const [guardado, setGuardado] = useState('')
  // El chat es opcional en el servidor: si no hay llave del modelo, la ruta
  // ni siquiera existe y responde 404.
  const [sinConfigurar, setSinConfigurar] = useState(false)

  const hilo = useRef(null)
  const campo = useRef(null)

  useEffect(() => {
    // Mirando la cuenta de otro no hay nada que cargar: el backend responde
    // 403 porque el chat de alguien no es un dato que el panel revise.
    if (soloLectura) {
      setCargando(false)
      return
    }

    // Si el usuario cambia de pantalla antes de que llegue la respuesta,
    // se cancela: así no se intenta pintar sobre un componente desmontado.
    const control = new AbortController()

    agenteApi
      .conversacion(control.signal)
      .then((datos) => {
        setMensajes(datos.mensajes)
        setPropuestas(datos.propuestas ?? [])
        setRestantes(datos.restantes)

        // Las listas solo hacen falta para pintar los selectores de una
        // tarjeta, así que se piden después de saber que el chat existe.
        return Promise.all([categoriasApi.listar(), mediosApi.listar()])
      })
      .then((listas) => {
        if (!listas || control.signal.aborted) return
        const [categorias, medios] = listas
        setListas({ categorias, medios })
      })
      .catch((err) => {
        if (err.name === 'AbortError') return
        if (err.status === 404) setSinConfigurar(true)
        else setError(err.message)
      })
      .finally(() => {
        if (!control.signal.aborted) setCargando(false)
      })

    return () => control.abort()
  }, [soloLectura])

  // El hilo siempre mirando el último mensaje, como cualquier chat.
  // Se mueve el scroll del contenedor y no con scrollIntoView: eso último
  // arrastra también la página, y el encabezado se iría de la pantalla cada
  // vez que llega una respuesta.
  useEffect(() => {
    const caja = hilo.current
    if (caja) caja.scrollTop = caja.scrollHeight
  }, [mensajes, enviando])

  async function enviar(e) {
    e?.preventDefault()

    const pregunta = texto.trim()
    if (!pregunta || enviando) return

    // Se pinta de una vez, sin esperar al servidor: el modelo puede tardar
    // unos segundos y ver tu propia frase en pantalla es lo que hace que la
    // espera se sienta normal.
    const provisional = { id: `local-${Date.now()}`, rol: 'usuario', contenido: pregunta }
    setMensajes((anteriores) => [...anteriores, provisional])
    setTexto('')
    setError('')
    setEnviando(true)

    try {
      const datos = await agenteApi.enviar(pregunta)
      setMensajes((anteriores) => [...anteriores, datos.mensaje])
      setRestantes(datos.restantes)
      if (datos.propuestas?.length) {
        setPropuestas((anteriores) => [...anteriores, ...datos.propuestas])
        setGuardado('')
      }
    } catch (err) {
      // La pregunta se queda en pantalla: el backend ya la guardó, y borrarla
      // obligaría a escribirla otra vez para reintentar.
      setError(err.message)
    } finally {
      setEnviando(false)
      campo.current?.focus()
    }
  }

  function alTeclear(e) {
    // Enter envía, Shift+Enter hace salto de línea. Es lo que espera
    // cualquiera que haya usado un chat.
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      enviar()
    }
  }

  async function empezarDeCero() {
    if (!confirm('¿Borrar la conversación? No se puede deshacer.')) return
    try {
      await agenteApi.borrar()
      setMensajes([])
      setError('')
    } catch (err) {
      setError(err.message)
    }
  }

  // Una tarjeta se fue: o se guardó (llega el movimiento creado) o se
  // descartó. El aviso es corto pero explícito — es el único momento en que
  // algo del asistente sí tocó las cuentas.
  function propuestaResuelta(id, movimiento) {
    setPropuestas((anteriores) => anteriores.filter((p) => p.id !== id))
    setGuardado(movimiento ? `Guardado: ${formatearMonto(movimiento.monto)} en ${movimiento.categoria_nombre}` : '')
  }

  if (soloLectura) {
    return (
      <>
        <div className="encabezado-pagina">
          <h1>Asistente</h1>
        </div>
        <section className="tarjeta">
          <p className="tenue">
            El chat con el asistente es privado: no se puede ver desde otra cuenta, ni siquiera
            revisando la de un cliente.
          </p>
        </section>
      </>
    )
  }

  if (sinConfigurar) {
    return (
      <>
        <div className="encabezado-pagina">
          <h1>Asistente</h1>
        </div>
        <section className="tarjeta">
          <p className="tenue">
            El asistente no está configurado en este servidor. Para activarlo hay que poner la
            llave del modelo (<code>LLM_API_KEY</code>) en el archivo <code>.env</code> y reiniciar
            el backend.
          </p>
        </section>
      </>
    )
  }

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Asistente</h1>
        {mensajes.length > 0 && (
          <button className="secundario" onClick={empezarDeCero}>
            Empezar de cero
          </button>
        )}
      </div>

      {error && <div className="alerta">{error}</div>}

      <section className="tarjeta chat">
        {/* role="log" + aria-live: un lector de pantalla anuncia la respuesta
            cuando llega, sin que el usuario tenga que ir a buscarla. */}
        <div className="chat-hilo" ref={hilo} role="log" aria-live="polite" aria-busy={enviando}>
          {cargando ? (
            <p className="tenue">Cargando...</p>
          ) : mensajes.length === 0 ? (
            <div className="chat-vacio">
              <p>
                Pregúntame por tus movimientos, tus saldos o cómo funciona la app. Todavía no puedo
                registrar nada: para eso está el botón de Movimientos.
              </p>
              <div className="chat-sugerencias">
                {SUGERENCIAS.map((s) => (
                  <button key={s} type="button" className="chip" onClick={() => setTexto(s)}>
                    {s}
                  </button>
                ))}
              </div>
            </div>
          ) : (
            mensajes.map((m) => <Burbuja key={m.id} mensaje={m} />)
          )}

          {enviando && <div className="burbuja burbuja-agente tenue">Escribiendo...</div>}

          {propuestas.map((p) => (
            <PropuestaAgente
              key={p.id}
              propuesta={p}
              categorias={listas.categorias}
              medios={listas.medios}
              onResuelta={propuestaResuelta}
            />
          ))}

          {guardado && <p className="chat-guardado">✓ {guardado}</p>}
        </div>

        <form className="chat-escribir" onSubmit={enviar}>
          <label htmlFor="mensaje" className="sr-solo">
            Escribe tu pregunta
          </label>
          <textarea
            id="mensaje"
            ref={campo}
            value={texto}
            onChange={(e) => setTexto(e.target.value)}
            onKeyDown={alTeclear}
            placeholder="Escribe tu pregunta..."
            maxLength={MAX_CARACTERES}
            rows={1}
            disabled={cargando || enviando}
          />
          <button type="submit" disabled={cargando || enviando || texto.trim() === ''}>
            Enviar
          </button>
        </form>

        {restantes !== null && restantes <= AVISAR_RESTANTES && (
          <p className="tenue chat-restantes">
            {restantes === 0
              ? 'Se acabaron tus mensajes por hoy. Mañana puedes seguir.'
              : `Te quedan ${restantes} mensaje${restantes === 1 ? '' : 's'} por hoy.`}
          </p>
        )}
      </section>
    </>
  )
}

function Burbuja({ mensaje }) {
  const esMio = mensaje.rol === 'usuario'

  return (
    <div className={`burbuja ${esMio ? 'burbuja-usuario' : 'burbuja-agente'}`}>
      {/* El texto va tal cual, sin interpretarlo como HTML ni como markdown:
          React escapa el contenido y eso es exactamente lo que se quiere con
          algo que escribe un modelo. Los saltos de línea los respeta el CSS
          con white-space: pre-wrap. */}
      <p>{mensaje.contenido}</p>

      {/* De dónde salieron las cifras. No es un adorno: quien lee un número en
          una app de plata tiene derecho a saber qué miró el agente. */}
      {mensaje.herramientas?.length > 0 && (
        <p className="burbuja-fuente">
          Consultó {mensaje.herramientas.map((h) => NOMBRES_HERRAMIENTAS[h] ?? h).join(' y ')}
        </p>
      )}

      {mensaje.creado_en && <time className="burbuja-hora">{formatearHora(mensaje.creado_en)}</time>}
    </div>
  )
}
