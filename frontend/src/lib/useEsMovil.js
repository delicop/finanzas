import { useEffect, useState } from 'react'

// Punto de quiebre unico para toda la app. Si lo cambias aqui, cambia en
// todas las pantallas a la vez (y debe coincidir con el @media del CSS).
export const ANCHO_MOVIL = 720

// useEsMovil dice si la pantalla es de telefono.
//
// Por que en JS y no solo con CSS: en movil no queremos "esconder la tabla",
// queremos renderizar algo DISTINTO (tarjetas en vez de filas, filtros
// plegados en vez de abiertos). Con CSS habria que pintar las dos versiones
// y ocultar una; asi solo existe la que se usa.
export function useEsMovil(ancho = ANCHO_MOVIL) {
  const consulta = `(max-width: ${ancho}px)`

  // El valor inicial se calcula antes del primer pintado para que no se vea
  // un parpadeo de la tabla de escritorio en el telefono.
  const [esMovil, setEsMovil] = useState(() => window.matchMedia(consulta).matches)

  useEffect(() => {
    const mq = window.matchMedia(consulta)
    const alCambiar = (e) => setEsMovil(e.matches)

    // Sincronizamos por si la pantalla cambio entre el render y este efecto
    // (por ejemplo al girar el telefono mientras cargaba).
    setEsMovil(mq.matches)

    mq.addEventListener('change', alCambiar)
    return () => mq.removeEventListener('change', alCambiar)
  }, [consulta])

  return esMovil
}
