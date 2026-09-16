import { useState } from 'react'
import { formatearMonto } from '../lib/formato'
import Modal from './Modal'

// Al marcar un préstamo como pagado preguntamos POR DÓNDE te pagaron.
//
// No es un detalle menor: puedes haber prestado en efectivo y que te devuelvan
// por transferencia. Si no se registra, el resumen mostraría que te falta
// plata en efectivo y que nunca llegó al banco.
export default function ModalCobro({ movimiento, medios, onCerrar, onConfirmar }) {
  // Arrancamos con el mismo medio con el que se prestó: es lo más común
  // (te devuelven por donde prestaste), así casi siempre es un solo clic.
  const [medioID, setMedioID] = useState(movimiento.medio_pago_id ?? '')
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState('')

  async function confirmar() {
    setGuardando(true)
    setError('')
    try {
      await onConfirmar(medioID ? Number(medioID) : 0)
    } catch (err) {
      setError(err.message)
      setGuardando(false)
    }
  }

  return (
    <Modal titulo="¿Por dónde te pagaron?" onCerrar={onCerrar}>
      {error && <div className="alerta">{error}</div>}

      <p className="resumen-cobro">
        <strong>{movimiento.a_quien}</strong> te devolvió{' '}
        <strong className="positivo">{formatearMonto(movimiento.monto)}</strong>
        {movimiento.medio_pago_nombre && (
          <span className="tenue"> · le prestaste en {movimiento.medio_pago_nombre}</span>
        )}
      </p>

      <label htmlFor="medio-cobro">Medio de pago</label>
      <select
        id="medio-cobro"
        value={medioID}
        onChange={(e) => setMedioID(e.target.value)}
        autoFocus
      >
        <option value="">Sin registrar</option>
        {medios.map((m) => (
          <option key={m.id} value={m.id}>
            {m.nombre}
          </option>
        ))}
      </select>

      <div className="acciones-modal">
        <button type="button" className="secundario" onClick={onCerrar} disabled={guardando}>
          Cancelar
        </button>
        <button type="button" className="principal-pagar" onClick={confirmar} disabled={guardando}>
          {guardando ? 'Guardando...' : '✓ Marcar pagado'}
        </button>
      </div>
    </Modal>
  )
}
