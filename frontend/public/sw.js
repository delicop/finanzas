// Service worker de Finanzas: lo que hace que la app se pueda instalar en el
// teléfono y abra aunque la señal esté mala.
//
// Reglas, de la más importante a la menos:
//
//  1. /api NUNCA pasa por aquí. Son datos de plata: una cifra vieja sacada de
//     un caché es peor que un error. El navegador va directo al servidor.
//  2. Las páginas (navegación) van primero a la red. Si no hay red, se sirve
//     la última versión guardada de la app, que al menos abre y dice que no
//     hay conexión en vez del dinosaurio.
//  3. Los archivos de /assets/ llevan un hash en el nombre (index-C1w6.js):
//     si el nombre es el mismo, el contenido es el mismo. Se sirven del caché.
//
// Al cambiar la lógica de este archivo, sube VERSION: así el navegador borra
// el caché anterior en la siguiente visita.
const VERSION = 'finanzas-v1'
const PRECARGA = ['/', '/manifest.webmanifest', '/iconos/icono-192.png', '/iconos/icono-512.png']

self.addEventListener('install', (evento) => {
  evento.waitUntil(
    caches
      .open(VERSION)
      .then((cache) => cache.addAll(PRECARGA))
      // La versión nueva entra de una vez, sin esperar a que se cierren
      // todas las pestañas.
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (evento) => {
  evento.waitUntil(
    caches
      .keys()
      .then((nombres) =>
        Promise.all(nombres.filter((n) => n !== VERSION).map((n) => caches.delete(n))),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (evento) => {
  const peticion = evento.request
  if (peticion.method !== 'GET') return

  const url = new URL(peticion.url)
  // Otro dominio (la API en producción puede estar en otro) o la API: que
  // lo maneje el navegador como siempre.
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/uploads/')) return

  if (peticion.mode === 'navigate') {
    evento.respondWith(primeroLaRed(peticion))
    return
  }

  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/iconos/')) {
    evento.respondWith(primeroElCache(peticion))
  }
})

async function primeroLaRed(peticion) {
  const cache = await caches.open(VERSION)
  try {
    const respuesta = await fetch(peticion)
    // Se guarda como "/" porque la app es una sola página: cualquier ruta
    // (/movimientos, /categorias) devuelve el mismo index.html.
    if (respuesta.ok) cache.put('/', respuesta.clone())
    return respuesta
  } catch {
    const guardada = await cache.match('/')
    return guardada ?? Response.error()
  }
}

async function primeroElCache(peticion) {
  const cache = await caches.open(VERSION)
  const guardada = await cache.match(peticion)
  if (guardada) return guardada

  const respuesta = await fetch(peticion)
  if (respuesta.ok) cache.put(peticion, respuesta.clone())
  return respuesta
}
