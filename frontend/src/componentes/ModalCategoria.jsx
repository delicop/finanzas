import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { movimientosApi } from '../lib/api'
import {
  ETIQUETAS_ESTADO,
  ETIQUETAS_TIPO,
  esDeuda,
  esDeudaPropia,
  estiloMonto,
  formatearFecha,
  formatearMonto,
  rutaTraslado,
} from '../lib/formato'
import { useAlGuardarMovimiento } from '../lib/eventos'
import Modal from './Modal'
import EtiquetaTipo from './EtiquetaTipo'

// Cuántos se traen de una vez. Es para mirar, no para trabajar: para editar o
// filtrar está la pantalla de Movimientos, a un clic.
const TOPE = 200

// Todo lo de una categoría, abierto desde el Resumen: sus cifras arriba y
// cada movimiento debajo, incluidos los traslados que le entraron desde otra.
//
// `categoria` es la fila del resumen tal como está ahora: el Resumen la busca
// otra vez en cada render, así que si el asistente guarda algo con el modal
// abierto, las cifras de arriba se actualizan igual que las de atrás.
export default function ModalCategoria({ categoria: c, onCerrar }) {
  const [movimientos, setMovimientos] = useState(null)
  const [total, setTotal] = useState(0)
  const [error, setError] = useState('')

  function cargar() {
    return movimientosApi
      .listar({ categoria_id: c.categoria_id, limite: TOPE })
      .then((d) => {
        setMovimientos(d.movimientos)
        setTotal(d.total)
      })
      .catch((err) => setError(err.message))
  }

  useEffect(() => {
    cargar()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [c.categoria_id])

  useAlGuardarMovimiento(cargar)

  const hayTraslados = Number(c.traslados_entraron) > 0 || Number(c.traslados_salieron) > 0

  return (
    <Modal titulo={c.nombre} onCerrar={onCerrar} ancho>
      <div className="detalle-cat-balance">
        <span className="tenue">Balance</span>
        <strong className={`fig ${Number(c.balance) >= 0 ? 'positivo' : 'negativo'}`}>
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
        {/* Solo si hubo: en la mayoría de las categorías serían dos ceros
            más que leer. */}
        {hayTraslados && (
          <>
            <div>
              <dt>Llegó de otras</dt>
              <dd className="positivo">{formatearMonto(c.traslados_entraron)}</dd>
            </div>
            <div>
              <dt>Pasó a otras</dt>
              <dd className="negativo">{formatearMonto(c.traslados_salieron)}</dd>
            </div>
          </>
        )}
      </dl>

      {error && <div className="alerta">{error}</div>}

      {!movimientos && !error ? (
        <p className="tenue">Cargando movimientos...</p>
      ) : movimientos?.length === 0 ? (
        <p className="tenue">Esta categoría todavía no tiene movimientos.</p>
      ) : (
        movimientos && (
          <ul className="lista-detalle">
            {movimientos.map((m) => (
              <FilaDetalle key={m.id} m={m} categoriaID={c.categoria_id} />
            ))}
          </ul>
        )
      )}

      <div className="detalle-cat-pie">
        <span className="tenue">
          {total > TOPE
            ? `Se muestran los ${TOPE} más recientes de ${total}`
            : `${total} movimiento${total === 1 ? '' : 's'}`}
        </span>
        <Link to={`/movimientos?categoria_id=${c.categoria_id}`}>Ver y editar en Movimientos →</Link>
      </div>
    </Modal>
  )
}

// Un movimiento, visto desde esta categoría.
//
// Lo único que cambia respecto de la lista general es el signo de un traslado
// entre categorías: desde Trabajo, lo que llegó de Casa es un "+", y desde
// Casa es un "−". En la lista general el mismo traslado no suma ni resta.
function FilaDetalle({ m, categoriaID }) {
  const { signo, clase } = estiloEnCategoria(m, categoriaID)
  const propia = esDeudaPropia(m)

  let titulo = m.descripcion
  if (!titulo && m.tipo === 'traslado') titulo = rutaTraslado(m)
  if (!titulo && esDeuda(m)) titulo = propia ? `Le debes a ${m.a_quien}` : m.a_quien
  if (!titulo) titulo = ETIQUETAS_TIPO[m.tipo]

  return (
    <li className="lista-detalle-fila">
      <div className="lista-detalle-texto">
        <span className="lista-detalle-titulo">{titulo}</span>
        <div className="sub">
          <span>{formatearFecha(m.fecha)}</span>
          <EtiquetaTipo m={m} />
          {m.tipo === 'traslado' ? (
            // La descripción ya puede ser otra cosa: la ruta va siempre, que
            // es lo que responde "¿de dónde salió?".
            m.descripcion && <span className="traslado-ruta">{rutaTraslado(m)}</span>
          ) : (
            m.medio_pago_nombre && <span>{m.medio_pago_nombre}</span>
          )}
          {esDeuda(m) && (
            <>
              {m.descripcion && <span className="deuda-quien">{propia ? `le debes a ${m.a_quien}` : m.a_quien}</span>}
              <span className={`estado estado-${m.estado}`}>{ETIQUETAS_ESTADO[m.estado]}</span>
              {m.abonos > 0 && m.estado !== 'pagado' && (
                <span className="saldo-pendiente">faltan {formatearMonto(m.saldo)}</span>
              )}
            </>
          )}
        </div>
      </div>
      <span className={`monto-valor ${clase}`}>
        {signo} {formatearMonto(m.monto)}
      </span>
    </li>
  )
}

function estiloEnCategoria(m, categoriaID) {
  if (m.tipo === 'traslado' && m.categoria_destino_id) {
    return m.categoria_destino_id === categoriaID
      ? { signo: '+', clase: 'positivo' }
      : { signo: '−', clase: 'negativo' }
  }
  return estiloMonto(m)
}
