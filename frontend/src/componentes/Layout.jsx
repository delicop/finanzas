import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { useAuth } from '../lib/AuthContext'
import { useTema } from '../lib/TemaContext'
import { useEsMovil } from '../lib/useEsMovil'
import ModalPassword from './ModalPassword'

const SECCIONES = [
  { a: '/', etiqueta: 'Resumen', icono: '◉', exacta: true },
  { a: '/movimientos', etiqueta: 'Movimientos', icono: '⇅' },
  { a: '/categorias', etiqueta: 'Categorías', icono: '◫' },
  { a: '/medios-pago', etiqueta: 'Medios', icono: '▤' },
]

export default function Layout({ children }) {
  const { usuario, logout } = useAuth()
  const { tema, alternar } = useTema()
  const esMovil = useEsMovil()
  const [cambiandoPassword, setCambiandoPassword] = useState(false)

  return (
    <div className={`app ${esMovil ? 'con-barra-inferior' : ''}`}>
      <header className="barra">
        <div className="barra-izq">
          <strong className="marca">Finanzas</strong>

          {/* En escritorio el menú va arriba. En el teléfono se va abajo,
              donde llega el pulgar, y así no compite por espacio con los
              botones de tema, contraseña y salir: con cuatro secciones
              arriba quedaban dos escondidas. */}
          {!esMovil && (
            <nav className="nav">
              {/* NavLink agrega la clase "activo" solo en la ruta actual */}
              {SECCIONES.map((s) => (
                <NavLink
                  key={s.a}
                  to={s.a}
                  end={s.exacta}
                  className={({ isActive }) => (isActive ? 'activo' : '')}
                >
                  {s.etiqueta}
                </NavLink>
              ))}
            </nav>
          )}
        </div>

        <div className="barra-der">
          <span className="usuario">{usuario.nombre || usuario.email}</span>

          <button
            className="boton-tema"
            onClick={alternar}
            title={tema === 'claro' ? 'Cambiar a modo oscuro' : 'Cambiar a modo claro'}
            aria-label={tema === 'claro' ? 'Cambiar a modo oscuro' : 'Cambiar a modo claro'}
          >
            {tema === 'claro' ? '🌙' : '☀️'}
          </button>

          <button
            className="boton-tema"
            onClick={() => setCambiandoPassword(true)}
            title="Cambiar contraseña"
            aria-label="Cambiar contraseña"
          >
            🔑
          </button>

          <button className="secundario" onClick={logout}>
            Salir
          </button>
        </div>
      </header>

      <main className="contenido">{children}</main>

      {esMovil && (
        <nav className="barra-inferior">
          {SECCIONES.map((s) => (
            <NavLink
              key={s.a}
              to={s.a}
              end={s.exacta}
              className={({ isActive }) => (isActive ? 'activo' : '')}
            >
              <span className="icono-tab" aria-hidden="true">
                {s.icono}
              </span>
              {s.etiqueta}
            </NavLink>
          ))}
        </nav>
      )}

      {cambiandoPassword && <ModalPassword onCerrar={() => setCambiandoPassword(false)} />}
    </div>
  )
}
