import { useCallback, useEffect, useRef, useState } from 'react'

// Leer en voz alta las respuestas del asistente, con la voz que trae el
// propio dispositivo (Web Speech API, la hermana de `dictado.js`).
//
// No hay que instalar nada ni pagar otro servicio, y el texto NO sale del
// teléfono: a diferencia del dictado —que manda el audio a Google para
// convertirlo—, aquí la voz se sintetiza en el aparato y no viaja a ningún
// lado. Firefox también la trae, así que esta funciona en un navegador más
// que el micrófono.
const sintetizador = typeof window !== 'undefined' ? window.speechSynthesis : null

export const hayVoz = Boolean(sintetizador)

// Lo de leer TODAS las respuestas sin pedirlo viene apagado, y la decisión se
// recuerda: una app de plata que se pone a hablar sola en una reunión es un
// problema, pero volver a apagarla todos los días también.
const CLAVE = 'finanzas:voz-asistente'

// useVoz() devuelve { auto, leyendo, alternarAuto, leer, alternarLectura, callar }.
//
//   leyendo          el id del mensaje que suena ahora mismo, o null
//   leer(id, texto)  lo lee, venga de un toque o del modo automático
//   alternarLectura  el botón de cada respuesta: lo lee, o lo calla si ya sonaba
//   auto             si cada respuesta nueva se lee sola
export function useVoz() {
  const [auto, setAuto] = useState(recordada)
  const [leyendo, setLeyendo] = useState(null)
  // La voz en español que se eligió, si el dispositivo tiene alguna.
  const voz = useRef(null)

  useEffect(() => {
    if (!sintetizador) return

    // getVoices() suele venir vacío en la primera llamada: el navegador carga
    // la lista aparte y avisa con 'voiceschanged'. Por eso se mira dos veces.
    const elegir = () => {
      voz.current = mejorVoz(sintetizador.getVoices())
    }
    elegir()
    sintetizador.addEventListener('voiceschanged', elegir)

    return () => {
      sintetizador.removeEventListener('voiceschanged', elegir)
      // Si el chat se cierra a mitad de una frase, se calla.
      sintetizador.cancel()
    }
  }, [])

  const callar = useCallback(() => {
    sintetizador?.cancel()
    setLeyendo(null)
  }, [])

  const leer = useCallback((id, texto) => {
    if (!sintetizador) return
    const dicho = paraLeer(texto)
    if (!dicho) return

    // Lo anterior se corta: dos respuestas encimadas no se entienden.
    sintetizador.cancel()

    const frase = new SpeechSynthesisUtterance(dicho)
    frase.lang = 'es-CO'
    if (voz.current) frase.voice = voz.current
    // El `cancel()` de arriba hace que la frase anterior termine (con error
    // "interrupted") DESPUÉS de que esta arranque. Sin comparar el id, ese
    // final tardío apagaría el botón de la que acaba de empezar.
    const soltar = () => setLeyendo((actual) => (actual === id ? null : actual))
    frase.onend = soltar
    frase.onerror = soltar

    sintetizador.speak(frase)
    // Sin esperar al onstart: entre el toque y el primer sonido puede pasar
    // medio segundo, y un botón que no responde al instante se vuelve a tocar.
    setLeyendo(id)
  }, [])

  const alternarLectura = useCallback(
    (id, texto) => {
      if (leyendo === id) callar()
      else leer(id, texto)
    },
    [leyendo, leer, callar],
  )

  // Lo de prender y apagar se hace aquí afuera y no dentro de setAuto: un
  // actualizador de estado tiene que ser una función limpia (React lo llama
  // dos veces en desarrollo), y hablarle al sintetizador no lo es.
  const alternarAuto = useCallback(() => {
    const ahora = !auto
    setAuto(ahora)

    try {
      localStorage.setItem(CLAVE, ahora ? '1' : '0')
    } catch {
      // Modo incógnito o almacenamiento bloqueado: la voz funciona igual,
      // solo que no se recuerda para la próxima visita.
    }

    if (!ahora) {
      sintetizador?.cancel()
      setLeyendo(null)
    } else if (sintetizador) {
      // Safari en iPhone solo deja hablar si la primera vez salió de un
      // toque del usuario, y la respuesta del asistente llega segundos
      // después. Este silencio, dicho DENTRO del toque, abre el permiso
      // para las frases de después. (Con el botón de cada respuesta no hace
      // falta: ese toque ya es el permiso.)
      const silencio = new SpeechSynthesisUtterance(' ')
      silencio.volume = 0
      sintetizador.speak(silencio)
    }
  }, [auto])

  // Chrome en computador se queda callado si la pestaña lleva rato hablando
  // (un bug viejo suyo: corta cerca de los 15 segundos). Este pellizco cada
  // 10 s lo mantiene despierto mientras haya algo que decir, y una respuesta
  // larga del asistente pasa de sobra esos 15 segundos.
  useEffect(() => {
    if (!leyendo || !sintetizador) return
    const reloj = setInterval(() => {
      sintetizador.pause()
      sintetizador.resume()
    }, 10000)
    return () => clearInterval(reloj)
  }, [leyendo])

  return { auto, leyendo, alternarAuto, leer, alternarLectura, callar }
}

function recordada() {
  try {
    return localStorage.getItem(CLAVE) === '1'
  } catch {
    return false
  }
}

// La voz más parecida a como se habla aquí: primero Colombia, después
// cualquier español de América, y de último el que haya. Si el dispositivo no
// trae ninguna en español no se fuerza: se deja la de por defecto, que con
// `lang = 'es-CO'` suele acertar igual.
function mejorVoz(voces) {
  if (!voces?.length) return null
  const es = voces.filter((v) => v.lang?.toLowerCase().startsWith('es'))
  if (!es.length) return null
  const como = (prefijos) =>
    es.find((v) => prefijos.some((p) => v.lang.toLowerCase().replace('_', '-').startsWith(p)))
  return como(['es-co']) ?? como(['es-mx', 'es-us', 'es-419']) ?? es[0]
}

// El texto de la pantalla no se lee bien tal cual. Esto lo deja hablado:
//
//   "Llevas **$1.250.000** este mes 📊"  ->  "Llevas 1250000 pesos este mes"
//
// Lo importante son los montos: "$45.000" leído letra por letra suena
// "cuarenta y cinco punto cero cero cero". Sin los puntos de los miles, el
// sintetizador lo dice como lo diría una persona.
export function paraLeer(texto) {
  if (!texto) return ''

  return (
    String(texto)
      // Las **negritas** que el modelo usa mucho.
      .replace(/\*\*(.+?)\*\*/g, '$1')
      // Los montos: se quitan los separadores de miles y se dice "pesos".
      .replace(/\$\s?(\d[\d.]*)(,\d+)?/g, (_, entero, decimales) => {
        const limpio = entero.replace(/\./g, '')
        return `${limpio}${decimales ?? ''} pesos`
      })
      // Emojis y símbolos: el 📎 del adjunto, el ✓ de guardado, las viñetas.
      .replace(/\p{Extended_Pictographic}|[✓✔•·–—]/gu, ' ')
      // Los guiones de lista al empezar una línea.
      .replace(/^[\s>]*[-*]\s+/gm, '')
      // Cada salto de línea es una pausa; sin punto, todo sale de corrido.
      .replace(/\n+/g, '. ')
      .replace(/\.\s*\./g, '.')
      .replace(/\s+/g, ' ')
      .trim()
  )
}
