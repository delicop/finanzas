import { useEffect, useState } from 'react'
import { categoriasApi, tiendasApi } from '../lib/api'
import { useAuth } from '../lib/AuthContext'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'
import Distintivo from '../componentes/Distintivo'
import Icono from '../componentes/Icono'
import Vacio from '../componentes/Vacio'

// Aquí se llevan las dos listas con las que se etiqueta la plata: las
// categorías (de qué es) y, para quien tenga tiendas en su plan, sus locales.
// Van juntas porque son la misma tarea — dejar preparado con qué se registra —
// y separarlas obligaba a acordarse de dos pantallas distintas.
export default function Categorias() {
  const [categorias, setCategorias] = useState([])
  const [tiendas, setTiendas] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [editando, setEditando] = useState(null) // null = modal cerrado
  const [editandoTienda, setEditandoTienda] = useState(null)
  const esMovil = useEsMovil()
  // Mirando la cuenta de otro no se ofrece nada que escriba: el backend
  // rechaza cualquier método que no sea GET mientras dura la revisión.
  const { soloLectura, conTiendas } = useAuth()

  async function recargar() {
    setError('')
    try {
      setCategorias(await categoriasApi.listar())
      // Sin tiendas en el plan ni se piden: la ruta responde 403.
      if (conTiendas) setTiendas(await tiendasApi.listar())
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  async function eliminarTienda(tienda) {
    if (!confirm(`¿Eliminar la tienda "${tienda.nombre}"?`)) return
    try {
      await tiendasApi.eliminar(tienda.id)
      await recargar()
    } catch (err) {
      // El backend responde 409 si la tienda ya tiene cierres.
      setError(err.message)
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
          <div className="acciones-encabezado">
            {conTiendas && (
              <button
                className="secundario"
                onClick={() => setEditandoTienda({ id: null, nombre: '' })}
              >
                Nueva tienda
              </button>
            )}
            <button onClick={() => setEditando({ id: null, nombre: '' })}>Nueva categoría</button>
          </div>
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

      {/* Los locales, para quien los tenga. La lista va aquí, al lado de las
          categorías, y cómo VA cada uno se ve en la sección Tiendas. */}
      {conTiendas && !cargando && tiendas.length > 0 && (
        <section className="tarjeta">
          <div className="encabezado-seccion">
            <h2>Tiendas</h2>
            <span className="tenue">Tus locales</span>
          </div>

          <ul className="lista-compacta">
            {tiendas.map((t) => (
              <li className="fila-compacta" key={t.id}>
                <Distintivo nombre={t.nombre} clase="medio" grande />
                <div className="fila-compacta-texto">
                  <strong>{t.nombre}</strong>
                  <span className="tenue">
                    {t.cierres} cierre{t.cierres === 1 ? '' : 's'}
                  </span>
                </div>
                {!soloLectura && (
                  <div className="fila-compacta-acciones">
                    <button
                      className="boton-tema"
                      onClick={() => setEditandoTienda(t)}
                      title={`Editar ${t.nombre}`}
                      aria-label={`Editar ${t.nombre}`}
                    >
                      <Icono nombre="editar" tamano={18} />
                    </button>
                    <button
                      className="boton-tema boton-eliminar"
                      onClick={() => eliminarTienda(t)}
                      title={`Eliminar ${t.nombre}`}
                      aria-label={`Eliminar ${t.nombre}`}
                    >
                      <Icono nombre="eliminar" tamano={18} />
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      {editandoTienda && (
        <FormularioTienda
          tienda={editandoTienda}
          onCerrar={() => setEditandoTienda(null)}
          onGuardado={async () => {
            setEditandoTienda(null)
            await recargar()
          }}
        />
      )}

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

// El mismo formulario de una sola casilla que las categorías, pero para los
// locales. Vive aquí porque aquí es donde se crean.
function FormularioTienda({ tienda, onCerrar, onGuardado }) {
  const [nombre, setNombre] = useState(tienda.nombre)
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  const esNueva = tienda.id === null

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      if (esNueva) await tiendasApi.crear(nombre)
      else await tiendasApi.actualizar(tienda.id, nombre)
      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={esNueva ? 'Nueva tienda' : 'Editar tienda'} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="nombre-tienda">Nombre</label>
        <input
          id="nombre-tienda"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
          placeholder="Ej: Tienda del centro"
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
