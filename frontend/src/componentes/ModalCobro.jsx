import { useState } from 'react'
import { esDeudaPropia, formatearMonto } from '../lib/formato'
import CubrirFaltante, { CUBRIR_VACIO, cuerpoCubrir } from './CubrirFaltante'
import Modal from './Modal'

// Al marcar un préstamo como pagado preguntamos POR DÓNDE te pagaron.
//
// No es un detalle menor: puedes haber prestado en efectivo y que te devuelvan
// por transferencia. Si no se registra, el resumen mostraría que te falta
// plata en efectivo y que nunca llegó al banco.
//
// Sirve para los dos lados: cobrar un préstamo que hiciste y terminar de
// pagar una deuda tuya. En el segundo caso la plata SALE del medio, y si ahí
// no alcanza se pregunta de dónde salió, igual que en un gasto.
export default function ModalCobro({ movimiento, categorias = [], medios, contrapartes = [], onCerrar, onConfirmar }) {
  const propia = esDeudaPropia(movimiento)
  // Arrancamos con el mismo medio con el que se prestó: es lo más común
  // (te devuelven por donde prestaste), así casi siempre es un solo clic.
  const [medioID, setMedioID] = useState(movimiento.medio_pago_id ?? '')
  const [guardando, setGuardando] = useState(false)
  const [error, setError] = useState('')
  const [campos, setCampos] = useState({})
  const [falta, setFalta] = useState(null)
  const [cubrir, setCubrir] = useState(CUBRIR_VACIO)

  // Lo que se registra es lo que FALTA, no el monto original: si ya había
  // abonos, saldar es poner el resto.
  const saldo = movimiento.saldo ?? movimiento.monto

  async function confirmar() {
    setGuardando(true)
    setError('')
    setCampos({})
    try {
      await onConfirmar(medioID ? Number(medioID) : 0, falta ? cuerpoCubrir(cubrir, falta) : undefined)
    } catch (err) {
      if (err.faltaPlata) {
        setFalta(err.faltaPlata)
      } else {
        setError(err.message)
      }
      setCampos(err.campos ?? {})
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={propia ? '¿Por dónde le pagaste?' : '¿Por dónde te pagaron?'} onCerrar={onCerrar}>
      {error && <div className="alerta">{error}</div>}

      <p className="resumen-cobro">
        {propia ? (
          <>
            Le terminas de pagar <strong className="negativo">{formatearMonto(saldo)}</strong> a{' '}
            <strong>{movimiento.a_quien}</strong>
            {movimiento.medio_pago_nombre && (
              <span className="tenue"> · te prestó en {movimiento.medio_pago_nombre}</span>
            )}
          </>
        ) : (
          <>
            <strong>{movimiento.a_quien}</strong> te devolvió{' '}
            <strong className="positivo">{formatearMonto(saldo)}</strong>
            {movimiento.medio_pago_nombre && (
              <span className="tenue"> · le prestaste en {movimiento.medio_pago_nombre}</span>
            )}
          </>
        )}
      </p>

      <label htmlFor="medio-cobro">Medio de pago</label>
      <select
        id="medio-cobro"
        value={medioID}
        onChange={(e) => {
          setMedioID(e.target.value)
          // Otro medio, otras cifras: el aviso de antes ya no aplica.
          setFalta(null)
        }}
        autoFocus
      >
        <option value="">Sin registrar</option>
        {medios.map((m) => (
          <option key={m.id} value={m.id}>
            {m.nombre}
          </option>
        ))}
      </select>

      {falta && (
        <CubrirFaltante
          falta={falta}
          valor={cubrir}
          onCambio={setCubrir}
          categorias={categorias}
          medios={medios}
          contrapartes={contrapartes}
          campos={campos}
        />
      )}

      <div className="acciones-modal">
        <button type="button" className="secundario" onClick={onCerrar} disabled={guardando}>
          Cancelar
        </button>
        <button type="button" className="principal-pagar" onClick={confirmar} disabled={guardando}>
          {guardando ? 'Guardando...' : falta ? 'Guardar todo' : '✓ Marcar pagado'}
        </button>
      </div>
    </Modal>
  )
}
