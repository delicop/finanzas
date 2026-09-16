import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/AuthContext'
import { TemaProvider } from './lib/TemaContext'
import Layout from './componentes/Layout'
import Login from './paginas/Login'
import Dashboard from './paginas/Dashboard'
import Categorias from './paginas/Categorias'
import MediosPago from './paginas/MediosPago'
import Movimientos from './paginas/Movimientos'

function Ruteo() {
  const { usuario, cargando } = useAuth()

  // Sin esto se ve un parpadeo del login mientras /me responde.
  if (cargando) return <div className="pantalla-centrada tenue">Cargando...</div>

  // Sin sesión no hay router: cualquier URL muestra el login.
  if (!usuario) return <Login />

  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/movimientos" element={<Movimientos />} />
        <Route path="/categorias" element={<Categorias />} />
        <Route path="/medios-pago" element={<MediosPago />} />
        {/* Cualquier otra ruta vuelve al resumen */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  )
}

export default function App() {
  return (
    <TemaProvider>
      <BrowserRouter>
        <AuthProvider>
          <Ruteo />
        </AuthProvider>
      </BrowserRouter>
    </TemaProvider>
  )
}
