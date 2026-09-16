import { useEffect, useState } from 'react'
import { negocioApi } from '../lib/api'
import { formatearMonto, montoAEntrada, entradaAMonto } from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'
import InputMonto from '../componentes/InputMonto'

// Los planes que le vendes a tus clientes: nombre y precio mensual.
export default function Planes() {
  const [planes, setPlanes] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [editando, setEditando] = useState(null) // null = modal cerrado
  const esMovil = useEsMovil()

  async function recargar() {
    setError('')
    try {
      setPlanes(await negocioApi.listarPlanes())
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
  }, [])

  async function eliminar(plan) {
    if (!confirm(`¿Eliminar el plan "${plan.nombre}"?`)) return
    try {
      await negocioApi.eliminarPlan(plan.id)
      await recargar()
    } catch (err) {
      // 409 si el plan tiene clientes: el backend explica qué hacer.
      setError(err.message)
    }
  }

  async function alternarActivo(plan) {
    setError('')
    try {
      await negocioApi.actualizarPlan(plan.id, {
        nombre: plan.nombre,
        precio_mensual: plan.precio_mensual,
        activo: !plan.activo,
      })
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Planes</h1>
        <button onClick={() => setEditando({ id: null, nombre: '', precio_mensual: '' })}>
          Nuevo plan
        </button>
      </div>

      {error && <div className="alerta">{error}</div>}

      <div className="alerta aviso">
        Cambiar el precio de un plan afecta lo que <strong>vas a cobrar</strong> de aquí en
        adelante. Los pagos ya registrados guardan su propio monto y no se tocan: lo que
        cobraste en marzo sigue siendo lo que cobraste en marzo.
      </div>

      <section className="tarjeta">
        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : planes.length === 0 ? (
          <p className="tenue">
            Aún no hay planes. Crea el primero para poder asignárselo a tus clientes y
            empezar a llevar los cobros.
          </p>
        ) : esMovil ? (
          <div className="lista-movil">
            {planes.map((p) => (
              <article className="tarjeta-cat" key={p.id}>
                <div className="tarjeta-cat-arriba">
                  <strong>
                    {p.nombre} {!p.activo && <span className="etiqueta tipo-pague">Inactivo</span>}
                  </strong>
                  <span className="saldo-fuerte">{formatearMonto(p.precio_mensual)}</span>
                </div>
                <div className="tenue sub">
                  {p.clientes} cliente{p.clientes === 1 ? '' : 's'}
                </div>
                <AccionesPlan
                  plan={p}
                  onEditar={() => setEditando(p)}
                  onActivo={() => alternarActivo(p)}
                  onEliminar={() => eliminar(p)}
                />
              </article>
            ))}
          </div>
        ) : (
          <div className="tabla-scroll">
            <table>
              <thead>
                <tr>
                  <th>Plan</th>
                  <th className="num">Precio mensual</th>
                  <th className="num">Clientes</th>
                  <th className="num">Suma al mes</th>
                  <th className="acciones"></th>
                </tr>
              </thead>
              <tbody>
                {planes.map((p) => (
                  <tr key={p.id} className={p.activo ? '' : 'fila-inactiva'}>
                    <td>
                      {p.nombre}{' '}
                      {!p.activo && <span className="etiqueta tipo-pague">Inactivo</span>}
                    </td>
                    <td className="num">{formatearMonto(p.precio_mensual)}</td>
                    <td className="num tenue">{p.clientes}</td>
                    <td className="num saldo-fuerte">
                      {formatearMonto(String(Number(p.precio_mensual) * p.clientes))}
                    </td>
                    <td className="acciones">
                      <AccionesPlan
                        plan={p}
                        onEditar={() => setEditando(p)}
                        onActivo={() => alternarActivo(p)}
                        onEliminar={() => eliminar(p)}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {editando && (
        <FormularioPlan
          plan={editando}
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

function AccionesPlan({ plan, onEditar, onActivo, onEliminar }) {
  return (
    <div className="tarjeta-mov-acciones">
      <button className="secundario" onClick={onEditar}>
        Editar
      </button>
      <button className="secundario" onClick={onActivo}>
        {plan.activo ? 'Desactivar' : 'Activar'}
      </button>
      {/* Borrar solo tiene sentido en un plan que nadie usa. Con clientes
          dentro, el backend responde 409 y lo correcto es desactivarlo:
          deja de ofrecerse sin tocar a los que ya lo tienen. */}
      <button className="peligro" onClick={onEliminar} disabled={plan.clientes > 0}>
        Eliminar
      </button>
    </div>
  )
}

function FormularioPlan({ plan, onCerrar, onGuardado }) {
  const [nombre, setNombre] = useState(plan.nombre)
  // El input muestra el monto con separadores de miles; se convierte al
  // formato crudo ("50000.00") justo antes de enviarlo.
  const [precio, setPrecio] = useState(() =>
    plan.precio_mensual ? montoAEntrada(plan.precio_mensual) : '',
  )
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  const esNuevo = plan.id === null

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      const datos = { nombre, precio_mensual: entradaAMonto(precio) }
      if (esNuevo) await negocioApi.crearPlan(datos)
      else await negocioApi.actualizarPlan(plan.id, { ...datos, activo: plan.activo })
      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={esNuevo ? 'Nuevo plan' : 'Editar plan'} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        {!esNuevo && plan.clientes > 0 && (
          <div className="alerta aviso">
            {plan.clientes} cliente{plan.clientes === 1 ? '' : 's'} tiene
            {plan.clientes === 1 ? '' : 'n'} este plan. El precio nuevo aplica desde el
            próximo cobro que registres.
          </div>
        )}

        <label htmlFor="nombre">
          Nombre <span className="req">*</span>
        </label>
        <input
          id="nombre"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
          placeholder="Ej: Básico"
          autoFocus
        />
        {campos.nombre && <span className="error-campo">{campos.nombre}</span>}

        <label htmlFor="precio">
          Precio mensual <span className="req">*</span>
        </label>
        <InputMonto id="precio" valor={precio} onCambio={setPrecio} />
        {campos.precio_mensual && <span className="error-campo">{campos.precio_mensual}</span>}

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
