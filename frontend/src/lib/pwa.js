import { useEffect, useState } from 'react'

// Registra el service worker (public/sw.js), que es lo que permite instalar la
// app en el teléfono.
//
// Solo en producción: en desarrollo Vite recarga los módulos al vuelo y un
// service worker guardando archivos en caché haría que los cambios no se vean.
// El navegador además solo lo acepta con HTTPS (o en localhost).
export function registrarServiceWorker() {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      // Sin service worker la app funciona igual; solo no se puede instalar.
    })
  })
}

// Chrome y Edge avisan con "beforeinstallprompt" cuando la app se puede
// instalar. El aviso llega una sola vez y muy temprano (a veces antes de que
// React monte), así que se escucha desde que carga el módulo.
let avisoGuardado = null
const oyentes = new Set()

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    // Sin esto el navegador muestra su propia barra, en el momento que él
    // quiera. Se guarda para ofrecerlo desde un botón nuestro.
    e.preventDefault()
    avisoGuardado = e
    oyentes.forEach((fn) => fn(true))
  })
  window.addEventListener('appinstalled', () => {
    avisoGuardado = null
    oyentes.forEach((fn) => fn(false))
  })
}

function yaInstalada() {
  return (
    window.matchMedia?.('(display-mode: standalone)').matches || window.navigator.standalone === true
  )
}

// useInstalarApp dice si se puede ofrecer instalar y da la función que abre
// el diálogo del navegador. En iPhone nunca llega el aviso: allá se instala
// desde Compartir › "Agregar a inicio", y por eso esIOS va aparte.
export function useInstalarApp() {
  const [disponible, setDisponible] = useState(() => avisoGuardado !== null)

  useEffect(() => {
    oyentes.add(setDisponible)
    return () => oyentes.delete(setDisponible)
  }, [])

  async function instalar() {
    if (!avisoGuardado) return
    const aviso = avisoGuardado
    avisoGuardado = null
    setDisponible(false)
    await aviso.prompt()
  }

  const esIOS = /iphone|ipad|ipod/i.test(navigator.userAgent) && !yaInstalada()

  return { disponible, instalar, esIOS, instalada: yaInstalada() }
}

// useEnLinea sigue si hay conexión, para avisar en vez de mostrar errores
// raros cuando la app abre sin señal.
export function useEnLinea() {
  const [enLinea, setEnLinea] = useState(() => navigator.onLine)
  useEffect(() => {
    const si = () => setEnLinea(true)
    const no = () => setEnLinea(false)
    window.addEventListener('online', si)
    window.addEventListener('offline', no)
    return () => {
      window.removeEventListener('online', si)
      window.removeEventListener('offline', no)
    }
  }, [])
  return enLinea
}
