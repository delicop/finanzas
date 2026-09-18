import { useCallback, useEffect, useState } from 'react'
import { pushApi } from './api'

// Las notificaciones que llegan al celular con la app CERRADA.
//
// Tres piezas tienen que estar de acuerdo para que esto funcione, y por eso el
// estado tiene tantos casos:
//
//  1. El SERVIDOR: si no tiene llaves VAPID configuradas, /api/push responde
//     404 y aquí no se ofrece nada.
//  2. El NAVEGADOR: iPhone solo lo permite con la app instalada desde
//     Compartir › Agregar a inicio, y en desarrollo no hay service worker.
//  3. El PERMISO del usuario, que puede estar concedido, pendiente o
//     BLOQUEADO — y si está bloqueado, pedirlo otra vez no hace nada: hay que
//     decirle que lo cambie en los ajustes del navegador.
//
// Sin ninguna de las tres, la app funciona igual: los avisos siguen en la
// campana. Nada de esto es indispensable.

// Convierte la llave pública del servidor (base64url) a los bytes que espera
// pushManager.subscribe(). El navegador no acepta el texto: quiere un
// Uint8Array, y no hay atajo.
function llaveABytes(base64url) {
  const relleno = '='.repeat((4 - (base64url.length % 4)) % 4)
  const base64 = (base64url + relleno).replace(/-/g, '+').replace(/_/g, '/')
  const crudo = atob(base64)
  const bytes = new Uint8Array(crudo.length)
  for (let i = 0; i < crudo.length; i++) bytes[i] = crudo.charCodeAt(i)
  return bytes
}

function elNavegadorPuede() {
  return (
    typeof window !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  )
}

// useAvisosEnElCelular devuelve todo lo que necesita el interruptor de la
// barra: si tiene sentido mostrarlo, en qué estado está y cómo cambiarlo.
export function useAvisosEnElCelular() {
  // 'cargando' | 'no-disponible' | 'apagado' | 'encendido' | 'bloqueado'
  const [estado, setEstado] = useState('cargando')
  const [error, setError] = useState('')
  const [ocupado, setOcupado] = useState(false)

  const revisar = useCallback(async () => {
    if (!elNavegadorPuede()) {
      setEstado('no-disponible')
      return
    }

    try {
      // Si el servidor no tiene llaves, esta llamada da 404 y se acabó.
      await pushApi.estado()
    } catch {
      setEstado('no-disponible')
      return
    }

    if (Notification.permission === 'denied') {
      setEstado('bloqueado')
      return
    }

    try {
      const registro = await navigator.serviceWorker.ready
      const suscripcion = await registro.pushManager.getSubscription()
      // El navegador es la fuente de verdad de si este dispositivo está
      // suscrito, no lo que diga el servidor: aquí puede haber una suscripción
      // que el servidor ya borró por muerta, o al revés.
      setEstado(suscripcion ? 'encendido' : 'apagado')
    } catch {
      setEstado('no-disponible')
    }
  }, [])

  useEffect(() => {
    revisar()
  }, [revisar])

  const encender = useCallback(async () => {
    setOcupado(true)
    setError('')
    try {
      const permiso = await Notification.requestPermission()
      if (permiso === 'denied') {
        setEstado('bloqueado')
        return
      }
      if (permiso !== 'granted') return // lo cerró sin decidir

      const { clave_publica: clave } = await pushApi.estado()
      const registro = await navigator.serviceWorker.ready

      const suscripcion = await registro.pushManager.subscribe({
        // Obligatorio en todos los navegadores modernos: no se permite
        // suscribirse para mandar mensajes vacíos y silenciosos.
        userVisibleOnly: true,
        applicationServerKey: llaveABytes(clave),
      })

      await pushApi.suscribir(suscripcion.toJSON())
      setEstado('encendido')
    } catch (err) {
      setError(err.message ?? 'No se pudieron activar los avisos')
    } finally {
      setOcupado(false)
    }
  }, [])

  const apagar = useCallback(async () => {
    setOcupado(true)
    setError('')
    try {
      const registro = await navigator.serviceWorker.ready
      const suscripcion = await registro.pushManager.getSubscription()

      if (suscripcion) {
        // Primero el servidor y después el navegador: al revés, si falla la
        // segunda parte, el servidor seguiría mandando avisos a un endpoint
        // que ya no existe.
        await pushApi.desuscribir(suscripcion.endpoint).catch(() => {})
        await suscripcion.unsubscribe()
      }
      setEstado('apagado')
    } catch (err) {
      setError(err.message ?? 'No se pudieron apagar los avisos')
    } finally {
      setOcupado(false)
    }
  }, [])

  return { estado, error, ocupado, encender, apagar }
}
