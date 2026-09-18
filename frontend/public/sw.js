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
const VERSION = 'finanzas-v3'
// Las tipografías van en la precarga: son la cara de la app, y sin ellas
// abierta sin señal se vería con la serif del sistema. Son 75 KB entre las
// dos y se guardan una sola vez.
const PRECARGA = [
  '/',
  '/manifest.webmanifest',
  '/iconos/icono-192.png',
  '/iconos/icono-512.png',
  '/fuentes/cormorant-garamond-latin.woff2',
  '/fuentes/lora-latin.woff2',
]

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

  if (
    url.pathname.startsWith('/assets/') ||
    url.pathname.startsWith('/iconos/') ||
    // Las tipografías llevan el nombre fijo, pero cambian tan poco como los
    // íconos: del caché y listo.
    url.pathname.startsWith('/fuentes/')
  ) {
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

/* ---------------------------------------------------------------------------
 * Las notificaciones que llegan con la app CERRADA
 *
 * Esta es la única parte del service worker que corre cuando nadie tiene la
 * app abierta: el navegador lo despierta solo para entregar el mensaje.
 *
 * El contenido llega cifrado y el navegador ya lo descifró cuando nos lo pasa
 * aquí. Lo que llega es el JSON que arma internal/push (título, cuerpo, url y
 * etiqueta).
 * ------------------------------------------------------------------------ */

self.addEventListener('push', (evento) => {
  // Si el mensaje viene sin datos o con basura, igual se muestra algo: un
  // aviso genérico es mucho mejor que una notificación vacía (que en algunos
  // navegadores sale como "Este sitio se actualizó en segundo plano").
  let datos = {}
  try {
    datos = evento.data ? evento.data.json() : {}
  } catch {
    datos = {}
  }

  const titulo = datos.titulo || 'Finanzas'
  const opciones = {
    body: datos.cuerpo || '',
    icon: '/iconos/icono-192.png',
    badge: '/iconos/icono-192.png',
    // La etiqueta agrupa: un resumen semanal nuevo reemplaza al anterior en
    // vez de apilarse. Sin esto, tres días sin abrir la app dejan la bandeja
    // llena de avisos casi iguales.
    tag: datos.etiqueta || 'finanzas',
    // A dónde lleva el toque. Viaja en `data` porque es lo único que
    // sobrevive hasta el evento de clic.
    data: { url: datos.url || '/' },
  }

  // waitUntil mantiene vivo el service worker hasta que la notificación esté
  // mostrada. Sin esto el navegador puede apagarlo antes y el aviso no sale.
  evento.waitUntil(self.registration.showNotification(titulo, opciones))
})

self.addEventListener('notificationclick', (evento) => {
  evento.notification.close()
  const destino = evento.notification.data?.url || '/'

  evento.waitUntil(
    (async () => {
      const abiertas = await self.clients.matchAll({
        type: 'window',
        includeUncontrolled: true,
      })

      // Si la app ya está abierta se reutiliza esa ventana y se navega dentro:
      // abrir una segunda pestaña de la misma app es molesto y deja al usuario
      // con dos sesiones de la misma cosa.
      for (const cliente of abiertas) {
        if (new URL(cliente.url).origin === self.location.origin) {
          await cliente.focus()
          if ('navigate' in cliente) await cliente.navigate(destino)
          return
        }
      }

      await self.clients.openWindow(destino)
    })(),
  )
})
