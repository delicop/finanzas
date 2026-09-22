import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { notificacionesApi } from '../lib/api'
import { useEnLinea } from '../lib/pwa'
import { useAuth } from '../lib/AuthContext'
import { useTema } from '../lib/TemaContext'
import { useEsMovil } from '../lib/useEsMovil'
import BurbujaAsistente from './BurbujaAsistente'
import AvisosCelular from './AvisosCelular'
import InstalarApp, { AvisoSinConexion } from './InstalarApp'
import ModalPassword from './ModalPassword'
import Notificaciones from './Notificaciones'
import Tutorial, { marcarTutorialVisto, tutorialVisto } from './Tutorial'
import { useAlAbrirTutorial } from '../lib/eventos'
import Icono from './Icono'

const SECCIONES = [
  { a: '/', etiqueta: 'Resumen', icono: 'resumen', exacta: true },
  { a: '/movimientos', etiqueta: 'Movimientos', icono: 'movimientos' },
  { a: '/recurrentes', etiqueta: 'Se repiten', icono: 'recurrentes' },
  { a: '/categorias', etiqueta: 'Categorías', icono: 'categorias' },
  { a: '/medios-pago', etiqueta: 'Medios', icono: 'medios' },
]

// Las secciones del administrador. Son OTRA app: el dueño del servidor no
// lleva gastos personales, lleva el negocio — a quién le cobra, cuánto y quién
// ya pagó. Ocultarlas a los demás es comodidad, no seguridad: quien escriba
// /admin a mano llega a una pantalla que el backend deja vacía a punta de 403.
const SECCIONES_ADMIN = [
  { a: '/admin', etiqueta: 'Negocio', icono: 'negocio', exacta: true },
  { a: '/admin/clientes', etiqueta: 'Clientes', icono: 'clientes' },
  { a: '/admin/planes', etiqueta: 'Planes', icono: 'planes' },
  { a: '/admin/errores', etiqueta: 'Errores', icono: 'errores' },
]

