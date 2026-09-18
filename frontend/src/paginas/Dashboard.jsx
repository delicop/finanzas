import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { categoriasApi, dashboardApi, mediosApi } from '../lib/api'
import { formatearMonto } from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import { useAlGuardarMovimiento } from '../lib/eventos'
import { useAuth } from '../lib/AuthContext'
import MovimientoForm from '../componentes/MovimientoForm'
import RecurrentesPendientes from '../componentes/RecurrentesPendientes'
import FechaCobro from '../componentes/FechaCobro'

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

  const { totales, categorias, medios, contrapartes } = resumen

  const listo = listaCategorias.length > 0 && listaMedios.length > 0

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <span className="kicker">{mesEnPalabras()}</span>
          <h1>Resumen</h1>
        </div>
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

      {/* Lo que toca confirmar va arriba de todo: es lo único de esta
          pantalla que pide una acción hoy. */}
      {!soloLectura && <RecurrentesPendientes onConfirmado={cargarResumen} conProximas />}

      {/* El balance va PRIMERO y en grande: es la cifra que se viene a ver.
          Las otras tres lo explican, y por eso van en fichas más pequeñas a
          su lado. */}
      <div className="fila-tarjetas">
        <Metrica
          titulo="Balance"
          monto={totales.balance}
          nota="Recibido − Pagado − Por cobrar + Por pagar"
        />
        <Metrica titulo="Recibido" monto={totales.recibido} signo="+" barra={1} />
        <Metrica
          titulo="Pagado"
          monto={totales.pagado}
          signo="−"
          barra={proporcion(totales.pagado, totales.recibido)}
        />
        <Metrica
          titulo="Recuperado"
          monto={totales.recuperado}
          nota={`${resumen.prestamos_cobrados} préstamo${resumen.prestamos_cobrados === 1 ? '' : 's'} devuelto${resumen.prestamos_cobrados === 1 ? '' : 's'}`}
        />
      </div>

      <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
        <div className="encabezado-seccion">
          <h2>¿Dónde está la plata?</h2>
          <span className="tenue">Cuánto tienes en cada medio de pago</span>
        </div>

        {medios.length === 0 ? (
          <p className="tenue">
            Todavía no tienes medios de pago. <Link to="/medios-pago">Crea el primero</Link>.
          </p>
        ) : (
          /* Fichas y no una tabla, también en escritorio: aquí lo que importa
             es el saldo de cada medio como una cifra suelta, no comparar
             columnas. La tabla obligaba a leer en cruz para responder "¿cuánto
             tengo en Nequi?". */
          <div className="rejilla-medios">
            {medios.map((m) => (
              <TarjetaMedio key={m.medio_id ?? 'sin'} m={m} mayor={saldoMayor(medios)} />
            ))}
          </div>
        )}
      </section>

      {/* Las dos caras de lo mismo, una al lado de la otra: lo que está
          afuera y lo que hay que devolver. Juntarlas en una sola cifra neta
          escondería justamente lo que hay que hacer con cada una. */}
      <div className="fila-tarjetas dos-grandes">
        <section className="tarjeta destacada">
          <h2>Te deben</h2>
          <p className="monto-grande advertencia">{formatearMonto(resumen.prestado_pendiente)}</p>
          <p className="tenue">
            {resumen.prestamos_pendientes === 0
              ? 'No tienes préstamos sin cobrar'
              : `${resumen.prestamos_pendientes} préstamo${resumen.prestamos_pendientes === 1 ? '' : 's'} sin cobrar`}
          </p>
        </section>

        <section className="tarjeta destacada">
          <h2>Debes</h2>
          <p className="monto-grande negativo">{formatearMonto(resumen.debido_pendiente)}</p>
          <p className="tenue">
            {resumen.deudas_pendientes === 0
              ? 'No debes nada registrado'
              : `${resumen.deudas_pendientes} deuda${resumen.deudas_pendientes === 1 ? '' : 's'} sin pagar`}
          </p>
        </section>
      </div>

      {contrapartes.length > 0 && (
        <section className="tarjeta">
          <h2>Cuentas con cada quien</h2>
          <p className="subtitulo">
            El neto: a favor cuando te deben, en contra cuando debes tú
          </p>

          <div className="tabla-scroll">
            <table>
              <tbody>
                {contrapartes.map((c) => (
                  <Contraparte key={c.nombre} c={c} />
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

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
                  <th className="num">Por pagar</th>
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
                    <td className="num negativo">{formatearMonto(c.por_pagar)}</td>
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

// Una persona o negocio con cuentas pendientes, en los dos sentidos.
//
// El enlace filtra por a_quien y no por texto libre: así la lista de atrás
// agrupa exactamente igual que esta cifra (sin distinguir mayúsculas ni
// espacios de sobra), y no aparecen movimientos de otro que se llame parecido.
function Contraparte({ c }) {
  const aFavor = Number(c.neto) >= 0
  const enLosDosSentidos = Number(c.te_deben) > 0 && Number(c.le_debes) > 0

  return (
    <tr>
      <td>
        <Link to={`/movimientos?a_quien=${encodeURIComponent(c.nombre)}`}>{c.nombre}</Link>
        {c.es_categoria && (
          <div className="tenue" style={{ fontSize: 11 }} title="También tienes una categoría con este nombre">
            también es una categoría
          </div>
        )}
        <FechaCobro fecha={c.proxima_fecha} propia={!aFavor} />
      </td>

      {/* Cuando hay deuda en los dos sentidos se muestran las dos: decir solo
          "te debe 50" cuando además le debes 200 sería cierto y engañoso a la
          vez. */}
      <td className="num tenue" style={{ fontSize: 12 }}>
        {enLosDosSentidos
          ? `te debe ${formatearMonto(c.te_deben)} · le debes ${formatearMonto(c.le_debes)}`
          : aFavor
            ? `te debe ${formatearMonto(c.te_deben)}`
            : `le debes ${formatearMonto(c.le_debes)}`}
      </td>

      <td className="num nowrap">
        <span className={`fig ${aFavor ? 'advertencia' : ''}`} style={{ fontSize: 17 }}>
          {formatearMonto(String(c.neto).replace('-', ''))}
        </span>{' '}
        <span className="tenue" style={{ fontSize: 11 }}>
          {aFavor ? 'a favor' : 'en contra'}
        </span>
      </td>
    </tr>
  )
}

// Una cifra del encabezado.
//
// `signo` va aparte del monto porque es lo que carga el significado: en este
// sistema el color ya no distingue lo que entra de lo que sale, así que el +
// y el − tienen que estar siempre, no solo cuando se ven bien.
//
// `barra` es una proporción de 0 a 1 para el filete de abajo. Es lo único de
// esta pantalla que se calcula en el navegador, y se puede: no es una cifra
// que alguien lea, es el ancho de una línea.
function Metrica({ titulo, monto, nota, signo, barra }) {
  return (
    <div className="tarjeta metrica">
      <span>{titulo}</span>
      <strong>
        {signo ? `${signo} ` : ''}
        {formatearMonto(monto)}
      </strong>
      {barra !== undefined && (
        <div className="barra-oro">
          <i style={{ width: `${Math.round(Math.min(1, Math.max(0, barra)) * 100)}%` }} />
        </div>
      )}
      {nota && <span className="metrica-nota">{nota}</span>}
    </div>
  )
}

// Qué tan grande es `parte` respecto de `todo`, de 0 a 1. Solo para el ancho
// de un filete: nunca sale de aquí como número que alguien vea.
function proporcion(parte, todo) {
  const t = Number(todo)
  if (!Number.isFinite(t) || t <= 0) return 0
  return Number(parte) / t
}

// El saldo más grande de todos los medios, para que las barritas se comparen
// entre sí y no cada una consigo misma.
function saldoMayor(medios) {
  return medios.reduce((mayor, m) => Math.max(mayor, Math.abs(Number(m.saldo) || 0)), 0)
}

// "Septiembre de 2026", para el rótulo del encabezado.
function mesEnPalabras() {
  const ahora = new Date()
  const meses = [
    'Enero', 'Febrero', 'Marzo', 'Abril', 'Mayo', 'Junio',
    'Julio', 'Agosto', 'Septiembre', 'Octubre', 'Noviembre', 'Diciembre',
  ]
  return `${meses[ahora.getMonth()]} de ${ahora.getFullYear()}`
}

// Un medio de pago, como ficha.
//
// La fila "Sin registrar" va con borde punteado: no es un medio de verdad,
// es el hueco que hace que los saldos sumen el balance general. Sin ella los
// números no cuadrarían y no habría dónde ver por qué.
function TarjetaMedio({ m, mayor }) {
  const saldo = Number(m.saldo) || 0
  const ancho = mayor > 0 ? Math.round((Math.abs(saldo) / mayor) * 100) : 0
  const sinRegistrar = !m.medio_id

  return (
    <article className={`tarjeta ficha-medio ${sinRegistrar ? 'sin-registrar' : ''}`}>
      <span className="kicker">
        {sinRegistrar ? (
          m.nombre
        ) : (
          <Link to={`/movimientos?medio_pago_id=${m.medio_id}`}>{m.nombre}</Link>
        )}
      </span>
      <span className="fig saldo-medio">{formatearMonto(m.saldo)}</span>
      <div className="barra-oro">
        <i style={{ width: `${ancho}%` }} />
      </div>
      <span className="tenue detalle-medio">
        {sinRegistrar
          ? 'para que los saldos cuadren'
          : `+${formatearMonto(m.recibido)} · −${formatearMonto(m.pagado)} · ${m.movimientos} movs.`}
      </span>
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
        <div>
          <dt>Por pagar</dt>
          <dd className="negativo">{formatearMonto(c.por_pagar)}</dd>
        </div>
      </dl>
    </article>
  )
}
