import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'

const TemaContext = createContext(null)
const CLAVE = 'finanzas_tema'

// Lee la preferencia guardada; si no hay ninguna, usa la del sistema
// operativo (si tienes el celular en modo oscuro, la app abre oscura).
function temaInicial() {
  try {
    const guardado = localStorage.getItem(CLAVE)
    if (guardado === 'claro' || guardado === 'oscuro') return guardado
  } catch {
    // localStorage puede fallar en modo incógnito: seguimos con el default.
  }
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'claro' : 'oscuro'
}

export function TemaProvider({ children }) {
  const [tema, setTema] = useState(temaInicial)

  // El tema se aplica con un atributo en <html>, no con clases en React.
  // Así el CSS lo resuelve solo con `:root[data-tema="claro"]` y no hay que
  // pasar el tema por props a cada componente.
  useEffect(() => {
    document.documentElement.setAttribute('data-tema', tema)
    // color-scheme le avisa al navegador para que pinte los controles
    // nativos (calendario, scrollbars) acordes al tema.
    document.documentElement.style.colorScheme = tema === 'claro' ? 'light' : 'dark'
    try {
      localStorage.setItem(CLAVE, tema)
    } catch {
      // Sin persistencia el tema igual funciona durante la sesión.
    }
  }, [tema])

  const alternar = useCallback(() => {
    setTema((t) => (t === 'claro' ? 'oscuro' : 'claro'))
  }, [])

  const valor = useMemo(() => ({ tema, alternar }), [tema, alternar])

  return <TemaContext.Provider value={valor}>{children}</TemaContext.Provider>
}

export function useTema() {
  const ctx = useContext(TemaContext)
  if (!ctx) throw new Error('useTema debe usarse dentro de <TemaProvider>')
  return ctx
}
