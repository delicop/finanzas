import { useEffect, useState } from 'react'
import { categoriasApi } from '../lib/api'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'

export default function Categorias() {
  const [categorias, setCategorias] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [editando, setEditando] = useState(null) // null = modal cerrado
  const esMovil = useEsMovil()

  async function recargar() {
    setError('')
    try {
      setCategorias(await categoriasApi.listar())
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
  }, [])

  async function eliminar(categoria) {
    if (!confirm(`¿Eliminar la categoría "${categoria.nombre}"?`)) return
    try {
      await categoriasApi.eliminar(categoria.id)
      await recargar()
    } catch (err) {
      // El backend responde 409 si la categoría tiene movimientos.
      setError(err.message)
    }
  }

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Categorías</h1>
        <button onClick={() => setEditando({ id: null, nombre: '' })}>Nueva categoría</button>
      </div>

      {error && <div className="alerta">{error}</div>}

      <section className="tarjeta">
        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : categorias.length === 0 ? (
          <p className="tenue">Aún no hay categorías. Crea la primera para empezar a registrar movimientos.</p>
        ) : esMovil ? (
          // En teléfono, cada categoría es una tarjeta con los botones
          // grandes debajo, en vez de una fila apretada con scroll lateral.
          <div className="lista-movil">
            {categorias.map((c) => (
              <article className="tarjeta-cat" key={c.id}>
                <div className="tarjeta-cat-arriba">
                  <strong>{c.nombre}</strong>
                  <span className="tenue">
                    {c.movimientos} mov{c.movimientos === 1 ? '' : 's'}.
                  </span>
                </div>
                <div className="tarjeta-mov-acciones">
                  <button className="secundario" onClick={() => setEditando(c)}>
                    Editar
                  </button>
                  <button className="peligro" onClick={() => eliminar(c)}>
                    Eliminar
                  </button>
                </div>
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
                {categorias.map((c) => (
                  <tr key={c.id}>
                    <td>{c.nombre}</td>
                    <td className="num tenue">{c.movimientos}</td>
                    <td className="acciones">
                      <button className="secundario" onClick={() => setEditando(c)}>
                        Editar
                      </button>
                      <button className="peligro" onClick={() => eliminar(c)}>
                        Eliminar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {editando && (
        <FormularioCategoria
          categoria={editando}
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

function FormularioCategoria({ categoria, onCerrar, onGuardado }) {
  const [nombre, setNombre] = useState(categoria.nombre)
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  const esNueva = categoria.id === null

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      if (esNueva) await categoriasApi.crear(nombre)
      else await categoriasApi.actualizar(categoria.id, nombre)
      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={esNueva ? 'Nueva categoría' : 'Editar categoría'} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="nombre">Nombre</label>
        <input
          id="nombre"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
          placeholder="Ej: Negocio 1"
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
