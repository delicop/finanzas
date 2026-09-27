import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { cierresApi, tiendasApi } from '../lib/api'
import { formatearFecha, formatearMonto } from '../lib/formato'
import Distintivo from '../componentes/Distintivo'
import Modal from '../componentes/Modal'
import Vacio from '../componentes/Vacio'

// El resumen de las tiendas: cómo va cada local, sumando todos sus cierres.
//
// Se lee como el Resumen del dinero: arriba los totales de todas juntas y
// debajo una ficha por tienda. Los cierres del día a día están en su propia
// sección (ver Cierres.jsx), y las tiendas se crean en Categorías.
//
// Esta pantalla solo existe para quien tenga las tiendas en su plan. No se
// protege aquí: la ruta ni siquiera se monta (ver App.jsx) y el backend
// responde 403 a cualquiera que escriba /api/tiendas a mano.
export default function Tiendas() {
  const [tiendas, setTiendas] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  // La tienda abierta en el modal, o null. Se abre encima del resumen y no en
  // otra pantalla: es mirar una ficha, no irse a otra parte.
  const [abierta, setAbierta] = useState(null)
  const navegar = useNavigate()

  useEffect(() => {
    tiendasApi
      .listar()
      .then(setTiendas)
      .catch((err) => setError(err.message))
      .finally(() => setCargando(false))
  }, [])

  // Qué tajada de la venta se fue en gastos, de 0 a 100. Solo para el ancho
  // de un filete: nunca sale de aquí como número que alguien lea.
  const porcentaje = (parte, todo) => {
    const total = Number(todo)
    if (!Number.isFinite(total) || total <= 0) return 0
    return Math.round(Math.min(1, Number(parte) / total) * 100)
  }

  // Los totales de todas juntas. Se suman aquí y no en el servidor porque son
  // las mismas cifras que ya están en pantalla: pedir otra consulta para
  // sumarlas sería arriesgarse a que las dos digan cosas distintas.
  const suma = (campo) => tiendas.reduce((total, t) => total + (Number(t[campo]) || 0), 0)
  const ventas = suma('ventas')
  const salidas = suma('salidas')

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <span className="kicker">¿Cómo va cada local?</span>
          <h1>Tiendas</h1>
        </div>
        <Link to="/cierres" className="boton-enlace">
          Ver los cierres
        </Link>
      </div>

      {error && <div className="alerta">{error}</div>}

      {cargando ? (
        <section className="tarjeta">
          <p className="tenue">Cargando...</p>
        </section>
      ) : tiendas.length === 0 ? (
        <section className="tarjeta">
          <Vacio
            icono="tiendas"
            titulo="Aún no hay tiendas"
            accion={<Link to="/categorias">Crear una tienda</Link>}
          >
            Son tus locales: el del centro, el del barrio, el puesto de la plaza. Se crean junto a
            las categorías, y aquí ves cómo va cada uno.
          </Vacio>
        </section>
      ) : (
        <>
          <div className="fila-tarjetas">
            <Metrica
              titulo="Queda"
              principal
              monto={ventas - salidas}
              clase={ventas - salidas >= 0 ? 'positivo' : 'negativo'}
              nota="Las ventas menos lo que salió de la caja"
            />
            <Metrica titulo="Ventas" monto={ventas} signo="+" clase="positivo" />
            <Metrica
              titulo="Gastos"
              monto={salidas}
              signo="−"
              clase="negativo"
              nota="Compras, gastos, descuentos y vales"
            />
          </div>

          <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
            <div className="encabezado-seccion">
              <h2>Por tienda</h2>
              <span className="tenue">Toca una para ver sus cierres</span>
            </div>

            <div className="rejilla-tiendas">
              {tiendas.map((t) => (
                <article className="tarjeta ficha-tienda" key={t.id}>
                  <button
                    type="button"
                    className="ficha-tienda-cuerpo"
                    onClick={() => setAbierta(t)}
                    title={`Ver el detalle de ${t.nombre}`}
                  >
                    <span className="nombre-tienda con-distintivo">
                      <Distintivo nombre={t.nombre} clase="medio" />
                      {t.nombre}
                    </span>

                    {/* Sin cierres el saldo es cero porque no hay nada, no
                        porque el local vaya en ceros: ahí va apagado y no en
                        verde, que se leería como "todo bien". */}
                    <span
                      className={`fig saldo-tienda ${
                        t.cierres === 0 ? 'tenue' : Number(t.queda) < 0 ? 'negativo' : 'positivo'
                      }`}
                    >
                      {formatearMonto(t.queda)}
                    </span>

                    {/* El filete dice de un vistazo qué parte de la venta se
                        fue en gastos: dos fichas con el mismo saldo pero
                        distinto gasto dejan de verse iguales. */}
                    <span className="barra-tienda" aria-hidden="true">
                      <i
                        className="gastos"
                        style={{ width: `${porcentaje(t.salidas, t.ventas)}%` }}
                      />
                    </span>

                    <span className="detalle-tienda">
                      <span className="positivo">+{formatearMonto(t.ventas)}</span>
                      <span className="tenue"> · </span>
                      <span className="negativo">−{formatearMonto(t.salidas)}</span>
                    </span>

                    <span className="tenue detalle-tienda">
                      {t.cierres === 0
                        ? 'Sin cierres todavía'
                        : `${t.cierres} cierre${t.cierres === 1 ? '' : 's'} · último ${formatearFecha(t.ultimo_cierre)}`}
                    </span>
                  </button>
                </article>
              ))}
            </div>
          </section>
        </>
      )}

      {abierta && (
        <DetalleTienda
          tienda={abierta}
          onCerrar={() => setAbierta(null)}
          onVerCierres={() => navegar(`/cierres?tienda=${abierta.id}`)}
        />
      )}
    </>
  )
}