export default function Layout({ children }) {
  const { usuario, logout, esAdmin, verComo, dejarDeObservar, conIA } = useAuth()
  const { tema, alternar } = useTema()
  const esMovil = useEsMovil()
  const enLinea = useEnLinea()
  const [cambiandoPassword, setCambiandoPassword] = useState(false)
  const [viendoAvisos, setViendoAvisos] = useState(false)
  const [sinLeer, setSinLeer] = useState(0)

  // La guía solo es para quien lleva finanzas propias: el admin sin observar
  // no tiene categorías ni medios, y observando no puede crear nada.
  const conGuia = !esAdmin && !verComo
  // La primera vez que alguien entra, la guía se abre sola. Después, con el
  // botón de ayuda o desde los primeros pasos del Resumen.
  const [viendoTutorial, setViendoTutorial] = useState(
    () => conGuia && !tutorialVisto(usuario.id),
  )
  useAlAbrirTutorial(() => setViendoTutorial(true))

  function cerrarTutorial() {
    marcarTutorialVisto(usuario.id)
    setViendoTutorial(false)
  }

  // El contador de la campana. Se consulta al entrar y cada cinco minutos:
  // los avisos los genera una tarea del servidor cada varias horas, así que
  // preguntar más seguido sería gastar peticiones para nada.
  //
  // Mientras se observa la cuenta de un cliente no se pregunta: sus avisos son
  // suyos y el backend responde 403, igual que con el chat del asistente.
  useEffect(() => {
    if (verComo) {
      setSinLeer(0)
      return
    }

    const control = new AbortController()
    const consultar = () =>
      notificacionesApi
        .listar(control.signal)
        .then((datos) => setSinLeer(datos.sin_leer))
        // Un fallo aquí no puede romper la barra entera: la app sirve
        // perfectamente sin el contador.
        .catch(() => {})

    consultar()
    const cadaRato = setInterval(consultar, 5 * 60 * 1000)

    return () => {
      control.abort()
      clearInterval(cadaRato)
    }
  }, [verComo])

  // El menú tiene que decir lo mismo que las rutas (ver App.jsx): un admin no
  // tiene finanzas propias, así que solo ve el panel. Mientras observa a otro
  // sí aparecen las secciones de dinero, porque son las de esa persona.
  const secciones = esAdmin
    ? verComo
      ? // Observando: las secciones de dinero son las del cliente observado, y
        // "Negocio" es la salida de vuelta al panel. La burbuja del asistente
        // no aparece: el chat de una persona no es un dato que el panel
        // revise, y el backend responde 403 si se intenta.
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
                  <Icono nombre={s.icono} tamano={16} />
                  {s.etiqueta}
                </NavLink>
              ))}
            </nav>
          )}
        </div>

        <div className="barra-der">
          <span className="usuario">{usuario.nombre || usuario.email}</span>

          <InstalarApp />

          {/* En el teléfono la barra ya no tiene espacio (con instalar y avisos
              al celular tapaba la marca): ahí el botón va junto al título del
              Resumen. */}
          {conGuia && !esMovil && (
            <button
              className="boton-tema"
              onClick={() => setViendoTutorial(true)}
              title="Cómo funciona"
              aria-label="Cómo funciona"
            >
              <Icono nombre="ayuda" tamano={18} />
            </button>
          )}

          <button
            className="boton-tema"
            onClick={alternar}
            title={tema === 'claro' ? 'Cambiar a modo oscuro' : 'Cambiar a modo claro'}
            aria-label={tema === 'claro' ? 'Cambiar a modo oscuro' : 'Cambiar a modo claro'}
          >
            <Icono nombre={tema === 'claro' ? 'luna' : 'sol'} tamano={18} />
          </button>

          {/* Los avisos al celular van pegados a la campana: uno es "lo que
              pasó" y el otro "avísame aunque no esté mirando". Mientras se
              observa otra cuenta no aparece: esos avisos son de esa persona. */}
          {!verComo && <AvisosCelular />}

          {!verComo && (
            <button
              className="boton-tema campana"
              onClick={() => setViendoAvisos(true)}
              title="Avisos"
              aria-label={sinLeer > 0 ? `Avisos (${sinLeer} sin leer)` : 'Avisos'}
            >
              <Icono nombre="campana" tamano={18} />
              {sinLeer > 0 && <span className="campana-punto">{sinLeer > 9 ? '9+' : sinLeer}</span>}
            </button>
          )}

          <button
            className="boton-tema"
            onClick={() => setCambiandoPassword(true)}
            title="Cambiar contraseña"
            aria-label="Cambiar contraseña"
          >
            <Icono nombre="llave" tamano={18} />
          </button>

          <button className="secundario boton-salir" onClick={logout} title="Salir" aria-label="Salir">
            <Icono nombre="salir" tamano={16} />
            {/* En el teléfono la barra ya lleva cinco botones: ahí basta la
                puerta, y el rótulo vuelve en escritorio. */}
            {!esMovil && 'Salir'}
          </button>
        </div>
      </header>

      <main className="contenido">
        <AvisoSinConexion enLinea={enLinea} />
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
              <Icono nombre={s.icono} tamano={22} className="icono-tab" />
              <span className="rotulo-tab">{s.etiqueta}</span>
            </NavLink>
          ))}
        </nav>
      )}

      {/* El asistente no es una sección: es una burbuja encima de todas.
          Solo si el plan lo incluye (conIA ya es falso mirando otra cuenta). */}
      {conIA && <BurbujaAsistente />}

      {viendoTutorial && conGuia && <Tutorial conIA={conIA} onCerrar={cerrarTutorial} />}

      {cambiandoPassword && <ModalPassword onCerrar={() => setCambiandoPassword(false)} />}

      {viendoAvisos && (
        <Notificaciones onCerrar={() => setViendoAvisos(false)} onLeidos={() => setSinLeer(0)} />
      )}
    </div>
  )
}
