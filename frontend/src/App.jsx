import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/AuthContext'
import { TemaProvider } from './lib/TemaContext'
import Layout from './componentes/Layout'
import Login from './paginas/Login'
import Dashboard from './paginas/Dashboard'
import Categorias from './paginas/Categorias'
import MediosPago from './paginas/MediosPago'
import Movimientos from './paginas/Movimientos'
import Negocio from './paginas/Negocio'
import Clientes from './paginas/Clientes'
import Planes from './paginas/Planes'
import Errores from './paginas/Errores'

function Ruteo() {
  const { usuario, cargando, esAdmin, verComo } = useAuth()

  // Sin esto se ve un parpadeo del login mientras /me responde.
  if (cargando) return <div className="pantalla-centrada tenue">Cargando...</div>

  // Sin sesión no hay router: cualquier URL muestra el login.
  if (!usuario) return <Login />

  // El administrador NO lleva finanzas propias: es el operador del servidor, no
  // un cliente. Su app es el panel, y las secciones de dinero solo aparecen
  // mientras está revisando la cuenta de alguien — y entonces son de esa
  // persona, no suyas.
  //
  // Las rutas no se esconden: directamente no existen. Así, cuando deja de
  // observar estando en /movimientos, el comodín lo devuelve al panel solo, sin
  // que quede un segundo mirando una pantalla vacía que ya no es de nadie.
  const conFinanzas = !esAdmin || verComo !== null
  const inicio = conFinanzas ? '/' : '/admin'

  return (
    <Layout>
      <Routes>
        {conFinanzas && (
          <>
            <Route path="/" element={<Dashboard />} />
            <Route path="/movimientos" element={<Movimientos />} />
            <Route path="/categorias" element={<Categorias />} />
            <Route path="/medios-pago" element={<MediosPago />} />
          </>
        )}
        {/* El panel. Solo existe para un admin: para el resto cae en el
            comodín de abajo, igual que cualquier URL inventada. */}
        {esAdmin && (
          <>
            <Route path="/admin" element={<Negocio />} />
            <Route path="/admin/clientes" element={<Clientes />} />
            <Route path="/admin/planes" element={<Planes />} />
            <Route path="/admin/errores" element={<Errores />} />
          </>
        )}
        {/* Cualquier otra ruta vuelve al inicio, que para cada quien es uno */}
        <Route path="*" element={<Navigate to={inicio} replace />} />
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
