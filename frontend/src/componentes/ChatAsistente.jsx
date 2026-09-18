import { useEffect, useRef, useState } from 'react'
import { agenteApi, categoriasApi, mediosApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { descargarConversacion } from '../lib/descargarChat'
import { hayDictado, useDictado } from '../lib/dictado'
import { formatearMonto } from '../lib/formato'
import { BotonAdjuntar, VistaAdjunto } from './AdjuntoFactura'
import Burbuja from './BurbujaMensaje'
import ConversacionesGuardadas from './ConversacionesGuardadas'
import PropuestaAgente from './PropuestaAgente'

// Ideas para arrancar cuando el hilo está vacío.
const SUGERENCIAS = [
  '¿Cuánto llevo gastado este mes?',
  '¿Quién me debe plata?',
  'Pagué 45 mil de almuerzo en efectivo',
]

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

  // 'chat' o 'guardadas': las conversaciones terminadas se ven en el mismo
  // panel, en vez de abrir otra ventana encima.
  const [vista, setVista] = useState('chat')
  const [menuAbierto, setMenuAbierto] = useState(false)
  // Se muestra una vez al terminar, para que quede claro que no se perdió.
  const [recienTerminada, setRecienTerminada] = useState(false)

  // La foto de la factura que el usuario adjuntó con el clip. Queda esperando
  // hasta que el asistente prepare el gasto, y ahí pasa a esa tarjeta, que la
  // sube como factura al guardar. El modelo no ve la foto: el monto y el
  // comercio los sigue diciendo la persona.
  const [fotoPendiente, setFotoPendiente] = useState(null)
  // Qué foto va con qué tarjeta: { [id de la propuesta]: File }.
  const [facturas, setFacturas] = useState({})

  const hilo = useRef(null)
  const campo = useRef(null)

  // Notas de voz: lo dictado cae en el cajón para revisarlo antes de enviar.
  // No se manda solo: un "18 mil" mal oído como "80 mil" es plata.
  const dictado = useDictado((dicho) => {
    setTexto(dicho.slice(0, MAX_CARACTERES))
    ajustarAltura()
  })

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
    if (!cargando && vista === 'chat') campo.current?.focus()
  }, [cargando, vista])

  async function enviar(e) {
    e?.preventDefault()

    const escrito = texto.trim()
    if (!escrito || enviando) return
    dictado.parar()

    // Con una foto esperando, el mensaje lo dice: el modelo no ve la foto, y
    // sin esto pediría una factura que el usuario ya adjuntó.
    const pregunta = fotoPendiente ? `${escrito}
📎 (con la foto de la factura)` : escrito

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
    setRecienTerminada(false)
    setEnviando(true)

    try {
      const datos = await agenteApi.enviar(pregunta)
      setMensajes((anteriores) => [...anteriores, datos.mensaje])
      setRestantes(datos.restantes)
      if (datos.propuestas?.length) {
        setPropuestas((anteriores) => [...anteriores, ...datos.propuestas])

        // La foto pendiente se va con el primer gasto que prepare.
        const destino = datos.propuestas.find((p) => p.tipo === 'movimiento')
        if (fotoPendiente && destino) {
          setFacturas((anteriores) => ({ ...anteriores, [destino.id]: fotoPendiente }))
          setFotoPendiente(null)
        }
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
    ajustarAltura()
  }

  function ajustarAltura() {
    // Después del render, cuando el texto nuevo ya está en el cajón.
    requestAnimationFrame(() => {
      const caja = campo.current
      if (!caja) return
      caja.style.height = 'auto'
      caja.style.height = `${Math.min(caja.scrollHeight, 120)}px`
    })
  }

  function alternarDictado() {
    if (dictado.escuchando) dictado.parar()
    else dictado.empezar(texto)
  }

  // Terminar deja el chat limpio: el asistente ya no relee lo anterior (más
  // rápido y más barato) y la conversación queda guardada para consultarla.
  async function terminar() {
    setMenuAbierto(false)
    try {
      const { guardada } = await agenteApi.terminar()
      setMensajes([])
      // El servidor descartó las tarjetas sin confirmar de esa conversación.
      setPropuestas([])
      setGuardado('')
      setError('')
      setRecienTerminada(guardada)
    } catch (err) {
      setError(err.message)
    }
  }

  // La foto del clip va a la última tarjeta de gasto que aún no tenga factura;
  // si no hay ninguna, queda esperando la próxima.
  function adjuntarFoto(archivo) {
    const tarjeta = propuestas.findLast((p) => p.tipo === 'movimiento' && !facturas[p.id])
    if (tarjeta) {
      setFacturas((anteriores) => ({ ...anteriores, [tarjeta.id]: archivo }))
    } else {
      setFotoPendiente(archivo)
      campo.current?.focus()
    }
  }

  function seguir() {
    setGuardado('')
    campo.current?.focus()
  }

  async function borrarHistorial() {
    setMenuAbierto(false)
    if (!confirm('¿Borrar esta conversación y todas las guardadas? No se puede deshacer.')) return
    try {
      await agenteApi.borrar()
      setMensajes([])
      setGuardado('')
      setError('')
      setRecienTerminada(false)
    } catch (err) {
      setError(err.message)
    }
  }

  function descargar() {
    setMenuAbierto(false)
    descargarConversacion(mensajes)
  }

  function verGuardadas() {
    setMenuAbierto(false)
    setVista('guardadas')
  }

  if (vista === 'guardadas') {
    return <ConversacionesGuardadas onVolver={() => setVista('chat')} />
  }

  // Una tarjeta se fue: o se guardó (llega el movimiento creado) o se
  // descartó. El aviso es corto pero explícito — es el único momento en que
  // algo del asistente sí tocó las cuentas.
  function propuestaResuelta(id, movimiento, aviso) {
    setPropuestas((anteriores) => anteriores.filter((p) => p.id !== id))
    setFacturas(({ [id]: _, ...resto }) => resto)
    if (movimiento) {
      const donde = movimiento.categoria_nombre ? ` en ${movimiento.categoria_nombre}` : ''
      const conFactura = movimiento.factura ? ', con su factura' : ''
      setGuardado(`Guardado: ${formatearMonto(movimiento.monto)}${donde}${conFactura}`)
      setError(aviso || '')
      onGuardado?.(movimiento)
    } else {
      setGuardado('')
    }
  }

  return (
    <section className="wa">
      <header className="wa-cabecera">
        {onCerrar && (
          <button type="button" className="wa-icono" onClick={onCerrar} aria-label="Cerrar el chat">
            ←
          </button>
        )}
        <div className="wa-cabecera-titulo">
          <span>{sinConfigurar ? 'Sin conectar' : enviando ? 'Consultando' : 'Consulta'}</span>
          <strong>Asistente</strong>
        </div>
        {!soloLectura && !sinConfigurar && !cargando && (
          <MenuChat
            abierto={menuAbierto}
            onAlternar={() => setMenuAbierto((a) => !a)}
            onCerrar={() => setMenuAbierto(false)}
          >
            {mensajes.length > 0 && (
              <>
                <button type="button" role="menuitem" onClick={terminar}>
                  Terminar y guardar
                </button>
                <button type="button" role="menuitem" onClick={descargar}>
                  Descargar esta conversación
                </button>
              </>
            )}
            <button type="button" role="menuitem" onClick={verGuardadas}>
              Conversaciones guardadas
            </button>
            <button type="button" role="menuitem" className="peligro" onClick={borrarHistorial}>
              Borrar todo el historial
            </button>
          </MenuChat>
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
                {recienTerminada && (
                  <p className="wa-sistema wa-ok">
                    ✓ Tu conversación quedó guardada. La encuentras en ⋮ › Conversaciones guardadas.
                  </p>
                )}
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
                factura={facturas[p.id]}
                onResuelta={propuestaResuelta}
              />
            ))}

            {/* Después de guardar, la pregunta de siempre: ¿algo más? Si no,
                se termina y el chat queda limpio para la próxima vez. Solo
                cuando no quedan otras tarjetas por confirmar. */}
            {guardado && (
              <div className="wa-sistema wa-algo-mas">
                <p className="wa-ok">✓ {guardado}</p>
                {propuestas.length === 0 && (
                  <>
                    <p>¿Necesitas algo más?</p>
                    <div className="chat-sugerencias">
                      <button type="button" className="chip" onClick={seguir}>
                        Sí, otra cosa
                      </button>
                      <button type="button" className="chip" onClick={terminar}>
                        No, terminar
                      </button>
                    </div>
                  </>
                )}
              </div>
            )}
            {error && <p className="wa-sistema wa-error">{error}</p>}
            {dictado.error && <p className="wa-sistema wa-error">{dictado.error}</p>}
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

      {fotoPendiente && (
        <div className="wa-adjunto-pendiente">
          <VistaAdjunto
            archivo={fotoPendiente}
            texto="Factura lista"
            onQuitar={() => setFotoPendiente(null)}
          />
          <p className="tenue">
            Cuéntame de qué es y cuánto fue: la adjunto al gasto que te prepare.
          </p>
        </div>
      )}

      {!soloLectura && !sinConfigurar && (
        <form className="wa-escribir" onSubmit={enviar}>
          <BotonAdjuntar
            onArchivo={adjuntarFoto}
            className="wa-clip"
            titulo="Adjuntar la foto de una factura"
            disabled={cargando || enviando}
          >
            📎
          </BotonAdjuntar>
          <label htmlFor="mensaje-asistente" className="sr-solo">
            Escribe un mensaje
          </label>
          <textarea
            id="mensaje-asistente"
            ref={campo}
            value={texto}
            onChange={alEscribir}
            onKeyDown={alTeclear}
            placeholder={dictado.escuchando ? 'Te escucho...' : 'Escribe un mensaje'}
            maxLength={MAX_CARACTERES}
            rows={1}
            disabled={cargando || enviando || restantes === 0}
          />
          {/* Como en WhatsApp: con el cajón vacío el botón es el micrófono, y
              con texto es enviar. Mientras se dicta sigue siendo el micrófono
              (en rojo) para poder pararlo. */}
          {hayDictado && (dictado.escuchando || texto.trim() === '') ? (
            <button
              type="button"
              className={`wa-enviar wa-microfono ${dictado.escuchando ? 'grabando' : ''}`}
              onClick={alternarDictado}
              disabled={cargando || enviando || restantes === 0}
              aria-label={dictado.escuchando ? 'Dejar de dictar' : 'Dictar un mensaje'}
              aria-pressed={dictado.escuchando}
              title={dictado.escuchando ? 'Dejar de dictar' : 'Dictar un mensaje'}
            >
              {dictado.escuchando ? '■' : '🎤'}
            </button>
          ) : (
            <button
              type="submit"
              className="wa-enviar"
              disabled={cargando || enviando || texto.trim() === ''}
              aria-label="Enviar"
            >
              ➤
            </button>
          )}
        </form>
      )}
    </section>
  )
}

// El menú de los tres puntos de la cabecera. Se cierra con Escape (sin cerrar
// también el panel del chat) y con un clic en cualquier otra parte.
function MenuChat({ abierto, onAlternar, onCerrar, children }) {
  const caja = useRef(null)

  useEffect(() => {
    if (!abierto) return
    const alClic = (e) => {
      if (!caja.current?.contains(e.target)) onCerrar()
    }
    document.addEventListener('pointerdown', alClic)
    return () => document.removeEventListener('pointerdown', alClic)
  }, [abierto, onCerrar])

  function alTeclear(e) {
    if (e.key === 'Escape' && abierto) {
      // Sin esto, el mismo Escape cierra el chat entero.
      e.stopPropagation()
      onCerrar()
    }
  }

  return (
    <div className="wa-menu" ref={caja} onKeyDown={alTeclear}>
      <button
        type="button"
        className="wa-icono"
        onClick={onAlternar}
        aria-label="Más opciones"
        aria-haspopup="menu"
        aria-expanded={abierto}
      >
        ⋮
      </button>
      {abierto && (
        <div className="wa-menu-lista" role="menu">
          {children}
        </div>
      )}
    </div>
  )
}
