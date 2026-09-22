import { useEffect, useState } from 'react'
import { categoriasApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'
import Distintivo from '../componentes/Distintivo'
import Icono from '../componentes/Icono'
import Vacio from '../componentes/Vacio'

export default function Categorias() {
  const [categorias, setCategorias] = useState([])
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
        <div>
          <span className="kicker">¿De qué es esta plata?</span>
          <h1>Categorías</h1>
        </div>
        {!soloLectura && (
          <button onClick={() => setEditando({ id: null, nombre: '' })}>Nueva categoría</button>
        )}
      </div>

      {error && <div className="alerta">{error}</div>}

      <section className="tarjeta">
        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : categorias.length === 0 ? (
          <Vacio
            icono="categorias"
            titulo="Aún no hay categorías"
            accion={
              !soloLectura && (
                <button onClick={() => setEditando({ id: null, nombre: '' })}>Crear la primera</button>
              )
            }
          >
            Son los grupos de tu plata: mercado, arriendo, sueldo. Crea al menos una para poder
            registrar movimientos.
          </Vacio>
        ) : esMovil ? (
          // En teléfono, una fila por categoría: el nombre a la izquierda y los
          // botones de ícono a la derecha. Antes era una tarjeta con "Editar" y
          // "Eliminar" apilados debajo, y con diez ya había que bajar un buen rato.
          <ul className="lista-compacta">
            {categorias.map((c) => (
              <li className="fila-compacta" key={c.id}>
                <Distintivo nombre={c.nombre} grande />
                <div className="fila-compacta-texto">
                  <strong>{c.nombre}</strong>
                  <span className="tenue">
                    {c.movimientos} movimiento{c.movimientos === 1 ? '' : 's'}
                  </span>
                </div>
                {!soloLectura && (
                  <div className="fila-compacta-acciones">
                    <button
                      className="boton-tema"
                      onClick={() => setEditando(c)}
                      title={`Editar ${c.nombre}`}
                      aria-label={`Editar ${c.nombre}`}
                    >
                      <Icono nombre="editar" tamano={18} />
                    </button>
                    <button
                      className="boton-tema boton-eliminar"
                      onClick={() => eliminar(c)}
                      title={`Eliminar ${c.nombre}`}
                      aria-label={`Eliminar ${c.nombre}`}
                    >
                      <Icono nombre="eliminar" tamano={18} />
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
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
                    <td>
                      <span className="con-distintivo">
                        <Distintivo nombre={c.nombre} />
                        {c.nombre}
                      </span>
                    </td>
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
