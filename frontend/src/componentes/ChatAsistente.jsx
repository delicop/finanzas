import { useEffect, useRef, useState } from 'react'
import { agenteApi, categoriasApi, mediosApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { formatearHora, formatearMonto } from '../lib/formato'
import PropuestaAgente from './PropuestaAgente'

// Ideas para arrancar cuando el hilo está vacío.
const SUGERENCIAS = [
  '¿Cuánto llevo gastado este mes?',
  '¿Quién me debe plata?',
  'Pagué 45 mil de almuerzo en efectivo',
]

// Cómo se le nombran al usuario las herramientas que consultó el agente. El
// backend manda el nombre técnico; aquí se traduce, y lo que no esté en la
// lista se muestra tal cual en vez de desaparecer.
// Las de "proponer_" no se listan: no son una consulta, y lo que prepararon ya
// está a la vista en su tarjeta.
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

// El chat con el asistente, con la forma de un chat de WhatsApp: cabecera con
// el contacto, burbujas a cada lado y el cajón de escribir abajo.
//
// Se abre desde la burbuja flotante (BurbujaAsistente). La conversación la
// guarda el servidor, así que sigue ahí al cerrar y volver a abrir.
//
//   onCerrar   si llega, la cabecera muestra una flecha para cerrar el panel.
//   onGuardado se llama cuando una tarjeta del asistente se confirma, para que
//              las pantallas de atrás recarguen sus cifras.
export default function ChatAsistente({ onCerrar, onGuardado }) {
  const { soloLectura } = useAuth()

  const [mensajes, setMensajes] = useState([])
  const [texto, setTexto] = useState('')
  const [cargando, setCargando] = useState(true)
  const [enviando, setEnviando] = useState(false)
  const [error, setError] = useState('')
  const [restantes, setRestantes] = useState(null)

  // Lo que el asistente dejó preparado y todavía no se guarda. Vienen del
  // servidor (no solo del último mensaje) para que una tarjeta sin confirmar
  // sobreviva a cerrar el chat: si desapareciera, el usuario creería que se
  // guardó algo.
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

    // Si el usuario cierra antes de que llegue la respuesta, se cancela: así
    // no se intenta pintar sobre un componente desmontado.
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
  // arrastra también la página de fondo.
  useEffect(() => {
    const caja = hilo.current
    if (caja) caja.scrollTop = caja.scrollHeight
  }, [mensajes, enviando, propuestas, guardado])

  // Al abrir, el cursor ya está listo para escribir, como en WhatsApp.
  useEffect(() => {
    if (!cargando) campo.current?.focus()
  }, [cargando])

  async function enviar(e) {
    e?.preventDefault()

    const pregunta = texto.trim()
    if (!pregunta || enviando) return

    // Se pinta de una vez, sin esperar al servidor: el modelo puede tardar
    // unos segundos y ver tu propia frase en pantalla es lo que hace que la
    // espera se sienta normal.
    const provisional = {
      id: `local-${Date.now()}`,
      rol: 'usuario',
      contenido: pregunta,
      creado_en: new Date().toISOString(),
    }
    setMensajes((anteriores) => [...anteriores, provisional])
    setTexto('')
    // El cajón había crecido con el texto: vuelve a una línea.
    if (campo.current) campo.current.style.height = 'auto'
    setError('')
    setGuardado('')
    setEnviando(true)

    try {
      const datos = await agenteApi.enviar(pregunta)
      setMensajes((anteriores) => [...anteriores, datos.mensaje])
      setRestantes(datos.restantes)
      if (datos.propuestas?.length) {
        setPropuestas((anteriores) => [...anteriores, ...datos.propuestas])
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

  // El cajón crece con el texto hasta un tope, como en WhatsApp.
  function alEscribir(e) {
    setTexto(e.target.value)
    const caja = e.target
    caja.style.height = 'auto'
    caja.style.height = `${Math.min(caja.scrollHeight, 120)}px`
  }

  async function empezarDeCero() {
    if (!confirm('¿Borrar la conversación? No se puede deshacer.')) return
    try {
      await agenteApi.borrar()
      setMensajes([])
      setGuardado('')
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
    if (movimiento) {
      const donde = movimiento.categoria_nombre ? ` en ${movimiento.categoria_nombre}` : ''
      setGuardado(`Guardado: ${formatearMonto(movimiento.monto)}${donde}`)
      onGuardado?.(movimiento)
    } else {
      setGuardado('')
    }
  }

  const estado = enviando ? 'escribiendo...' : 'en línea'

  return (
    <section className="wa">
      <header className="wa-cabecera">
        {onCerrar && (
          <button type="button" className="wa-icono" onClick={onCerrar} aria-label="Cerrar el chat">
            ←
          </button>
        )}
        <div className="wa-avatar" aria-hidden="true">
          ✦
        </div>
        <div className="wa-contacto">
          <strong>Asistente</strong>
          <span className={enviando ? 'wa-escribiendo' : ''}>{sinConfigurar ? 'sin conectar' : estado}</span>
        </div>
        {mensajes.length > 0 && !soloLectura && (
          <button type="button" className="wa-icono wa-borrar" onClick={empezarDeCero} title="Empezar de cero">
            Vaciar
          </button>
        )}
      </header>

      {/* role="log" + aria-live: un lector de pantalla anuncia la respuesta
          cuando llega, sin que el usuario tenga que ir a buscarla. */}
      <div className="wa-hilo" ref={hilo} role="log" aria-live="polite" aria-busy={enviando}>
        {soloLectura ? (
          <p className="wa-sistema">
            El chat con el asistente es privado: no se puede ver desde otra cuenta, ni siquiera
            revisando la de un cliente.
          </p>
        ) : sinConfigurar ? (
          <p className="wa-sistema">
            El asistente no está configurado en este servidor. Hay que poner la llave del modelo (
            <code>LLM_API_KEY</code>) en el <code>.env</code> y reiniciar el backend.
          </p>
        ) : cargando ? (
          <p className="wa-sistema">Cargando...</p>
        ) : (
          <>
            {mensajes.length === 0 && (
              <div className="wa-bienvenida">
                <p className="wa-sistema">
                  Escríbeme como a un amigo: pregúntame por tus gastos, tus saldos o quién te debe, o
                  dime qué pagaste y te lo dejo listo para guardar.
                </p>
                <div className="chat-sugerencias">
                  {SUGERENCIAS.map((s) => (
                    <button key={s} type="button" className="chip" onClick={() => setTexto(s)}>
                      {s}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {mensajes.map((m) => (
              <Burbuja key={m.id} mensaje={m} />
            ))}

            {enviando && (
              <div className="wa-burbuja wa-agente wa-puntos" aria-label="El asistente está escribiendo">
                <span />
                <span />
                <span />
              </div>
            )}

            {propuestas.map((p) => (
              <PropuestaAgente
                key={p.id}
                propuesta={p}
                categorias={listas.categorias}
                medios={listas.medios}
                onResuelta={propuestaResuelta}
              />
            ))}

            {guardado && <p className="wa-sistema wa-ok">✓ {guardado}</p>}
            {error && <p className="wa-sistema wa-error">{error}</p>}
          </>
        )}
      </div>

      {restantes !== null && restantes <= AVISAR_RESTANTES && (
        <p className="wa-restantes">
          {restantes === 0
            ? 'Se acabaron tus mensajes por hoy. Mañana puedes seguir.'
            : `Te quedan ${restantes} mensaje${restantes === 1 ? '' : 's'} por hoy.`}
        </p>
      )}

      {!soloLectura && !sinConfigurar && (
        <form className="wa-escribir" onSubmit={enviar}>
          <label htmlFor="mensaje-asistente" className="sr-solo">
            Escribe un mensaje
          </label>
          <textarea
            id="mensaje-asistente"
            ref={campo}
            value={texto}
            onChange={alEscribir}
            onKeyDown={alTeclear}
            placeholder="Escribe un mensaje"
            maxLength={MAX_CARACTERES}
            rows={1}
            disabled={cargando || enviando || restantes === 0}
          />
          <button
            type="submit"
            className="wa-enviar"
            disabled={cargando || enviando || texto.trim() === ''}
            aria-label="Enviar"
          >
            ➤
          </button>
        </form>
      )}
    </section>
  )
}

function Burbuja({ mensaje }) {
  const esMio = mensaje.rol === 'usuario'
  const consultas = (mensaje.herramientas ?? []).filter((h) => !h.startsWith('proponer_'))

  return (
    <div className={`wa-burbuja ${esMio ? 'wa-mio' : 'wa-agente'}`}>
      {/* El texto va tal cual, sin interpretarlo como HTML ni como markdown:
          React escapa el contenido y eso es exactamente lo que se quiere con
          algo que escribe un modelo. Los saltos de línea los respeta el CSS
          con white-space: pre-wrap. */}
      <p>{mensaje.contenido}</p>

      {/* De dónde salieron las cifras. No es un adorno: quien lee un número en
          una app de plata tiene derecho a saber qué miró el agente. */}
      {consultas.length > 0 && (
        <p className="wa-fuente">
          Consultó {consultas.map((h) => NOMBRES_HERRAMIENTAS[h] ?? h).join(' y ')}
        </p>
      )}

      {mensaje.creado_en && (
        <time className="wa-hora">
          {formatearHora(mensaje.creado_en)}
          {esMio && <span className="wa-check"> ✓✓</span>}
        </time>
      )}
    </div>
  )
}
