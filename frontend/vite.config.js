import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

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
  },
})
