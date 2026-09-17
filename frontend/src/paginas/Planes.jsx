import { useEffect, useState } from 'react'
import { negocioApi } from '../lib/api'
import { formatearMonto, montoAEntrada, entradaAMonto } from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'
import InputMonto from '../componentes/InputMonto'

// Los planes que le vendes a tus clientes: nombre, precio mensual, precio
// anual (opcional) y si incluyen el asistente con IA.
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
      // El PUT reemplaza el plan entero: se reenvían los precios y la IA tal
      // como están, para que desactivarlo no le cambie nada más.
      await negocioApi.actualizarPlan(plan.id, {
        nombre: plan.nombre,
        precio_mensual: plan.precio_mensual,
        precio_anual: plan.precio_anual,
        incluye_ia: plan.incluye_ia,
        activo: !plan.activo,
      })
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  const nuevo = { id: null, nombre: '', precio_mensual: '', precio_anual: '', incluye_ia: false, activo: true }

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Planes</h1>
        <button onClick={() => setEditando(nuevo)}>Nuevo plan</button>
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
                  <EtiquetaIA plan={p} />
                </div>
                <div className="par-cifras">
                  <div>
                    <span className="tenue">Mensual</span>
                    <strong>{formatearMonto(p.precio_mensual)}</strong>
                  </div>
                  <div>
                    <span className="tenue">Anual</span>
                    <strong>{p.precio_anual ? formatearMonto(p.precio_anual) : '—'}</strong>
                    <Ahorro plan={p} />
                  </div>
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
                  <th className="num">Mensual</th>
                  <th className="num">Anual</th>
                  <th>Asistente IA</th>
                  <th className="num">Clientes</th>
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
                    <td className="num">
                      {p.precio_anual ? formatearMonto(p.precio_anual) : <span className="tenue">—</span>}
                      <Ahorro plan={p} />
                    </td>
                    <td>
                      <EtiquetaIA plan={p} />
                    </td>
                    <td className="num tenue">{p.clientes}</td>
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

function EtiquetaIA({ plan }) {
  return plan.incluye_ia ? (
    <span className="etiqueta etiqueta-ia">✦ Con IA</span>
  ) : (
    <span className="etiqueta etiqueta-sin-ia">Sin IA</span>
  )
}

// Cuánto ahorra el cliente pagando por año frente a doce meses sueltos.
// El porcentaje es solo para mostrar: el dinero de verdad nunca se calcula
// aquí, se guarda tal cual lo escribió el administrador.
function porcentajeAhorro(mensual, anual) {
  const m = Number(mensual)
  const a = Number(anual)
  if (!m || !a) return null
  return Math.round((1 - a / (m * 12)) * 100)
}

function Ahorro({ plan }) {
  const pct = porcentajeAhorro(plan.precio_mensual, plan.precio_anual)
  if (pct === null || pct <= 0) return null
  return <div className="nota-ahorro">ahorra {pct}%</div>
}

function AccionesPlan({ plan, onEditar, onActivo, onEliminar }) {
  return (
    <div className="acciones-fila">
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
  // Los inputs muestran los montos con separadores de miles; se convierten al
  // formato crudo ("50000.00") justo antes de enviarlos.
  const [precio, setPrecio] = useState(() =>
    plan.precio_mensual ? montoAEntrada(plan.precio_mensual) : '',
  )
  const [conAnual, setConAnual] = useState(Boolean(plan.precio_anual))
  const [precioAnual, setPrecioAnual] = useState(() =>
    plan.precio_anual ? montoAEntrada(plan.precio_anual) : '',
  )
  const [incluyeIA, setIncluyeIA] = useState(Boolean(plan.incluye_ia))
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  const esNuevo = plan.id === null
  const mensualCrudo = entradaAMonto(precio)
  const anualCrudo = conAnual ? entradaAMonto(precioAnual) : ''

  // Referencia para quien arma el precio anual: lo que costarían doce meses.
  const doceMeses = Number(mensualCrudo) > 0 ? String(Number(mensualCrudo) * 12) : ''
  const ahorro = conAnual ? porcentajeAhorro(mensualCrudo, anualCrudo) : null

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})

    if (conAnual && !anualCrudo) {
      setCampos({ precio_anual: 'Escribe el precio anual o desmarca la opción' })
      return
    }

    setGuardando(true)
    try {
      const datos = {
        nombre,
        precio_mensual: mensualCrudo,
        precio_anual: anualCrudo,
        incluye_ia: incluyeIA,
      }
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
            próximo cobro que registres, y el cambio de IA, desde ya.
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

        <label className="checkbox">
          <input
            type="checkbox"
            checked={conAnual}
            onChange={(e) => setConAnual(e.target.checked)}
          />
          También se puede pagar por año
        </label>

        {conAnual && (
          <div className="bloque-opcion">
            <label htmlFor="precio-anual">
              Precio anual <span className="req">*</span>
            </label>
            <InputMonto id="precio-anual" valor={precioAnual} onCambio={setPrecioAnual} />
            {campos.precio_anual && <span className="error-campo">{campos.precio_anual}</span>}
            <p className="tenue ayuda-campo">
              {doceMeses && <>Doce meses sueltos serían {formatearMonto(doceMeses)}. </>}
              {ahorro !== null &&
                (ahorro > 0 ? (
                  <strong className="positivo">El cliente ahorra {ahorro}%.</strong>
                ) : ahorro < 0 ? (
                  <strong className="negativo">Ojo: sale más caro que pagar mes a mes.</strong>
                ) : (
                  'Mismo valor que pagar mes a mes.'
                ))}
            </p>
          </div>
        )}
        {!conAnual && campos.precio_anual && (
          <span className="error-campo">{campos.precio_anual}</span>
        )}

        <label className="interruptor">
          <input
            type="checkbox"
            checked={incluyeIA}
            onChange={(e) => setIncluyeIA(e.target.checked)}
          />
          <span className="interruptor-pista" aria-hidden="true" />
          <span>
            <strong>Incluye el asistente con IA</strong>
            <span className="tenue ayuda-campo">
              Los clientes de este plan pueden chatear con el asistente. Cada mensaje tiene un
              costo para ti.
            </span>
          </span>
        </label>

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
