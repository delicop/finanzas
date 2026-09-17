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