// El detalle de un local: sus cifras y sus últimos cierres. Los cierres se
// piden al abrirlo y no antes: el resumen no los necesita para pintarse.
function DetalleTienda({ tienda, onCerrar, onVerCierres }) {
  const [ultimos, setUltimos] = useState([])
  const [cargando, setCargando] = useState(true)

  useEffect(() => {
    cierresApi
      .todos({ tienda_id: tienda.id })
      .then((lista) => setUltimos(lista.slice(0, 5)))
      // Un fallo aquí no puede tapar las cifras que ya están en pantalla.
      .catch(() => {})
      .finally(() => setCargando(false))
  }, [tienda.id])

  return (
    <Modal titulo={tienda.nombre} onCerrar={onCerrar} ancho>
      <div className="fila-tarjetas">
        <Metrica
          titulo="Queda"
          principal
          monto={tienda.queda}
          clase={Number(tienda.queda) >= 0 ? 'positivo' : 'negativo'}
        />
        <Metrica titulo="Ventas" monto={tienda.ventas} signo="+" clase="positivo" />
        <Metrica titulo="Gastos" monto={tienda.salidas} signo="−" clase="negativo" />
      </div>

      <div className="encabezado-seccion">
        <h2>Últimos cierres</h2>
        {tienda.cierres > 0 && (
          <span className="tenue">
            {tienda.cierres} en total · último {formatearFecha(tienda.ultimo_cierre)}
          </span>
        )}
      </div>

      {cargando ? (
        <p className="tenue">Cargando...</p>
      ) : ultimos.length === 0 ? (
        <p className="tenue">
          Todavía no tiene cierres. Se hacen en la sección Cierres, al final del día.
        </p>
      ) : (
        <div className="tabla-scroll">
          <table>
            <thead>
              <tr>
                <th>Día</th>
                <th>Responsable</th>
                <th className="num">Ventas</th>
                <th className="num">Diferencia</th>
              </tr>
            </thead>
            <tbody>
              {ultimos.map((c) => {
                const descuadre = Math.round(Number(c.totales.queda_diferencia) || 0) !== 0
                return (
                  <tr key={c.id}>
                    <td className="nowrap">{formatearFecha(c.fecha)}</td>
                    <td className="tenue">{c.responsable || '—'}</td>
                    <td className="num fig">{formatearMonto(c.venta_tienda)}</td>
                    <td className={`num fig ${descuadre ? 'negativo' : 'tenue'}`}>
                      {formatearMonto(c.totales.queda_diferencia)}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="acciones-modal">
        <button type="button" className="secundario" onClick={onCerrar}>
          Cerrar
        </button>
        <button type="button" onClick={onVerCierres}>
          Ver sus cierres
        </button>
      </div>
    </Modal>
  )
}

// La misma ficha de cifra del Resumen, para que las dos pantallas se lean
// igual. Se copia y no se importa porque Dashboard la tiene privada; el día
// que una tercera la necesite, se saca a un componente.
function Metrica({ titulo, monto, nota, signo, clase = '', principal = false }) {
  return (
    <div className={`tarjeta metrica ${principal ? 'principal' : ''}`}>
      <span>{titulo}</span>
      <strong className={clase}>
        {signo ? `${signo} ` : ''}
        {formatearMonto(monto)}
      </strong>
      {nota && <span className="metrica-nota">{nota}</span>}
    </div>
  )
}
