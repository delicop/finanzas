import { useState } from 'react'
import { authApi } from '../lib/api'
import Modal from './Modal'

export default function ModalPassword({ onCerrar }) {
  const [actual, setActual] = useState('')
  const [nueva, setNueva] = useState('')
  const [repetir, setRepetir] = useState('')
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [listo, setListo] = useState(false)
  const [guardando, setGuardando] = useState(false)

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})

    // La confirmación se valida solo aquí: al backend no le importa, él
    // recibe una sola contraseña nueva. Es para atajar un dedazo.
    if (nueva !== repetir) {
      setCampos({ repetir: 'Las contraseñas no coinciden' })
      return
    }

    setGuardando(true)
    try {
      await authApi.cambiarPassword(actual, nueva)
      setListo(true)
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  if (listo) {
    return (
      <Modal titulo="Contraseña actualizada" onCerrar={onCerrar}>
        <p className="resumen-cobro">
          Listo. La próxima vez que entres, usa la contraseña nueva.
        </p>
        <p className="tenue" style={{ fontSize: 13 }}>
          Tu sesión actual sigue abierta.
        </p>
        <div className="acciones-modal">
          <button type="button" onClick={onCerrar}>
            Entendido
          </button>
        </div>
      </Modal>
    )
  }

  return (
    <Modal titulo="Cambiar contraseña" onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="actual">Contraseña actual</label>
        <input
          id="actual"
          type="password"
          autoComplete="current-password"
          value={actual}
          onChange={(e) => setActual(e.target.value)}
          autoFocus
        />
        {campos.actual && <span className="error-campo">{campos.actual}</span>}

        <label htmlFor="nueva">Contraseña nueva</label>
        <input
          id="nueva"
          type="password"
          autoComplete="new-password"
          value={nueva}
          onChange={(e) => setNueva(e.target.value)}
        />
        {campos.nueva && <span className="error-campo">{campos.nueva}</span>}
        <span className="tenue" style={{ fontSize: 12 }}>
          Mínimo 8 caracteres
        </span>

        <label htmlFor="repetir">Repite la contraseña nueva</label>
        <input
          id="repetir"
          type="password"
          autoComplete="new-password"
          value={repetir}
          onChange={(e) => setRepetir(e.target.value)}
        />
        {campos.repetir && <span className="error-campo">{campos.repetir}</span>}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {guardando ? 'Guardando...' : 'Cambiar contraseña'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
