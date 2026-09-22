import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// TUNEL=1 cuando la app sale a internet por Cloudflare Tunnel
// (docker-compose.tunel.yml). Cambia dos cosas, ninguna en el código:
//
//  - Se aceptan las peticiones que llegan con el dominio del túnel. Vite
//    rechaza por defecto un Host desconocido, y sin esto la página respondería
//    "Blocked request".
//  - Se apaga el hot-reload: su conexión websocket no sobrevive al túnel y el
//    navegador se queda reintentando. Para mostrar la app no hace falta.
const porTunel = process.env.TUNEL === '1'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0', // obligatorio dentro de Docker: sin esto solo escucha en 127.0.0.1 del contenedor
    port: 5173,
    watch: {
      // En Windows/Docker los eventos de filesystem no siempre llegan al contenedor.
      // El polling es mas pesado pero hace que el hot-reload funcione de verdad.
      usePolling: true,
      interval: 300,
    },

    // Con VITE_API_URL vacío, la app llama a "/api/..." (la misma dirección
    // desde la que se abrió) y este proxy lo lleva al backend. Así, detrás de
    // un túnel, todo viaja por un solo dominio HTTPS: no hay CORS que
    // configurar y no hay que adivinar la URL pública de la API.
    //
    // Solo aplica al servidor de desarrollo. En la Raspberry el frontend lo
    // sirve nginx con la URL de la API incrustada en el build.
    proxy: {
      '/api': { target: process.env.API_INTERNA ?? 'http://backend:8080', changeOrigin: true },
      '/uploads': { target: process.env.API_INTERNA ?? 'http://backend:8080', changeOrigin: true },
    },

    ...(porTunel ? { allowedHosts: true, hmr: false } : {}),
  },
})
