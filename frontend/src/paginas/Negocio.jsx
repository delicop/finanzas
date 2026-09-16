import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { negocioApi } from '../lib/api'
import { formatearMonto, formatearFecha, hoyISO } from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import Modal from '../componentes/Modal'

// Mes actual en AAAA-MM. A mano y no con toISOString(): esa función convierte a
// UTC, y en Colombia (UTC-5) el primer día del mes puede salir como el mes
// anterior.
function mesActual() {
  const ahora = new Date()
  return `${ahora.getFullYear()}-${String(ahora.getMonth() + 1).padStart(2, '0')}`
}

function nombreDelMes(periodo) {
  const [anio, mes] = periodo.split('-')
  const nombres = [
    'enero', 'febrero', 'marzo', 'abril', 'mayo', 'junio',
    'julio', 'agosto', 'septiembre', 'octubre', 'noviembre', 'diciembre',
  ]
  return `${nombres[Number(mes) - 1]} de ${anio}`
}

function moverMes(periodo, meses) {
  const [anio, mes] = periodo.split('-').map(Number)
  // Date normaliza solo el desborde: mes 0 pasa a diciembre del año anterior.
  const d = new Date(anio, mes - 1 + meses, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

// El tablero del negocio: cuánto deberías facturar este mes, cuánto entró y a
// quién te falta cobrarle.
export default function Negocio() {
  const [periodo, setPeriodo] = useState(mesActual)
  const [resumen, setResumen] = useState(null)
  const [pagos, setPagos] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [cobrando, setCobrando] = useState(null) // el cliente pendiente elegido
  const esMovil = useEsMovil()

  async function recargar(mes = periodo) {
    setError('')
    try {
      const [datos, listaPagos] = await Promise.all([
        negocioApi.resumen(mes),
        negocioApi.listarPagos(mes),
      ])
      setResumen(datos)
      setPagos(listaPagos)
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar(periodo)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [periodo])

  function cambiarMes(delta) {
    setCargando(true)
    setPeriodo((p) => moverMes(p, delta))
  }

  async function deshacerPago(pago) {
    if (!confirm(`¿Deshacer el cobro de ${pago.cliente_email} de ${nombreDelMes(pago.periodo)}?`))
      return
    try {
      await negocioApi.eliminarPago(pago.id)
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <h1>Negocio</h1>
          <p className="subtitulo">{nombreDelMes(periodo)}</p>
        </div>
        <div className="paginacion-botones">
          <button className="secundario" onClick={() => cambiarMes(-1)}>
            ← Mes anterior
          </button>
          <button
            className="secundario"
            onClick={() => cambiarMes(1)}
            disabled={periodo >= mesActual()}
          >
            Mes siguiente →
          </button>
        </div>
      </div>

      {error && <div className="alerta">{error}</div>}

      {cargando || !resumen ? (
        <p className="tenue">Cargando...</p>
      ) : (
        <>
          <div className="fila-tarjetas">
            <article className="metrica">
              <span>Esperado</span>
              <strong className="monto-grande">{formatearMonto(resumen.esperado)}</strong>
              <span className="metrica-nota">
                {resumen.clientes_con_plan} cliente
                {resumen.clientes_con_plan === 1 ? '' : 's'} con plan
              </span>
            </article>

            <article className="metrica destacada">
              <span>Cobrado</span>
              <strong className="monto-grande positivo">
                {formatearMonto(resumen.cobrado)}
              </strong>
              <span className="metrica-nota">
                {resumen.clientes_pagaron} de {resumen.clientes_con_plan} ya pagaron
              </span>
            </article>

            <article className="metrica">
              <span>Por cobrar</span>
              <strong className={`monto-grande ${Number(resumen.pendiente) > 0 ? 'advertencia' : ''}`}>
                {formatearMonto(resumen.pendiente)}
              </strong>
              <span className="metrica-nota">
                {resumen.pendientes.length} sin pagar este mes
              </span>
            </article>
          </div>

          {resumen.clientes_con_plan === 0 && (
            <div className="alerta aviso">
              Ningún cliente tiene plan asignado todavía. Crea los planes en{' '}
              <Link to="/admin/planes">Planes</Link> y asígnalos desde{' '}
              <Link to="/admin/clientes">Clientes</Link>.
            </div>
          )}

          <section className="tarjeta">
            <h2 className="subtitulo">Por cobrar</h2>
            {resumen.pendientes.length === 0 ? (
              <p className="tenue">
                {resumen.clientes_con_plan > 0
                  ? 'Todos al día este mes. '
                  : 'Nada por cobrar. '}
              </p>
            ) : (
              <div className="lista-movil">
                {resumen.pendientes.map((p) => (
                  <article className="tarjeta-cat" key={p.usuario_id}>
                    <div className="tarjeta-cat-arriba">
                      <strong>{p.nombre || p.email}</strong>
                      <span className="saldo-fuerte advertencia">{formatearMonto(p.monto)}</span>
                    </div>
                    <div className="tenue sub">
                      {p.email} · {p.plan_nombre}
                    </div>
                    <div className="tarjeta-mov-acciones">
                      <button onClick={() => setCobrando(p)}>Registrar pago</button>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </section>

          <section className="tarjeta">
            <h2 className="subtitulo">Cobros del mes</h2>
            {pagos.length === 0 ? (
              <p className="tenue">Todavía no has registrado ningún cobro de este mes.</p>
            ) : esMovil ? (
              <div className="lista-movil">
                {pagos.map((g) => (
                  <article className="tarjeta-cat" key={g.id}>
                    <div className="tarjeta-cat-arriba">
                      <strong>{g.cliente_email}</strong>
                      <span className="saldo-fuerte positivo">{formatearMonto(g.monto)}</span>
                    </div>
                    <div className="tenue sub">
                      {g.plan_nombre} · pagado el {formatearFecha(g.pagado_en)}
                      {g.nota && <> · {g.nota}</>}
                    </div>
                    <div className="tarjeta-mov-acciones">
                      <button className="peligro" onClick={() => deshacerPago(g)}>
                        Deshacer
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
                      <th>Cliente</th>
                      <th>Plan</th>
                      <th>Pagado el</th>
                      <th>Nota</th>
                      <th className="num">Monto</th>
                      <th className="acciones"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagos.map((g) => (
                      <tr key={g.id}>
                        {/* usuario_id null = el cliente fue eliminado. El cobro
                            se conserva: la plata que entró ese mes entró. */}
                        <td>
                          {g.cliente_email}
                          {g.usuario_id === null && (
                            <span className="etiqueta tipo-pague">cliente eliminado</span>
                          )}
                        </td>
                        <td className="tenue">{g.plan_nombre}</td>
                        <td className="tenue nowrap">{formatearFecha(g.pagado_en)}</td>
                        <td className="tenue">{g.nota}</td>
                        <td className="num positivo">{formatearMonto(g.monto)}</td>
                        <td className="acciones">
                          <button className="menor menor-peligro" onClick={() => deshacerPago(g)}>
                            Deshacer
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="tarjeta">
            <h2 className="subtitulo">Por plan</h2>
            <div className="tabla-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Plan</th>
                    <th className="num">Precio</th>
                    <th className="num">Clientes</th>
                    <th className="num">Esperado</th>
                    <th className="num">Cobrado</th>
                  </tr>
                </thead>
                <tbody>
                  {resumen.por_plan.map((p) => (
                    <tr key={p.plan_id}>
                      <td>{p.nombre}</td>
                      <td className="num tenue">{formatearMonto(p.precio_mensual)}</td>
                      <td className="num tenue">{p.clientes}</td>
                      <td className="num">{formatearMonto(p.esperado)}</td>
                      <td className="num positivo">{formatearMonto(p.cobrado)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}

      {cobrando && (
        <FormularioPago
          cliente={cobrando}
          periodo={periodo}
          onCerrar={() => setCobrando(null)}
          onGuardado={async () => {
            setCobrando(null)
            await recargar()
          }}
        />
      )}
    </>
  )
}

// El monto NO se pide en el formulario por defecto.
//
// El backend lo lee del plan en la base, así que el caso normal (cobrar lo que
// vale el plan) es un solo botón y no hay forma de teclear mal una cifra. Solo
// si marcas "cobré otra cantidad" se abre el campo, para un descuento puntual o
// un mes a medias.
function FormularioPago({ cliente, periodo, onCerrar, onGuardado }) {
  const [pagadoEn, setPagadoEn] = useState(hoyISO)
  const [otroMonto, setOtroMonto] = useState(false)
  const [monto, setMonto] = useState('')
  const [nota, setNota] = useState('')
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)
    try {
      await negocioApi.registrarPago({
        usuario_id: cliente.usuario_id,
        periodo,
        monto: otroMonto ? monto : '',
        pagado_en: pagadoEn,
        nota,
      })
      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={`Cobro de ${cliente.nombre || cliente.email}`} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <p className="tenue">
          Plan <strong>{cliente.plan_nombre}</strong> · {nombreDelMes(periodo)}
        </p>

        <label htmlFor="pagado_en">Fecha del pago</label>
        <input
          id="pagado_en"
          type="date"
          value={pagadoEn}
          onChange={(e) => setPagadoEn(e.target.value)}
        />
        {campos.pagado_en && <span className="error-campo">{campos.pagado_en}</span>}

        <label className="checkbox">
          <input
            type="checkbox"
            checked={otroMonto}
            onChange={(e) => setOtroMonto(e.target.checked)}
          />
          Cobré una cantidad distinta a {formatearMonto(cliente.monto)}
        </label>

        {otroMonto && (
          <>
            <label htmlFor="monto">Monto cobrado</label>
            <input
              id="monto"
              value={monto}
              onChange={(e) => setMonto(e.target.value)}
              placeholder="Ej: 25000"
              inputMode="decimal"
            />
            {campos.monto && <span className="error-campo">{campos.monto}</span>}
          </>
        )}

        <label htmlFor="nota">Nota</label>
        <input
          id="nota"
          value={nota}
          onChange={(e) => setNota(e.target.value)}
          placeholder="Opcional: por dónde te pagó, un descuento…"
        />
        {campos.nota && <span className="error-campo">{campos.nota}</span>}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {guardando ? 'Guardando...' : 'Registrar pago'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
