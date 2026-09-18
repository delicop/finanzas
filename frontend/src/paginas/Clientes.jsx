import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { adminApi, negocioApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { useEsMovil } from '../lib/useEsMovil'
import { diasHasta, formatearFecha, formatearMonto } from '../lib/formato'
import MenuAcciones from '../componentes/MenuAcciones'
import Modal from '../componentes/Modal'

// Los clientes del servidor: quién existe, en qué plan está y qué puede hacer.
// Solo llega aquí un admin — y si alguien fuerza la URL, el backend responde
// 403 a cada petición, así que la pantalla queda vacía y sin datos.
export default function Clientes() {
  const [usuarios, setUsuarios] = useState([])
  const [planes, setPlanes] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [creando, setCreando] = useState(false)
  const [reseteando, setReseteando] = useState(null) // usuario o null
  const [eliminando, setEliminando] = useState(null) // usuario o null
  const [claveNueva, setClaveNueva] = useState(null) // la que acaba de generarse

  const { usuario: yo, observar } = useAuth()
  const esMovil = useEsMovil()
  const navegar = useNavigate()

  async function recargar() {
    setError('')
    try {
      // En paralelo: son dos peticiones independientes y la pantalla necesita
      // las dos para poder pintar el selector de plan de cada fila.
      const [lista, listaPlanes] = await Promise.all([
        adminApi.listarUsuarios(),
        negocioApi.listarPlanes(),
      ])
      setUsuarios(lista)
      setPlanes(listaPlanes)
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
  }, [])

  async function cambiar(usuario, cambios) {
    setError('')
    try {
      await adminApi.actualizarUsuario(usuario.id, cambios)
      await recargar()
    } catch (err) {
      // 409 cuando el cambio dejaría el servidor sin administradores.
      setError(err.message)
    }
  }

  function alternarActivo(u) {
    const accion = u.activo ? 'desactivar' : 'reactivar'
    if (!confirm(`¿Seguro que quieres ${accion} la cuenta de ${u.email}?`)) return
    cambiar(u, { activo: !u.activo })
  }

  async function asignarPlan(u, planID, ciclo) {
    setError('')
    try {
      await adminApi.asignarPlan(u.id, planID === '' ? null : Number(planID), ciclo)
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  function alternarRol(u) {
    const nuevo = u.rol === 'admin' ? 'usuario' : 'admin'
    const aviso =
      nuevo === 'admin'
        ? `${u.email} podrá crear usuarios y ver los datos de todos. ¿Continuar?`
        : `${u.email} dejará de administrar el servidor. ¿Continuar?`
    if (!confirm(aviso)) return
    cambiar(u, { rol: nuevo })
  }

  // Mirar los datos de otro: se guarda a quién y se salta al resumen, que es
  // donde de verdad se ve el estado de una cuenta.
  //
  // No se puede abrir la cuenta propia (ya la tienes) ni una desactivada: el
  // backend responde igual, pero una fila que no lleva a ningún lado no debe
  // comportarse como si llevara.
  function puedeAbrir(u) {
    return u.id !== yo.id && u.activo
  }

  function verDatos(u) {
    if (!puedeAbrir(u)) return
    observar({ id: u.id, email: u.email, nombre: u.nombre })
    navegar('/')
  }

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <span className="kicker">
            {usuarios.length} cuenta{usuarios.length === 1 ? '' : 's'}
          </span>
          <h1>Clientes</h1>
        </div>
        <button onClick={() => setCreando(true)}>Nuevo cliente</button>
      </div>

      {error && <div className="alerta">{error}</div>}

      {claveNueva && (
        <div className="alerta aviso">
          Contraseña de <strong>{claveNueva.email}</strong>:{' '}
          <code className="clave-generada">{claveNueva.password}</code>
          <br />
          Cópiala ahora y entrégasela. No se vuelve a mostrar: en la base solo
          queda su hash, ni el servidor puede leerla de vuelta.{' '}
          <button className="boton-enlace" onClick={() => setClaveNueva(null)}>
            Entendido
          </button>
        </div>
      )}

      <section className="tarjeta">
        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : esMovil ? (
          <div className="lista-movil">
            {usuarios.map((u) => (
              <article className="tarjeta-cat" key={u.id}>
                {/* En el teléfono lo clicable es el encabezado y no la tarjeta
                    entera: el pulgar cae en cualquier parte, y con toda la
                    tarjeta activa cerrar un selector abriría el cliente. */}
                <div
                  className={`tarjeta-cat-arriba ${puedeAbrir(u) ? 'fila-abrible' : ''}`}
                  onClick={() => verDatos(u)}
                >
                  <strong>
                    {u.nombre || u.email} {puedeAbrir(u) && <span className="flecha-abrir">›</span>}
                  </strong>
                  <EtiquetasUsuario usuario={u} yo={yo} />
                  {/* El menú va aquí arriba y no al final: si cuelga del pie,
                      la ficha vuelve a crecer y el pulgar tiene que bajar
                      hasta el fondo de cada una. */}
                  <AccionesUsuario
                    usuario={u}
                    yo={yo}
                    onResetear={() => setReseteando(u)}
                    onRol={() => alternarRol(u)}
                    onActivo={() => alternarActivo(u)}
                    onEliminar={() => setEliminando(u)}
                  />
                </div>
                <div className="tenue sub">
                  {u.email} · {u.movimientos} mov{u.movimientos === 1 ? '' : 's'}.
                </div>
                <div className="sub uso-movil">
                  <UltimaVez cuando={u.ultima_actividad} /> ·{' '}
                  <Frecuencia dias={u.dias_activos} />
                </div>
                <SelectorPlan
                  usuario={u}
                  planes={planes}
                  onCambiar={(plan, ciclo) => asignarPlan(u, plan, ciclo)}
                />
              </article>
            ))}
          </div>
        ) : (
          /* Sin .tabla-scroll a propósito: el menú "⋯" se posiciona en
             absoluto y cualquier overflow por encima lo recortaría. Ahora que
             las acciones ocupan una columna de 40px, la tabla cabe. */
          <div>
            <table className="tabla-clientes">
              <thead>
                <tr>
                  <th>Usuario</th>
                  <th>Correo</th>
                  <th></th>
                  <th>Plan</th>
                  <th className="num">Movimientos</th>
                  <th>Última vez</th>
                  <th className="num">Frecuencia</th>
                  <th>Desde</th>
                  <th className="acciones"></th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {usuarios.map((u) => (
                  // La fila entera abre la cuenta del cliente. Los controles
                  // que viven dentro (el selector de plan, los botones) paran
                  // el evento: si no, cambiarle el plan a alguien te sacaría
                  // de la lista de golpe.
                  <tr
                    key={u.id}
                    className={`${u.activo ? '' : 'fila-inactiva'} ${
                      puedeAbrir(u) ? 'fila-abrible' : ''
                    }`}
                    onClick={() => verDatos(u)}
                    title={puedeAbrir(u) ? `Ver las cuentas de ${u.email}` : undefined}
                  >
                    <td>{u.nombre || <span className="tenue">sin nombre</span>}</td>
                    <td className="tenue">{u.email}</td>
                    <td>
                      <EtiquetasUsuario usuario={u} yo={yo} />
                    </td>
                    <td onClick={(e) => e.stopPropagation()}>
                      <SelectorPlan
                        usuario={u}
                        planes={planes}
                        onCambiar={(plan, ciclo) => asignarPlan(u, plan, ciclo)}
                      />
                    </td>
                    <td className="num tenue">{u.movimientos}</td>
                    <td className="nowrap">
                      <UltimaVez cuando={u.ultima_actividad} />
                    </td>
                    <td className="num">
                      <Frecuencia dias={u.dias_activos} />
                    </td>
                    <td className="tenue nowrap">{formatearFecha(u.creado_en.slice(0, 10))}</td>
                    <td className="acciones" onClick={(e) => e.stopPropagation()}>
                      <AccionesUsuario
                        usuario={u}
                        yo={yo}
                        onResetear={() => setReseteando(u)}
                        onRol={() => alternarRol(u)}
                        onActivo={() => alternarActivo(u)}
                        onEliminar={() => setEliminando(u)}
                      />
                    </td>
                    {/* La flecha es lo que avisa de que la fila se puede
                        abrir: sin ella, que sea clicable no se adivina. */}
                    <td className="flecha-abrir">{puedeAbrir(u) && '›'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {creando && (
        <FormularioUsuario
          onCerrar={() => setCreando(false)}
          onCreado={async (creado, password) => {
            setCreando(false)
            setClaveNueva({ email: creado.email, password })
            await recargar()
          }}
        />
      )}

      {eliminando && (
        <ConfirmarBorrado
          usuario={eliminando}
          onCerrar={() => setEliminando(null)}
          onBorrado={async () => {
            setEliminando(null)
            await recargar()
          }}
        />
      )}

      {reseteando && (
        <FormularioReset
          usuario={reseteando}
          onCerrar={() => setReseteando(null)}
          onListo={(password) => {
            setReseteando(null)
            setClaveNueva({ email: reseteando.email, password })
          }}
        />
      )}
    </>
  )
}

// Cuánto hace que no aparece este cliente.
//
// El panel ya decía cuántos movimientos tiene cada uno, pero eso no contesta
// si TODAVÍA lo usa: mil movimientos de hace ocho meses y mil de esta semana
// se veían exactamente igual. Para cobrar —o para llamar a alguien antes de
// que se vaya en silencio— lo que importa es esta columna.
function UltimaVez({ cuando }) {
  if (!cuando) return <span className="tenue">sin registro</span>

  const dias = diasHasta(cuando.slice(0, 10))
  // diasHasta cuenta hacia adelante; hacia atrás viene en negativo.
  const hace = -dias

  let texto
  if (hace <= 0) texto = 'hoy'
  else if (hace === 1) texto = 'ayer'
  else if (hace < 7) texto = `hace ${hace} días`
  else if (hace < 30) texto = `hace ${Math.floor(hace / 7)} sem.`
  else if (hace < 365) texto = `hace ${Math.floor(hace / 30)} mes${hace < 60 ? '' : 'es'}`
  else texto = formatearFecha(cuando.slice(0, 10))

  // El color es el aviso: verde esta semana, ámbar el mes, rojo más de un mes
  // sin volver. Es lo que se mira de un vistazo cuando la lista es larga.
  const tono = hace < 7 ? 'positivo' : hace < 30 ? 'advertencia' : 'negativo'
  return (
    <span className={tono} title={formatearFecha(cuando.slice(0, 10))}>
      {texto}
    </span>
  )
}

// Con qué frecuencia lo usa: en cuántos días distintos de los últimos 30 hizo
// algo. Dos cuentas pueden tener la misma "última vez" y ser cosas muy
// distintas — una entró ayer por primera vez en el mes, la otra entra a diario.
function Frecuencia({ dias }) {
  const de = 30
  if (!dias) return <span className="tenue">—</span>

  const tono = dias >= 15 ? 'positivo' : dias >= 5 ? 'advertencia' : 'tenue'
  return (
    <span className={tono} title={`Usó la app ${dias} de los últimos ${de} días`}>
      {dias}/{de}
    </span>
  )
}

function EtiquetasUsuario({ usuario, yo }) {
  return (
    <span className="etiquetas-usuario">
      {usuario.rol === 'admin' && <span className="etiqueta tipo-preste">Admin</span>}
      {!usuario.activo && <span className="etiqueta tipo-pague">Desactivado</span>}
      {usuario.id === yo.id && <span className="etiqueta tipo-recibi">Tú</span>}
    </span>
  )
}

function AccionesUsuario({ usuario, yo, onResetear, onRol, onActivo, onEliminar }) {
  // Sobre uno mismo no se ofrecen ni "ver datos" (ya los estás viendo) ni
  // desactivar o degradarse: el backend lo rechaza igual, pero un botón que
  // siempre falla es un botón que no debería existir.
  const soyYo = usuario.id === yo.id

  return (
    // Detrás de un "⋯" y no a la vista: son cuatro y no caben. Sueltas, cada
    // fila de la tabla crecía a cuatro renglones, y en el teléfono la ficha
    // quedaba más alta que la pantalla.
    <MenuAcciones etiqueta={`Acciones de ${usuario.nombre || usuario.email}`}>
      <button type="button" role="menuitem" onClick={onResetear}>
        Resetear clave
      </button>
      {!soyYo && (
        <>
          <button type="button" role="menuitem" onClick={onRol}>
            {usuario.rol === 'admin' ? 'Quitar admin' : 'Hacer admin'}
          </button>
          <button type="button" role="menuitem" onClick={onActivo}>
            {usuario.activo ? 'Desactivar' : 'Reactivar'}
          </button>
          <button type="button" role="menuitem" className="peligro" onClick={onEliminar}>
            Eliminar
          </button>
        </>
      )}
    </MenuAcciones>
  )
}

// generarClave arma una contraseña aleatoria con el generador criptográfico del
// navegador (no Math.random, que es predecible). El admin la copia una vez y se
// la entrega; después esa persona la cambia desde la app.
function generarClave() {
  const alfabeto = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
  const bytes = crypto.getRandomValues(new Uint32Array(14))
  return Array.from(bytes, (n) => alfabeto[n % alfabeto.length]).join('')
}

function FormularioUsuario({ onCerrar, onCreado }) {
  const [email, setEmail] = useState('')
  const [nombre, setNombre] = useState('')
  const [password, setPassword] = useState(generarClave)
  const [rol, setRol] = useState('usuario')
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      const creado = await adminApi.crearUsuario({ email, nombre, password, rol })
      await onCreado(creado, password)
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo="Nuevo cliente" onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="email">
          Correo <span className="req">*</span>
        </label>
        <input
          id="email"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="persona@correo.com"
          autoFocus
        />
        {campos.email && <span className="error-campo">{campos.email}</span>}

        <label htmlFor="nombre">Nombre</label>
        <input
          id="nombre"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
          placeholder="Cómo se llama"
        />
        {campos.nombre && <span className="error-campo">{campos.nombre}</span>}

        <label htmlFor="password">
          Contraseña <span className="req">*</span>
        </label>
        <div className="fila-clave">
          <input
            id="password"
            className="crece"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <button type="button" className="secundario" onClick={() => setPassword(generarClave())}>
            Generar otra
          </button>
        </div>
        {campos.password && <span className="error-campo">{campos.password}</span>}

        <label htmlFor="rol">Rol</label>
        <select id="rol" value={rol} onChange={(e) => setRol(e.target.value)}>
          <option value="usuario">Usuario — solo sus propias cuentas</option>
          <option value="admin">Administrador — gestiona el servidor</option>
        </select>
        {campos.rol && <span className="error-campo">{campos.rol}</span>}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {guardando ? 'Creando...' : 'Crear cliente'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function FormularioReset({ usuario, onCerrar, onListo }) {
  const [password, setPassword] = useState(generarClave)
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      await adminApi.resetearPassword(usuario.id, password)
      onListo(password)
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={`Resetear la clave de ${usuario.email}`} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <div className="alerta aviso">
          Vas a conocer la contraseña de otra persona. Pídele que la cambie
          desde la app apenas entre.
        </div>

        <label htmlFor="nueva">Contraseña nueva</label>
        <div className="fila-clave">
          <input
            id="nueva"
            className="crece"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoFocus
          />
          <button type="button" className="secundario" onClick={() => setPassword(generarClave())}>
            Generar otra
          </button>
        </div>
        {campos.nueva && <span className="error-campo">{campos.nueva}</span>}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {guardando ? 'Guardando...' : 'Resetear'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

// ConfirmarBorrado no es un confirm() normal a propósito. Borrar una cuenta se
// lleva su historial de plata completo y no hay papelera: obligar a escribir el
// correo hace imposible confundirse de fila, que es el error probable cuando la
// lista tiene veinte clientes parecidos.
function ConfirmarBorrado({ usuario, onCerrar, onBorrado }) {
  const [confirmacion, setConfirmacion] = useState('')
  const [error, setError] = useState('')
  const [borrando, setBorrando] = useState(false)

  const coincide = confirmacion.trim().toLowerCase() === usuario.email.toLowerCase()

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setBorrando(true)
    try {
      await adminApi.eliminarUsuario(usuario.id, usuario.email)
      await onBorrado()
    } catch (err) {
      setError(err.message)
    } finally {
      setBorrando(false)
    }
  }

  return (
    <Modal titulo="Eliminar la cuenta" onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <div className="alerta">
          Vas a borrar <strong>{usuario.email}</strong> y con ella{' '}
          <strong>
            {usuario.movimientos} movimiento{usuario.movimientos === 1 ? '' : 's'}
          </strong>
          , sus categorías, sus medios de pago y sus facturas del disco. Esto{' '}
          <strong>no se puede deshacer</strong>.
          <br />
          <br />
          Si solo quieres cortarle el acceso, cierra esto y usa{' '}
          <strong>Desactivar</strong>: le quita la entrada de inmediato y deja el
          historial intacto.
        </div>

        <label htmlFor="confirmacion">Escribe {usuario.email} para confirmar</label>
        <input
          id="confirmacion"
          value={confirmacion}
          onChange={(e) => setConfirmacion(e.target.value)}
          autoComplete="off"
          autoFocus
        />

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" className="peligro" disabled={!coincide || borrando}>
            {borrando ? 'Borrando...' : 'Eliminar para siempre'}
          </button>
        </div>
      </form>
    </Modal>
  )
}


// SelectorPlan son dos <select> y no un modal: cambiar de plan es una acción de
// un solo dato que se hace seguido, y abrir una ventana para elegir una opción
// de una lista corta es fricción sin ninguna ganancia.
//
// El segundo selector (mensual/anual) solo aparece si el plan se vende por
// año. Al cambiar a un plan sin precio anual, el ciclo vuelve a mensual solo:
// el backend rechazaría un anual que el plan no ofrece.
//
// A un administrador no se le asigna plan: no te cobras a ti mismo.
function SelectorPlan({ usuario, planes, onCambiar }) {
  if (usuario.rol === 'admin') return <span className="tenue">—</span>

  // Un plan desactivado sigue apareciendo si es el que este cliente ya tiene:
  // si no, el select mostraría vacío y el primer cambio accidental le quitaría
  // el plan sin que nadie lo pidiera.
  const visibles = planes.filter((p) => p.activo || p.id === usuario.plan_id)
  const actual = planes.find((p) => p.id === usuario.plan_id)
  const ciclo = usuario.ciclo || 'mensual'

  function cambiarPlan(valor) {
    const elegido = planes.find((p) => String(p.id) === valor)
    // Se conserva el anual solo si el plan nuevo también lo ofrece.
    const nuevoCiclo = elegido?.precio_anual && ciclo === 'anual' ? 'anual' : 'mensual'
    onCambiar(valor, nuevoCiclo)
  }

  return (
    <div className="selector-plan-grupo">
      <select
        className="selector-plan"
        value={usuario.plan_id ?? ''}
        onChange={(e) => cambiarPlan(e.target.value)}
        aria-label={`Plan de ${usuario.email}`}
      >
        <option value="">Sin plan</option>
        {visibles.map((p) => (
          <option key={p.id} value={p.id}>
            {p.nombre}
            {p.incluye_ia ? ' ✦' : ''}
            {p.activo ? '' : ' (inactivo)'}
          </option>
        ))}
      </select>

      {actual?.precio_anual && (
        <select
          className="selector-plan selector-ciclo"
          value={ciclo}
          onChange={(e) => onCambiar(String(actual.id), e.target.value)}
          aria-label={`Cómo paga ${usuario.email}`}
        >
          <option value="mensual">Mensual · {formatearMonto(actual.precio_mensual)}</option>
          <option value="anual">Anual · {formatearMonto(actual.precio_anual)}</option>
        </select>
      )}

      {actual && !actual.precio_anual && (
        <span className="tenue precio-plan">{formatearMonto(actual.precio_mensual)}/mes</span>
      )}
    </div>
  )
}
