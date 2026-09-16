import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { categoriasApi, mediosApi, movimientosApi } from '../lib/api'
import {
  ETIQUETAS_ESTADO,
  ETIQUETAS_TIPO,
  estiloMonto,
  formatearFecha,
  formatearMonto,
} from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import { useAuth } from '../lib/AuthContext'
import MovimientoForm from '../componentes/MovimientoForm'
import VisorFactura from '../componentes/VisorFactura'
import ModalCobro from '../componentes/ModalCobro'

const POR_PAGINA = 50

export default function Movimientos() {
  // Los filtros viven en la URL (?tipo=preste&categoria_id=2) para que la
  // vista se pueda compartir, marcar y recargar sin perder el estado.
  const [params, setParams] = useSearchParams()
  const esMovil = useEsMovil()
  const { soloLectura } = useAuth()

  const [movimientos, setMovimientos] = useState([])
  const [total, setTotal] = useState(0)
  const [categorias, setCategorias] = useState([])
  const [medios, setMedios] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [editando, setEditando] = useState(null)
  const [viendoFactura, setViendoFactura] = useState(null)
  const [filtrosAbiertos, setFiltrosAbiertos] = useState(false)
  // Id del movimiento cuyo estado se está guardando, para desactivar su botón
  // y que un doble toque no mande dos peticiones.
  const [cambiandoEstado, setCambiandoEstado] = useState(null)
  // Préstamo que se está marcando como pagado (abre el modal del medio de cobro).
  const [cobrando, setCobrando] = useState(null)

  const filtros = {
    categoria_id: params.get('categoria_id') ?? '',
    medio_pago_id: params.get('medio_pago_id') ?? '',
    tipo: params.get('tipo') ?? '',
    estado: params.get('estado') ?? '',
    desde: params.get('desde') ?? '',
    hasta: params.get('hasta') ?? '',
    q: params.get('q') ?? '',
  }
  const pagina = Number(params.get('pagina') ?? 1)
  const filtrosActivos = Object.values(filtros).filter(Boolean).length

  // useCallback + la dependencia de params: la lista se recarga sola cada vez
  // que cambia un filtro en la URL.
  const cargar = useCallback(async () => {
    setCargando(true)
    setError('')
    try {
      const datos = await movimientosApi.listar({
        ...filtros,
        limite: POR_PAGINA,
        offset: (pagina - 1) * POR_PAGINA,
      })
      setMovimientos(datos.movimientos)
      setTotal(datos.total)
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params])

  useEffect(() => {
    cargar()
  }, [cargar])

  useEffect(() => {
    categoriasApi.listar().then(setCategorias).catch(() => {})
    mediosApi.listar().then(setMedios).catch(() => {})
  }, [])

  function cambiarFiltro(clave, valor) {
    const nuevos = new URLSearchParams(params)
    if (valor) nuevos.set(clave, valor)
    else nuevos.delete(clave)
    nuevos.delete('pagina') // cualquier cambio de filtro vuelve a la página 1
    setParams(nuevos)
  }

  function irAPagina(n) {
    const nuevos = new URLSearchParams(params)
    if (n <= 1) nuevos.delete('pagina')
    else nuevos.set('pagina', String(n))
    setParams(nuevos)
  }

  async function eliminar(m) {
    if (!confirm(`¿Eliminar este movimiento de ${formatearMonto(m.monto)}?`)) return
    try {
      await movimientosApi.eliminar(m.id)
      await cargar()
    } catch (err) {
      setError(err.message)
    }
  }

  // Marcar pagado / pendiente desde la lista, sin abrir el formulario.
  //
  // Marcar PAGADO abre antes un modal para saber por dónde te pagaron:
  // puede ser un medio distinto al que usaste para prestar.
  // Volver a PENDIENTE no pregunta nada: la plata simplemente no ha vuelto.
  function alternarEstado(m) {
    if (m.estado === 'pagado') {
      guardarEstado(m, 'pendiente', 0)
      return
    }
    setCobrando(m)
  }

  async function guardarEstado(m, estado, medioCobroID) {
    setCambiandoEstado(m.id)
    setError('')
    try {
      const actualizado = await movimientosApi.cambiarEstado(m.id, estado, medioCobroID)
      // Reemplazamos solo esa fila en vez de recargar toda la lista:
      // el cambio se ve al instante y no se pierde el scroll.
      setMovimientos((prev) => prev.map((x) => (x.id === m.id ? actualizado : x)))
      setCobrando(null)
    } catch (err) {
      // Si el modal está abierto, el error se muestra ahí; si no, arriba.
      if (estado === 'pagado' && cobrando) throw err
      setError(err.message)
    } finally {
      setCambiandoEstado(null)
    }
  }

  const totalPaginas = Math.max(1, Math.ceil(total / POR_PAGINA))
  const hayFiltros = filtrosActivos > 0

  const acciones = {
    onEditar: setEditando,
    onEliminar: eliminar,
    // Revisando la cuenta de otro, la lista se vuelve un informe: se puede
    // abrir una factura, pero no marcar pagado, ni editar, ni borrar.
    soloLectura,
    onVerFactura: setViendoFactura,
    onAlternarEstado: alternarEstado,
    cambiandoEstado,
  }

  return (
    <>
      <div className="encabezado-pagina">
        <h1>Movimientos</h1>
        {!soloLectura && (
          <button onClick={() => setEditando({})} disabled={categorias.length === 0}>
            Nuevo movimiento
          </button>
        )}
      </div>

      {categorias.length === 0 && !cargando && (
        <div className="alerta aviso">Crea una categoría antes de registrar movimientos.</div>
      )}

      <section className="tarjeta filtros">
        {/* En el teléfono seis filtros abiertos empujan la lista fuera de la
            pantalla, así que arrancan plegados con un contador de activos. */}
        {esMovil && (
          <button
            className="secundario boton-filtros"
            onClick={() => setFiltrosAbiertos((v) => !v)}
            aria-expanded={filtrosAbiertos}
          >
            Filtros
            {filtrosActivos > 0 && <span className="contador">{filtrosActivos}</span>}
            <span className="flecha">{filtrosAbiertos ? '▲' : '▼'}</span>
          </button>
        )}

        {(!esMovil || filtrosAbiertos) && (
          <div className="fila-filtros">
            <div>
              <label htmlFor="f-categoria">Categoría</label>
              <select
                id="f-categoria"
                value={filtros.categoria_id}
                onChange={(e) => cambiarFiltro('categoria_id', e.target.value)}
              >
                <option value="">Todas</option>
                {categorias.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.nombre}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label htmlFor="f-tipo">Tipo</label>
              <select
                id="f-tipo"
                value={filtros.tipo}
                onChange={(e) => cambiarFiltro('tipo', e.target.value)}
              >
                <option value="">Todos</option>
                <option value="recibi">Recibí</option>
                <option value="pague">Pagué</option>
                <option value="preste">Presté</option>
              </select>
            </div>

            <div>
              <label htmlFor="f-medio">Medio de pago</label>
              <select
                id="f-medio"
                value={filtros.medio_pago_id}
                onChange={(e) => cambiarFiltro('medio_pago_id', e.target.value)}
              >
                <option value="">Todos</option>
                {medios.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.nombre}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label htmlFor="f-estado">Estado</label>
              <select
                id="f-estado"
                value={filtros.estado}
                onChange={(e) => cambiarFiltro('estado', e.target.value)}
              >
                <option value="">Todos</option>
                <option value="pendiente">Pendiente</option>
                <option value="pagado">Pagado</option>
              </select>
            </div>

            <div>
              <label htmlFor="f-desde">Desde</label>
              <input
                id="f-desde"
                type="date"
                value={filtros.desde}
                onChange={(e) => cambiarFiltro('desde', e.target.value)}
              />
            </div>

            <div>
              <label htmlFor="f-hasta">Hasta</label>
              <input
                id="f-hasta"
                type="date"
                value={filtros.hasta}
                onChange={(e) => cambiarFiltro('hasta', e.target.value)}
              />
            </div>

            <div className="crece">
              <label htmlFor="f-q">Buscar</label>
              <input
                id="f-q"
                value={filtros.q}
                onChange={(e) => cambiarFiltro('q', e.target.value)}
                placeholder="Descripción o persona"
              />
            </div>

            {hayFiltros && (
              <div className="filtros-limpiar">
                <button className="secundario" onClick={() => setParams(new URLSearchParams())}>
                  Limpiar filtros
                </button>
              </div>
            )}
          </div>
        )}
      </section>

      {error && <div className="alerta">{error}</div>}

      <section className={esMovil ? '' : 'tarjeta'}>
        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : movimientos.length === 0 ? (
          <p className="tenue">
            {hayFiltros ? 'Ningún movimiento coincide con los filtros.' : 'Aún no hay movimientos.'}
          </p>
        ) : (
          <>
            {esMovil ? (
              <div className="lista-movil">
                {movimientos.map((m) => (
                  <TarjetaMovimiento key={m.id} m={m} {...acciones} />
                ))}
              </div>
            ) : (
              <div className="tabla-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Fecha</th>
                      <th>Tipo</th>
                      <th>Categoría</th>
                      <th>Descripción</th>
                      <th className="num">Monto</th>
                      <th className="acciones"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {movimientos.map((m) => (
                      <FilaMovimiento key={m.id} m={m} {...acciones} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            <div className="paginacion">
              <span className="tenue">
                {total} movimiento{total === 1 ? '' : 's'}
              </span>
              {totalPaginas > 1 && (
                <div className="paginacion-botones">
                  <button
                    className="secundario"
                    disabled={pagina <= 1}
                    onClick={() => irAPagina(pagina - 1)}
                  >
                    Anterior
                  </button>
                  <span className="tenue">
                    {pagina} / {totalPaginas}
                  </span>
                  <button
                    className="secundario"
                    disabled={pagina >= totalPaginas}
                    onClick={() => irAPagina(pagina + 1)}
                  >
                    Siguiente
                  </button>
                </div>
              )}
            </div>
          </>
        )}
      </section>

      {editando && (
        <MovimientoForm
          movimiento={editando}
          categorias={categorias}
          medios={medios}
          onCerrar={() => setEditando(null)}
          onGuardado={async () => {
            setEditando(null)
            await cargar()
          }}
        />
      )}

      {viendoFactura && (
        <VisorFactura movimiento={viendoFactura} onCerrar={() => setViendoFactura(null)} />
      )}

      {cobrando && (
        <ModalCobro
          movimiento={cobrando}
          medios={medios}
          onCerrar={() => setCobrando(null)}
          onConfirmar={(medioCobroID) => guardarEstado(cobrando, 'pagado', medioCobroID)}
        />
      )}
    </>
  )
}

/* ---------------------------------------------------------------------------
 * Piezas compartidas entre la tabla (escritorio) y las tarjetas (teléfono).
 * La LÓGICA vive aquí una sola vez; lo único que se repite abajo es el
 * acomodo visual, que es justamente lo que tiene que ser distinto.
 * ------------------------------------------------------------------------ */

function Monto({ m }) {
  const { signo, clase } = estiloMonto(m)
  return (
    <span className={`monto-valor ${clase}`}>
      {signo} {formatearMonto(m.monto)}
    </span>
  )
}

// Acciones PRINCIPALES: las que se usan seguido y valen un botón grande.
// "Marcar pagado" (solo si el préstamo está pendiente) y "Factura".
function AccionesPrincipales({ m, onVerFactura, onAlternarEstado, cambiandoEstado, soloLectura }) {
  const pendiente = m.tipo === 'preste' && m.estado === 'pendiente' && !soloLectura
  const ocupado = cambiandoEstado === m.id

  if (!pendiente && !m.factura) return null

  return (
    <div className="acciones-principales">
      {pendiente && (
        <button
          className="principal-pagar"
          disabled={ocupado}
          onClick={() => onAlternarEstado(m)}
          title="Marcar que ya te pagaron"
        >
          {ocupado ? 'Guardando...' : '✓ Marcar pagado'}
        </button>
      )}
      {m.factura && (
        <button className="principal-factura" onClick={() => onVerFactura(m)}>
          Factura
        </button>
      )}
    </div>
  )
}

// Acciones SECUNDARIAS: se usan poco, así que van discretas a un lado.
// "Marcar pendiente" vive aquí porque es deshacer, no la acción del día a día.
function AccionesSecundarias({ m, onEditar, onEliminar, onAlternarEstado, cambiandoEstado, soloLectura }) {
  const yaPagado = m.tipo === 'preste' && m.estado === 'pagado'
  const ocupado = cambiandoEstado === m.id

  // Sin acciones que ofrecer, el contenedor tampoco: si no, quedan huecos
  // vacíos alineando las filas de una lista que solo se está mirando.
  if (soloLectura) return null

  return (
    <div className="acciones-secundarias">
      {yaPagado && (
        <button className="menor" disabled={ocupado} onClick={() => onAlternarEstado(m)}>
          {ocupado ? '...' : 'Marcar pendiente'}
        </button>
      )}
      <button className="menor" onClick={() => onEditar(m)}>
        Editar
      </button>
      <button className="menor menor-peligro" onClick={() => onEliminar(m)}>
        Eliminar
      </button>
    </div>
  )
}

function DetallePrestamo({ m }) {
  if (m.tipo !== 'preste') return null
  return (
    <div className="sub">
      {m.a_quien}
      <span className={`estado estado-${m.estado}`}>{ETIQUETAS_ESTADO[m.estado]}</span>
      {m.medio_cobro_nombre && (
        <span className="cobrado-por">te pagó por {m.medio_cobro_nombre}</span>
      )}
    </div>
  )
}

/* ----------------------------- escritorio ------------------------------- */

function FilaMovimiento(props) {
  const { m } = props
  return (
    <tr>
      <td className="nowrap">{formatearFecha(m.fecha)}</td>
      <td>
        <span className={`etiqueta tipo-${m.tipo}`}>{ETIQUETAS_TIPO[m.tipo]}</span>
      </td>
      <td>
        {m.categoria_nombre}
        {m.medio_pago_nombre && <div className="sub-medio">{m.medio_pago_nombre}</div>}
      </td>
      <td>
        {m.descripcion || <span className="tenue">—</span>}
        <DetallePrestamo m={m} />
      </td>
      <td className="num nowrap">
        <Monto m={m} />
      </td>
      <td className="acciones">
        <div className="celda-acciones">
          <AccionesPrincipales {...props} />
          <AccionesSecundarias {...props} />
        </div>
      </td>
    </tr>
  )
}

/* ------------------------------- teléfono ------------------------------- */

function TarjetaMovimiento(props) {
  const { m } = props
  return (
    <article className="tarjeta-mov">
      <div className="tarjeta-mov-arriba">
        <span className={`etiqueta tipo-${m.tipo}`}>{ETIQUETAS_TIPO[m.tipo]}</span>
        <span className="tenue fecha">{formatearFecha(m.fecha)}</span>
      </div>

      <p className="tarjeta-mov-desc">
        {m.descripcion || <span className="tenue">Sin descripción</span>}
      </p>

      <div className="tarjeta-mov-meta">
        <span className="tenue">
          {m.categoria_nombre}
          {m.medio_pago_nombre && <span className="sub-medio"> · {m.medio_pago_nombre}</span>}
        </span>
        <Monto m={m} />
      </div>

      <DetallePrestamo m={m} />

      <div className="tarjeta-mov-acciones">
        <AccionesPrincipales {...props} />
        <AccionesSecundarias {...props} />
      </div>
    </article>
  )
}
