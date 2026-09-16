import { useEffect, useState } from 'react'
import { mediosApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'

export default function MediosPago() {
  const [medios, setMedios] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [editando, setEditando] = useState(null) // null = modal cerrado
  const esMovil = useEsMovil()
  // Mirando la cuenta de otro no se ofrece nada que escriba: el backend
  // rechaza cualquier método que no sea GET mientras dura la revisión.
  const { soloLectura } = useAuth()

  async function recargar() {
    setError('')
    try {
      setMedios(await mediosApi.listar())
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
  }, [])

  async function eliminar(medio) {
    if (!confirm(`¿Eliminar el medio de pago "${medio.nombre}"?`)) return
    try {
      await mediosApi.eliminar(medio.id)
      await recargar()
    } catch (err) {
      // El backend responde 409 si el medio está en uso.
      setError(err.message)
    }
  }

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Medios de pago</h1>
        {!soloLectura && (
          <button onClick={() => setEditando({ id: null, nombre: '' })}>Nuevo medio</button>
        )}
      </div>

      {error && <div className="alerta">{error}</div>}

      <section className="tarjeta">
        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : medios.length === 0 ? (
          <p className="tenue">Aún no hay medios de pago. Crea el primero (efectivo, transferencia, Nequi...).</p>
        ) : esMovil ? (
          // En teléfono, cada medio es una tarjeta con los botones
          // grandes debajo, en vez de una fila apretada con scroll lateral.
          <div className="lista-movil">
            {medios.map((c) => (
              <article className="tarjeta-cat" key={c.id}>
                <div className="tarjeta-cat-arriba">
                  <strong>{c.nombre}</strong>
                  <span className="tenue">
                    {c.movimientos} mov{c.movimientos === 1 ? '' : 's'}.
                  </span>
                </div>
                {!soloLectura && (
                  <div className="tarjeta-mov-acciones">
                    <button className="secundario" onClick={() => setEditando(c)}>
                      Editar
                    </button>
                    <button className="peligro" onClick={() => eliminar(c)}>
                      Eliminar
                    </button>
                  </div>
                )}
              </article>
            ))}
          </div>
        ) : (
          <div className="tabla-scroll">
            <table>
              <thead>
                <tr>
                  <th>Nombre</th>
                  <th className="num">Movimientos</th>
                  <th className="acciones"></th>
                </tr>
              </thead>
              <tbody>
                {medios.map((c) => (
                  <tr key={c.id}>
                    <td>{c.nombre}</td>
                    <td className="num tenue">{c.movimientos}</td>
                    <td className="acciones">
                      {!soloLectura && (
                        <>
                          <button className="secundario" onClick={() => setEditando(c)}>
                            Editar
                          </button>
                          <button className="peligro" onClick={() => eliminar(c)}>
                            Eliminar
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {editando && (
        <FormularioMedio
          medio={editando}
          onCerrar={() => setEditando(null)}
          onGuardado={async () => {
            setEditando(null)
            await recargar()
          }}
        />
      )}
    </>
  )
}

function FormularioMedio({ medio, onCerrar, onGuardado }) {
  const [nombre, setNombre] = useState(medio.nombre)
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  const esNuevo = medio.id === null

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      if (esNuevo) await mediosApi.crear(nombre)
      else await mediosApi.actualizar(medio.id, nombre)
      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={esNuevo ? 'Nuevo medio de pago' : 'Editar medio de pago'} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="nombre">Nombre</label>
        <input
          id="nombre"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
          placeholder="Ej: Transferencia"
          autoFocus
        />
        {campos.nombre && <span className="error-campo">{campos.nombre}</span>}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {guardando ? 'Guardando...' : 'Guardar'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
