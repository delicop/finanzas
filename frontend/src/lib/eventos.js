import { useEffect, useRef } from 'react'

// Un aviso dentro de la app: "se guardó un movimiento".
//
// El chat vive en una burbuja flotante encima de cualquier pantalla, así que
// no sabe quién está debajo (el Resumen, Movimientos...). En vez de pasarle
// funciones de recarga de pantalla en pantalla, el chat avisa y cada pantalla
// que muestra cifras se recarga sola si está abierta.
const EVENTO = 'finanzas:movimiento-guardado'

export function avisarMovimientoGuardado() {
  window.dispatchEvent(new Event(EVENTO))
}

// useAlGuardarMovimiento corre `alGuardar` cada vez que llega el aviso.
// La función va en una ref para no tener que volver a suscribirse en cada
// render, que es cuando cambia de identidad.
export function useAlGuardarMovimiento(alGuardar) {
  const ultima = useRef(alGuardar)
  ultima.current = alGuardar

  useEffect(() => {
    const escuchar = () => ultima.current()
    window.addEventListener(EVENTO, escuchar)
    return () => window.removeEventListener(EVENTO, escuchar)
  }, [])
}

// Otro aviso: "abre la guía". El tutorial vive en el Layout (se abre solo la
// primera vez y con el botón de ayuda de la barra), pero la lista de primeros
// pasos del Resumen también lo ofrece, y no tiene cómo llegar hasta allá.
const ABRIR_TUTORIAL = 'finanzas:abrir-tutorial'

export function abrirTutorial() {
  window.dispatchEvent(new Event(ABRIR_TUTORIAL))
}

export function useAlAbrirTutorial(alAbrir) {
  const ultima = useRef(alAbrir)
  ultima.current = alAbrir

  useEffect(() => {
    const escuchar = () => ultima.current()
    window.addEventListener(ABRIR_TUTORIAL, escuchar)
    return () => window.removeEventListener(ABRIR_TUTORIAL, escuchar)
  }, [])
}
