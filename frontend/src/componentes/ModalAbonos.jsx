import { useCallback, useEffect, useState } from 'react'
import { movimientosApi } from '../lib/api'
import {
  entradaAMonto,
  esDeudaPropia,
  formatearFecha,
  formatearMonto,
  hoyISO,
  montoAEntrada,
  sumarDias,
} from '../lib/formato'
import InputMonto from './InputMonto'
import Modal from './Modal'

// Los pagos parciales de una deuda y el acuerdo de cuotas.
//
// Las dos cosas viven en el mismo modal porque responden la misma pregunta
// desde dos lados: "¿cuánto falta?" (los abonos, que son plata de verdad) y
// "¿para cuándo?" (las cuotas, que son solo el calendario). Separarlas en dos
// pantallas obligaría a ir y volver para entender una sola deuda.
//
// Ninguna cuenta se hace aquí: el saldo, lo abonado y si una cuota está
// cubierta los calcula Postgres y llegan ya resueltos.
export default function ModalAbonos({ movimiento, medios, onCerrar, onCambio }) {
  const propia = esDeudaPropia(movimiento)

  const [deuda, setDeuda] = useState(movimiento)
  const [abonos, setAbonos] = useState([])
  const [cuotas, setCuotas] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [campos, setCampos] = useState({})
  const [guardando, setGuardando] = useState(false)
  const [armandoAcuerdo, setArmandoAcuerdo] = useState(false)

  const cargar = useCallback(async () => {
    setCargando(true)
    try {
      const [a, c] = await Promise.all([
        movimientosApi.listarAbonos(movimiento.id),
        movimientosApi.listarCuotas(movimiento.id),
      ])
      setAbonos(a.abonos)
      setCuotas(c.cuotas)
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }, [movimiento.id])

  useEffect(() => {
    cargar()
  }, [cargar])

  // Cada cambio devuelve la deuda ya recalculada: se guarda aquí y se le pasa
  // al padre para que la lista de atrás no quede mostrando el saldo viejo.
  function aplicar(actualizada) {
    setDeuda(actualizada)
    onCambio?.(actualizada)
  }

  const saldada = deuda.saldo === '0.00' || Number(deuda.saldo) <= 0

  return (
    <Modal titulo={propia ? 'Lo que debes' : 'Lo que te deben'} onCerrar={onCerrar}>
      {error && <div className="alerta">{error}</div>}

      <Encabezado deuda={deuda} propia={propia} />

      {cargando ? (
        <p className="tenue">Cargando...</p>
      ) : (
        <>
          <ListaAbonos
            abonos={abonos}
            propia={propia}
            onBorrar={async (abono) => {
              if (!confirm(`¿Borrar el abono de ${formatearMonto(abono.monto)}?`)) return
              try {
                const actualizada = await movimientosApi.borrarAbono(movimiento.id, abono.id)
                aplicar(actualizada)
                await cargar()
              } catch (err) {
                setError(err.message)
              }
            }}
          />

          {saldada ? (
            <p className="ayuda-campo tenue">
              Esta deuda ya está saldada. Si te falta registrar algo, borra un abono primero.
            </p>
          ) : (
            <FormAbono
              deuda={deuda}
              medios={medios}
              propia={propia}
              campos={campos}
              guardando={guardando}
              onGuardar={async (datos) => {
                setGuardando(true)
                setError('')
                setCampos({})
                try {
                  const actualizada = await movimientosApi.abonar(movimiento.id, datos)
                  aplicar(actualizada)
                  await cargar()
                } catch (err) {
                  setError(err.message)
                  setCampos(err.campos ?? {})
                } finally {
                  setGuardando(false)
                }
              }}
            />
          )}

          <hr className="separador" />

          <Acuerdo
            deuda={deuda}
            cuotas={cuotas}
            propia={propia}
            abierto={armandoAcuerdo}
            onAbrir={() => setArmandoAcuerdo(true)}
            onCerrarAcuerdo={() => setArmandoAcuerdo(false)}
            onGuardar={async (datos) => {
              setError('')
              setCampos({})
              try {
                const res = await movimientosApi.guardarAcuerdo(movimiento.id, datos)
                setCuotas(res.cuotas)
                setArmandoAcuerdo(false)
              } catch (err) {
                setError(err.message)
                setCampos(err.campos ?? {})
              }
            }}
            onBorrar={async () => {
              if (!confirm('¿Borrar el acuerdo de pago? Los abonos no se tocan.')) return
              try {
                await movimientosApi.borrarAcuerdo(movimiento.id)
                setCuotas([])
              } catch (err) {
                setError(err.message)
              }
            }}
            campos={campos}
          />
        </>
      )}

      <div className="acciones-modal">
        <button type="button" className="secundario" onClick={onCerrar}>
          Cerrar
        </button>
      </div>
    </Modal>
  )
}

/* ------------------------------------------------------------------------ */

function Encabezado({ deuda, propia }) {
  return (
    <div className="resumen-deuda">
      <p className="resumen-cobro">
        <strong>{deuda.a_quien}</strong>{' '}
        {propia ? 'te prestó' : 'se llevó'}{' '}
        <strong>{formatearMonto(deuda.monto)}</strong>
        <span className="tenue"> el {formatearFecha(deuda.fecha)}</span>
        {deuda.descripcion && <span className="tenue"> · {deuda.descripcion}</span>}
      </p>

      <div className="cifras-deuda">
        <div>
          <span className="tenue">Ya {propia ? 'pagaste' : 'te devolvió'}</span>
          <strong className="positivo">{formatearMonto(deuda.abonado)}</strong>
        </div>
        <div>
          <span className="tenue">Falta</span>
          <strong className={Number(deuda.saldo) > 0 ? 'advertencia' : 'positivo'}>
            {formatearMonto(deuda.saldo)}
          </strong>
        </div>
      </div>
    </div>
  )
}

function ListaAbonos({ abonos, propia, onBorrar }) {
  if (abonos.length === 0) {
    return (
      <p className="tenue">
        Todavía no hay abonos: {propia ? 'no has pagado nada' : 'no te han devuelto nada'}.
      </p>
    )
  }

  return (
    <div className="lista-abonos">
      <h3>Abonos</h3>
      {abonos.map((a) => (
        <div key={a.id} className="abono">
          <div>
            <strong>{formatearMonto(a.monto)}</strong>
            <span className="tenue"> · {formatearFecha(a.fecha)}</span>
            {a.medio_nombre && <span className="sub-medio"> · {a.medio_nombre}</span>}
            {a.nota && <div className="sub tenue">{a.nota}</div>}
          </div>
          <button className="menor menor-peligro" onClick={() => onBorrar(a)}>
            Borrar
          </button>
        </div>
      ))}
    </div>
  )
}

function FormAbono({ deuda, medios, propia, campos, guardando, onGuardar }) {
  const [monto, setMonto] = useState('')
  const [fecha, setFecha] = useState(hoyISO())
  // Arranca con el mismo medio del préstamo: es lo más común (te devuelven por
  // donde prestaste), así casi siempre es un campo menos que tocar.
  const [medioID, setMedioID] = useState(deuda.medio_pago_id ?? '')
  const [nota, setNota] = useState('')

  function enviar(e) {
    e.preventDefault()
    onGuardar({
      monto: entradaAMonto(monto),
      fecha,
      medio_id: medioID ? Number(medioID) : 0,
      nota,
    })
  }

  return (
    <form onSubmit={enviar} className="form-abono" noValidate>
      <h3>{propia ? 'Registrar un pago' : 'Registrar un abono'}</h3>

      <div className="dos-columnas">
        <div>
          <label htmlFor="abono-monto">Monto <span className="req">*</span></label>
          <InputMonto id="abono-monto" valor={monto} onCambio={setMonto} placeholder="50.000" />
          {campos.monto && <span className="error-campo">{campos.monto}</span>}
          {/* Un atajo para el caso más común después del abono parcial:
              terminar de pagar lo que queda. */}
          <button
            type="button"
            className="menor"
            onClick={() => setMonto(montoAEntrada(deuda.saldo))}
          >
            Todo lo que falta ({formatearMonto(deuda.saldo)})
          </button>
        </div>

        <div>
          <label htmlFor="abono-fecha">Fecha</label>
          <input
            id="abono-fecha"
            type="date"
            value={fecha}
            onChange={(e) => setFecha(e.target.value)}
          />
          {campos.fecha && <span className="error-campo">{campos.fecha}</span>}
        </div>
      </div>

      <label htmlFor="abono-medio">
        {propia ? '¿Por dónde le pagaste?' : '¿Por dónde te pagó?'}
      </label>
      <select id="abono-medio" value={medioID} onChange={(e) => setMedioID(e.target.value)}>
        <option value="">Sin registrar</option>
        {medios.map((m) => (
          <option key={m.id} value={m.id}>
            {m.nombre}
          </option>
        ))}
      </select>
      {campos.medio_id && <span className="error-campo">{campos.medio_id}</span>}

      <label htmlFor="abono-nota">Nota <span className="tenue">(opcional)</span></label>
      <input
        id="abono-nota"
        value={nota}
        onChange={(e) => setNota(e.target.value)}
        placeholder="Opcional"
      />

      <button type="submit" disabled={guardando}>
        {guardando ? 'Guardando...' : 'Registrar abono'}
      </button>
    </form>
  )
}

/* ---------------------------- acuerdo de pago --------------------------- */

function Acuerdo({ deuda, cuotas, propia, abierto, onAbrir, onCerrarAcuerdo, onGuardar, onBorrar, campos }) {
  if (abierto) {
    return (
      <FormAcuerdo
        deuda={deuda}
        campos={campos}
        onGuardar={onGuardar}
        onCancelar={onCerrarAcuerdo}
      />
    )
  }

  if (cuotas.length === 0) {
    return (
      <div className="bloque-acuerdo">
        <h3>Acuerdo de pago</h3>
        <p className="tenue">
          Sin acuerdo. Si quedaron en pagar por cuotas, ármalo aquí y la app avisa el día que
          vence cada una.
        </p>
        <button type="button" className="secundario" onClick={onAbrir}>
          Armar acuerdo
        </button>
      </div>
    )
  }

  const cubiertas = cuotas.filter((c) => c.cubierta).length

  return (
    <div className="bloque-acuerdo">
      <h3>
        Acuerdo de pago{' '}
        <span className="tenue">
          ({cubiertas} de {cuotas.length} cubierta{cuotas.length === 1 ? '' : 's'})
        </span>
      </h3>

      <div className="lista-cuotas">
        {cuotas.map((c) => (
          <div key={c.id} className={`cuota ${c.cubierta ? 'cuota-cubierta' : ''}`}>
            <span className="cuota-numero">{c.numero}</span>
            <span>{formatearMonto(c.monto)}</span>
            <span className="tenue">{formatearFecha(c.vence_el)}</span>
            <span className={c.cubierta ? 'positivo' : 'tenue'}>
              {c.cubierta ? '✓ cubierta' : 'pendiente'}
            </span>
          </div>
        ))}
      </div>

      <p className="ayuda-campo tenue">
        Las cuotas son el calendario, no la plata: lo que se debe sigue siendo el saldo de
        arriba. Los abonos las van cubriendo en orden.
        {propia
          ? ' El día que venza una sin cubrir, te avisamos.'
          : ' Si se pasa una sin cubrir, te avisamos para que cobres.'}
      </p>

      <div className="acciones-encabezado">
        <button type="button" className="secundario" onClick={onAbrir}>
          Rehacer acuerdo
        </button>
        <button type="button" className="menor menor-peligro" onClick={onBorrar}>
          Borrar acuerdo
        </button>
      </div>
    </div>
  )
}

function FormAcuerdo({ deuda, campos, onGuardar, onCancelar }) {
  const [cantidad, setCantidad] = useState(3)
  const [cada, setCada] = useState('mensual')
  const [primera, setPrimera] = useState(deuda.cobrar_el || sumarDias(hoyISO(), 30))

  function enviar(e) {
    e.preventDefault()
    // El `total` se deja vacío a propósito: el backend usa el saldo actual,
    // que es sobre lo que se negocia un acuerdo. Y el reparto en cuotas lo
    // hace él, no el navegador: ahí es donde se pierden los pesos.
    onGuardar({ cantidad: Number(cantidad), cada, primera })
  }

  return (
    <form onSubmit={enviar} className="bloque-acuerdo" noValidate>
      <h3>Armar acuerdo de pago</h3>
      <p className="tenue">
        Se reparten los {formatearMonto(deuda.saldo)} que faltan. Si el monto no se divide
        exacto, los centavos que sobran van en las primeras cuotas.
      </p>

      <div className="dos-columnas">
        <div>
          <label htmlFor="ac-cantidad">¿Cuántas cuotas?</label>
          <input
            id="ac-cantidad"
            type="number"
            min="1"
            max="120"
            value={cantidad}
            onChange={(e) => setCantidad(e.target.value)}
          />
          {campos.cantidad && <span className="error-campo">{campos.cantidad}</span>}
        </div>

        <div>
          <label htmlFor="ac-cada">¿Cada cuánto?</label>
          <select id="ac-cada" value={cada} onChange={(e) => setCada(e.target.value)}>
            <option value="mensual">Cada mes</option>
            <option value="quincenal">Cada 15 días</option>
            <option value="semanal">Cada semana</option>
          </select>
          {campos.cada && <span className="error-campo">{campos.cada}</span>}
        </div>
      </div>

      <label htmlFor="ac-primera">¿Cuándo vence la primera?</label>
      <input
        id="ac-primera"
        type="date"
        value={primera}
        min={deuda.fecha}
        onChange={(e) => setPrimera(e.target.value)}
      />
      {campos.primera && <span className="error-campo">{campos.primera}</span>}
      {campos.cuotas && <span className="error-campo">{campos.cuotas}</span>}

      <div className="acciones-modal">
        <button type="button" className="secundario" onClick={onCancelar}>
          Cancelar
        </button>
        <button type="submit">Guardar acuerdo</button>
      </div>
    </form>
  )
}
