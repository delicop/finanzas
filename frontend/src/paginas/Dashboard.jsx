import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { categoriasApi, dashboardApi, mediosApi } from '../lib/api'
import { formatearMonto } from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import { useAlGuardarMovimiento } from '../lib/eventos'
import { useAuth } from '../lib/AuthContext'
import MovimientoForm from '../componentes/MovimientoForm'

export default function Dashboard() {
  const [resumen, setResumen] = useState(null)
  const [error, setError] = useState('')
  const [cargando, setCargando] = useState(true)
  const esMovil = useEsMovil()
  const { soloLectura } = useAuth()

  // Para el formulario de registro rápido: necesita las dos listas completas.
  // Van con otro nombre para no chocar con las del resumen, que son cifras
  // agregadas, no catálogos.
  const [listaCategorias, setListaCategorias] = useState([])
  const [listaMedios, setListaMedios] = useState([])
  const [registrando, setRegistrando] = useState(false)

  function cargarResumen() {
    return dashboardApi
      .resumen()
      .then(setResumen)
      .catch((err) => setError(err.message))
      .finally(() => setCargando(false))
  }

  // Si el asistente guarda algo con el resumen abierto debajo, las cifras se
  // actualizan solas.
  useAlGuardarMovimiento(cargarResumen)

  useEffect(() => {
    cargarResumen()
    categoriasApi.listar().then(setListaCategorias).catch(() => {})
    mediosApi.listar().then(setListaMedios).catch(() => {})
  }, [])

  if (cargando) return <p className="tenue">Cargando resumen...</p>
  if (error) return <div className="alerta">{error}</div>
  if (!resumen) return null

  const { totales, categorias, medios, deudores } = resumen

  const listo = listaCategorias.length > 0 && listaMedios.length > 0

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Resumen</h1>
        {/* Registro rápido con el formulario. El asistente no va aquí: es la
            burbuja flotante, que está en todas las pantallas. */}
        {!soloLectura && (
          <button onClick={() => setRegistrando(true)} disabled={!listo}>
            Nuevo movimiento
          </button>
        )}
      </div>

      {!listo && !cargando && (
        <div className="alerta aviso">
          Para registrar movimientos necesitas al menos una{' '}
          <Link to="/categorias">categoría</Link> y un{' '}
          <Link to="/medios-pago">medio de pago</Link>.
        </div>
      )}

      <div className="fila-tarjetas">
        <Metrica titulo="Recibido" monto={totales.recibido} clase="positivo" />
        <Metrica titulo="Pagado" monto={totales.pagado} clase="negativo" />
        <Metrica
          titulo="Recuperado"
          monto={totales.recuperado}
          clase="positivo"
          nota={`${resumen.prestamos_cobrados} préstamo${resumen.prestamos_cobrados === 1 ? '' : 's'} devuelto${resumen.prestamos_cobrados === 1 ? '' : 's'}`}
        />
        <Metrica
          titulo="Balance"
          monto={totales.balance}
          clase={Number(totales.balance) >= 0 ? 'positivo' : 'negativo'}
          nota="Recibido − Pagado − Por cobrar"
        />
      </div>

      <section className="tarjeta">
        <h2>¿Dónde está la plata?</h2>
        <p className="subtitulo">Cuánto tienes en cada medio de pago</p>

        {medios.length === 0 ? (
          <p className="tenue">
            Todavía no tienes medios de pago. <Link to="/medios-pago">Crea el primero</Link>.
          </p>
        ) : esMovil ? (
          <div className="lista-movil">
            {medios.map((m) => (
              <TarjetaMedio key={m.medio_id ?? 'sin'} m={m} />
            ))}
          </div>
        ) : (
          <div className="tabla-scroll">
            <table>
              <thead>
                <tr>
                  <th>Medio</th>
                  <th className="num">Recibí</th>
                  <th className="num">Pagué</th>
                  <th className="num">Tengo</th>
                  <th className="num">Movs.</th>
                </tr>
              </thead>
              <tbody>
                {medios.map((m) => (
                  <tr key={m.medio_id ?? 'sin'}>
                    <td>
                      {m.medio_id ? (
                        <Link to={`/movimientos?medio_pago_id=${m.medio_id}`}>{m.nombre}</Link>
                      ) : (
                        // Los movimientos sin medio registrado. Se muestran para
                        // que los saldos sumen el balance general y todo cuadre.
                        <span className="tenue">{m.nombre}</span>
                      )}
                    </td>
                    <td className="num positivo">{formatearMonto(m.recibido)}</td>
                    <td className="num negativo">{formatearMonto(m.pagado)}</td>
                    <td className={`num saldo-fuerte ${Number(m.saldo) >= 0 ? 'positivo' : 'negativo'}`}>
                      {formatearMonto(m.saldo)}
                    </td>
                    <td className="num tenue">{m.movimientos}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="tarjeta destacada">
        <h2>Te deben</h2>
        <p className="monto-grande">{formatearMonto(resumen.prestado_pendiente)}</p>
        <p className="tenue">
          {resumen.prestamos_pendientes === 0
            ? 'No tienes préstamos pendientes'
            : `${resumen.prestamos_pendientes} préstamo${resumen.prestamos_pendientes === 1 ? '' : 's'} sin cobrar`}
        </p>

        {deudores.length > 0 && (
          <ul className="lista-deudores">
            {deudores.map((d) => (
              <li key={d.a_quien}>
                <Link to={`/movimientos?tipo=preste&estado=pendiente&q=${encodeURIComponent(d.a_quien)}`}>
                  {d.a_quien}
                </Link>
                <span className="monto">{formatearMonto(d.total)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="tarjeta">
        <h2>Por categoría</h2>

        {categorias.length === 0 ? (
          <p className="tenue">
            Todavía no tienes categorías. <Link to="/categorias">Crea la primera</Link>.
          </p>
        ) : esMovil ? (
          <div className="lista-movil">
            {categorias.map((c) => (
              <TarjetaCategoria key={c.categoria_id} c={c} />
            ))}
          </div>
        ) : (
          <div className="tabla-scroll">
            <table>
              <thead>
                <tr>
                  <th>Categoría</th>
                  <th className="num">Recibí</th>
                  <th className="num">Pagué</th>
                  <th className="num">Por cobrar</th>
                  <th className="num">Balance</th>
                  <th className="num">Movs.</th>
                </tr>
              </thead>
              <tbody>
                {categorias.map((c) => (
                  <tr key={c.categoria_id}>
                    <td>
                      <Link to={`/movimientos?categoria_id=${c.categoria_id}`}>{c.nombre}</Link>
                    </td>
                    <td className="num positivo">{formatearMonto(c.recibido)}</td>
                    <td className="num negativo">{formatearMonto(c.pagado)}</td>
                    <td className="num advertencia">{formatearMonto(c.por_cobrar)}</td>
                    <td className={`num ${Number(c.balance) >= 0 ? 'positivo' : 'negativo'}`}>
                      {formatearMonto(c.balance)}
                    </td>
                    <td className="num tenue">{c.movimientos}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {registrando && (
        <MovimientoForm
          movimiento={{}}
          categorias={listaCategorias}
          medios={listaMedios}
          onCerrar={() => setRegistrando(false)}
          onGuardado={async () => {
            setRegistrando(false)
            await cargarResumen()
          }}
        />
      )}

    </>
  )
}

function Metrica({ titulo, monto, clase = '', nota }) {
  return (
    <div className="tarjeta metrica">
      <span className="tenue">{titulo}</span>
      <strong className={clase}>{formatearMonto(monto)}</strong>
      {nota && <span className="metrica-nota">{nota}</span>}
    </div>
  )
}

function TarjetaMedio({ m }) {
  return (
    <article className="tarjeta-cat">
      <div className="tarjeta-cat-arriba">
        {m.medio_id ? (
          <Link to={`/movimientos?medio_pago_id=${m.medio_id}`}>{m.nombre}</Link>
        ) : (
          <span className="tenue">{m.nombre}</span>
        )}
        <strong className={`saldo-fuerte ${Number(m.saldo) >= 0 ? 'positivo' : 'negativo'}`}>
          {formatearMonto(m.saldo)}
        </strong>
      </div>
      <dl className="tarjeta-cat-datos dos">
        <div>
          <dt>Recibí</dt>
          <dd className="positivo">{formatearMonto(m.recibido)}</dd>
        </div>
        <div>
          <dt>Pagué</dt>
          <dd className="negativo">{formatearMonto(m.pagado)}</dd>
        </div>
      </dl>
    </article>
  )
}

function TarjetaCategoria({ c }) {
  return (
    <article className="tarjeta-cat">
      <div className="tarjeta-cat-arriba">
        <Link to={`/movimientos?categoria_id=${c.categoria_id}`}>{c.nombre}</Link>
        <strong className={Number(c.balance) >= 0 ? 'positivo' : 'negativo'}>
          {formatearMonto(c.balance)}
        </strong>
      </div>
      <dl className="tarjeta-cat-datos">
        <div>
          <dt>Recibí</dt>
          <dd className="positivo">{formatearMonto(c.recibido)}</dd>
        </div>
        <div>
          <dt>Pagué</dt>
          <dd className="negativo">{formatearMonto(c.pagado)}</dd>
        </div>
        <div>
          <dt>Por cobrar</dt>
          <dd className="advertencia">{formatearMonto(c.por_cobrar)}</dd>
        </div>
      </dl>
    </article>
  )
}
