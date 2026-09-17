import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { registrarServiceWorker } from './lib/pwa'
import './estilos.css'

registrarServiceWorker()

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>
)
