import { useEffect, useRef, useState } from 'react'

// Dictado por voz con el reconocimiento que trae el navegador (Web Speech API).
//
// No hay que instalar nada ni pagar otro servicio: Chrome (Android y
// computador), Edge y Safari (iPhone) lo traen. Firefox no, y ahí el botón del
// micrófono simplemente no aparece.
//
// Privacidad: Chrome manda el audio a los servidores de Google para
// convertirlo en texto. El audio NO pasa por nuestro servidor; lo que llega es
// el texto, igual que si se hubiera escrito.
const Reconocedor =
  typeof window !== 'undefined' ? window.SpeechRecognition || window.webkitSpeechRecognition : null

export const hayDictado = Boolean(Reconocedor)

// useDictado(alTexto) devuelve { escuchando, empezar, parar, error }.
// alTexto recibe el texto reconocido hasta el momento (incluye lo provisional)
// cada vez que cambia.
export function useDictado(alTexto) {
  const [escuchando, setEscuchando] = useState(false)
  const [error, setError] = useState('')
  const reconocedor = useRef(null)
  const avisar = useRef(alTexto)
  avisar.current = alTexto

  // Si el chat se cierra con el micrófono abierto, se apaga.
  useEffect(() => () => reconocedor.current?.abort(), [])

  function empezar(textoPrevio = '') {
    if (!Reconocedor || escuchando) return
    setError('')

    const r = new Reconocedor()
    r.lang = 'es-CO'
    // Resultados provisionales: el texto va apareciendo mientras se habla,
    // como en el teclado del teléfono.
    r.interimResults = true
    // Una frase y se detiene solo al callar. Es lo natural para "pagué 20
    // mil de gasolina".
    r.continuous = false

    const base = textoPrevio.trim() ? `${textoPrevio.trim()} ` : ''

    r.onresult = (e) => {
      let dicho = ''
      for (const resultado of e.results) dicho += resultado[0].transcript
      avisar.current(base + dicho.trim())
    }
    r.onerror = (e) => {
      // "aborted" es que lo paramos nosotros y "no-speech" que no se dijo
      // nada: ninguno merece un mensaje rojo.
      if (e.error === 'not-allowed' || e.error === 'service-not-allowed') {
        setError('Sin permiso para usar el micrófono. Actívalo en la configuración del navegador.')
      } else if (e.error === 'network') {
        setError('El dictado necesita conexión a internet.')
      } else if (e.error !== 'aborted' && e.error !== 'no-speech') {
        setError('No se pudo usar el micrófono. Intenta de nuevo o escribe el mensaje.')
      }
    }
    r.onend = () => {
      setEscuchando(false)
      reconocedor.current = null
    }

    reconocedor.current = r
    try {
      r.start()
      setEscuchando(true)
    } catch {
      setError('No se pudo usar el micrófono.')
    }
  }

  function parar() {
    reconocedor.current?.stop()
  }

  return { escuchando, empezar, parar, error }
}
