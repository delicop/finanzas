import { useCallback, useEffect, useState } from 'react'
import { recurrentesApi } from '../lib/api'
import { entradaAMonto, formatearFecha, formatearMonto, montoAEntrada } from '../lib/formato'
import { avisarMovimientoGuardado } from '../lib/eventos'
import InputMonto from './InputMonto'

// Lo que toca confirmar de los gastos e ingresos que se repiten.
//
// Sale en el resumen y en la página de Recurrentes, arriba de todo, porque es
// lo único de esas pantallas que pide una acción HOY. Si no hay nada
// pendiente, no ocupa ni un pixel.
//
// La app no registra el arriendo sola a propósito: un gasto inventado no se
// nota nunca, mientras que uno olvidado salta al cuadrar el mes. Aquí se ve lo
// que tocaba y se confirma con un clic — o se corrige el monto antes, que es
// lo que pasa con el recibo de la luz todos los meses.
export default function RecurrentesPendientes({ onConfirmado }) {
  const [pendientes, setPendientes] = useState([])
  const [error, setError] = useState('')
  const [ocupado, setOcupado] = useState(null)
  const [corrigiendo, setCorrigiendo] = useState(null)

  const cargar = useCallback(async (senal) => {
    try {
      const datos = await recurrentesApi.pendientes(senal)
      setPendientes(datos.pendientes)
    } catch (err) {
      if (err.name === 'AbortError') return
      // Un 404 significa que la ruta no existe (servidor viejo): no es un
      // error que valga la pena mostrarle a nadie.
      if (err.status !== 404) setError(err.message)
    }
  }, [])

  useEffect(() => {
    const control = new AbortController()
    cargar(control.signal)
    return () => control.abort()
  }, [cargar])

  async function confirmar(o, monto) {
    setOcupado(o.id)
    setError('')
    try {
      // Sin monto se confirma la plantilla tal cual; con monto, se manda el
      // movimiento completo porque el backend valida la entrada entera.
      const cuerpo = monto
        ? {
            categoria_id: o.categoria_id,
            medio_pago_id: o.medio_pago_id,
            tipo: o.tipo,
            monto,
            fecha: o.fecha,
            descripcion: o.descripcion,
          }
        : undefined

      await recurrentesApi.confirmar(o.id, cuerpo)
      setPendientes((prev) => prev.filter((x) => x.id !== o.id))
      setCorrigiendo(null)
      // El mismo evento que usa el asistente: el resumen y la lista de
      // movimientos se actualizan solos, sin recargar la página.
      avisarMovimientoGuardado()
      onConfirmado?.()
    } catch (err) {
      setError(err.message)
    } finally {
      setOcupado(null)
    }
  }

  async function descartar(o) {
    if (!confirm(`¿Descartar "${o.descripcion}" del ${formatearFecha(o.fecha)}? No se registra nada.`)) {
      return
    }
    setOcupado(o.id)
    setError('')
    try {
      await recurrentesApi.descartar(o.id)
      setPendientes((prev) => prev.filter((x) => x.id !== o.id))
    } catch (err) {
      setError(err.message)
    } finally {
      setOcupado(null)
    }
  }

  if (pendientes.length === 0 && !error) return null

  return (
    <section className="tarjeta pendientes-recurrentes">
      <h2>
        Por confirmar{' '}
        <span className="contador">{pendientes.length}</span>
      </h2>
      <p className="subtitulo">Gastos e ingresos que se repiten y ya les tocaba</p>

      {error && <div className="alerta">{error}</div>}

      {pendientes.map((o) => (
        <article key={o.id} className="pendiente">
          <div className="pendiente-datos">
            <strong>{o.descripcion}</strong>
            <span className="tenue">
              {formatearFecha(o.fecha)} · {o.categoria_nombre} · {o.medio_pago_nombre}
            </span>
          </div>

          {corrigiendo === o.id ? (
            <CorregirMonto
              ocurrencia={o}
              ocupado={ocupado === o.id}
              onCancelar={() => setCorrigiendo(null)}
              onConfirmar={(monto) => confirmar(o, monto)}
            />
          ) : (
            <div className="pendiente-acciones">
              <strong className={o.tipo === 'recibi' ? 'positivo' : 'negativo'}>
                {formatearMonto(o.monto)}
              </strong>
              <button
                className="principal-pagar"
                disabled={ocupado === o.id}
                onClick={() => confirmar(o)}
              >
                {ocupado === o.id ? '...' : o.tipo === 'recibi' ? '✓ Lo recibí' : '✓ Lo pagué'}
              </button>
              <button className="menor" onClick={() => setCorrigiendo(o.id)}>
                Otro monto
              </button>
              <button className="menor menor-peligro" onClick={() => descartar(o)}>
                Este mes no
              </button>
            </div>
          )}
        </article>
      ))}
    </section>
  )
}

// El recibo de la luz nunca llega por el mismo valor. Corregir el monto antes
// de confirmar es el caso normal, no la excepción.
function CorregirMonto({ ocurrencia, ocupado, onCancelar, onConfirmar }) {
  const [monto, setMonto] = useState(montoAEntrada(ocurrencia.monto))

  return (
    <div className="pendiente-corregir">
      <InputMonto
        valor={monto}
        onCambio={setMonto}
        placeholder={montoAEntrada(ocurrencia.monto)}
      />
      <button disabled={ocupado} onClick={() => onConfirmar(entradaAMonto(monto))}>
        {ocupado ? '...' : 'Confirmar'}
      </button>
      <button className="secundario" onClick={onCancelar}>
        Cancelar
      </button>
    </div>
  )
}
