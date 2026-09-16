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

// Las secciones del administrador. Son OTRA app: el dueño del servidor no
// lleva gastos personales, lleva el negocio — a quién le cobra, cuánto y quién
// ya pagó. Ocultarlas a los demás es comodidad, no seguridad: quien escriba
// /admin a mano llega a una pantalla que el backend deja vacía a punta de 403.
const SECCIONES_ADMIN = [
  { a: '/admin', etiqueta: 'Negocio', icono: '◈', exacta: true },
  { a: '/admin/clientes', etiqueta: 'Clientes', icono: '☰' },
  { a: '/admin/planes', etiqueta: 'Planes', icono: '▤' },
  { a: '/admin/errores', etiqueta: 'Errores', icono: '⚠' },
]

export default function Layout({ children }) {
  const { usuario, logout, esAdmin, verComo, dejarDeObservar } = useAuth()
  const { tema, alternar } = useTema()
  const esMovil = useEsMovil()
  const [cambiandoPassword, setCambiandoPassword] = useState(false)

  // El menú tiene que decir lo mismo que las rutas (ver App.jsx): un admin no
  // tiene finanzas propias, así que solo ve el panel. Mientras observa a otro
  // sí aparecen las secciones de dinero, porque son las de esa persona.
  const secciones = esAdmin
    ? verComo
      ? // Observando: las secciones de dinero son las del cliente observado, y
        // "Negocio" es la salida de vuelta al panel.
        [...SECCIONES, SECCIONES_ADMIN[0]]
      : SECCIONES_ADMIN
    : SECCIONES

  const hayMenu = secciones.length > 1

  return (
    <div className={`app ${esMovil && hayMenu ? 'con-barra-inferior' : ''}`}>
      <header className="barra">
        <div className="barra-izq">
          <strong className="marca">Finanzas</strong>

          {/* En escritorio el menú va arriba. En el teléfono se va abajo,
              donde llega el pulgar, y así no compite por espacio con los
              botones de tema, contraseña y salir: con cuatro secciones
              arriba quedaban dos escondidas. */}
          {!esMovil && hayMenu && (
            <nav className="nav">
              {/* NavLink agrega la clase "activo" solo en la ruta actual */}
              {secciones.map((s) => (
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

      <main className="contenido">
        {/* Mientras el admin mira una cuenta ajena, la app entera muestra
            datos que no son suyos. Sin un aviso permanente y difícil de
            ignorar, es cuestión de tiempo que alguien lea el saldo de otro
            creyendo que es el propio. */}
        {verComo && (
          <div className="alerta aviso banner-observando">
            <span>
              Estás viendo los datos de <strong>{verComo.nombre || verComo.email}</strong>. Es solo
              lectura: no puedes crear ni modificar nada suyo.
            </span>
            {/* "Volver al panel" y no "a lo mío": quien observa es siempre un
                administrador, y un administrador no tiene cuentas propias a
                las que volver. */}
            <button className="secundario" onClick={dejarDeObservar}>
              Volver al panel
            </button>
          </div>
        )}
        {children}
      </main>

      {esMovil && hayMenu && (
        <nav className="barra-inferior">
          {secciones.map((s) => (
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
