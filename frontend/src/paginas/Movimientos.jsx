import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { categoriasApi, dashboardApi, mediosApi, movimientosApi } from '../lib/api'
import {
  ETIQUETAS_ESTADO,
  enlaceWhatsApp,
  esDeuda,
  esDeudaPropia,
  estiloMonto,
  formatearFecha,
  formatearFechaCorta,
  formatearMonto,
  mensajeDeCobro,
  nombreCategoria,
  rutaTraslado,
} from '../lib/formato'
import { useEsMovil } from '../lib/useEsMovil'
import { useAlGuardarMovimiento } from '../lib/eventos'
import { useAuth } from '../lib/AuthContext'
import MovimientoForm from '../componentes/MovimientoForm'
import VisorFactura from '../componentes/VisorFactura'
import ModalCobro from '../componentes/ModalCobro'
import ModalAbonos from '../componentes/ModalAbonos'
import ModalExportar from '../componentes/ModalExportar'
import FechaCobro from '../componentes/FechaCobro'
import EtiquetaTipo from '../componentes/EtiquetaTipo'
import Modal from '../componentes/Modal'
import Distintivo from '../componentes/Distintivo'

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
  // Deuda que se está saldando (abre el modal del medio).
  const [cobrando, setCobrando] = useState(null)
  // Deuda cuyos abonos y acuerdo se están mirando.
  const [abonando, setAbonando] = useState(null)
  const [exportando, setExportando] = useState(false)
  // Los nombres con los que ya hay cuentas, para sugerirlos en el formulario.
  // Sin sugerencias, "Carlos" y "Carlos M" terminan siendo dos deudores
  // distintos con la mitad del saldo cada uno.
  const [contrapartes, setContrapartes] = useState([])

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

  // Lo que el asistente guarda aparece en la lista sin recargar la página.
  useAlGuardarMovimiento(cargar)

  useEffect(() => {
    categoriasApi.listar().then(setCategorias).catch(() => {})
    mediosApi.listar().then(setMedios).catch(() => {})
    dashboardApi
      .resumen()
      .then((r) => setContrapartes((r.contrapartes ?? []).map((c) => c.nombre)))
      // Si falla, el formulario funciona igual: se escribe el nombre a mano.
      .catch(() => {})
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

  // Saldar / volver a pendiente desde la lista, sin abrir el formulario.
  //
  // SALDAR abre antes un modal para saber por dónde se movió la plata: puede
  // ser un medio distinto al que se usó para prestar. Por dentro no marca un
  // flag, registra el abono que faltaba.
  //
  // VOLVER A PENDIENTE borra todos los abonos, y por eso se pregunta cuando
  // hay varios: es la única acción de esta pantalla que destruye datos que el
  // usuario escribió a mano.
  function alternarEstado(m) {
    if (m.estado === 'pagado' || m.estado === 'parcial') {
      const hayVarios = m.abonos > 1
      if (
        hayVarios &&
        !confirm(
          `Esta deuda tiene ${m.abonos} abonos registrados. Volverla a pendiente los borra todos. ¿Seguir?`,
        )
      ) {
        return
      }
      guardarEstado(m, 'pendiente', 0)
      return
    }
    setCobrando(m)
  }

  async function guardarEstado(m, estado, medioCobroID, cubrir) {
    setCambiandoEstado(m.id)
    setError('')
    try {
      const actualizado = await movimientosApi.cambiarEstado(m.id, estado, medioCobroID, cubrir)
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

  // El movimiento abierto en el detalle del teléfono. Se guarda el id y no
  // el objeto: así, si un abono o un "ya me pagó" lo cambia, el detalle
  // muestra lo nuevo, y si se elimina, se cierra solo.
  const [viendoID, setViendoID] = useState(null)
  const viendo = movimientos.find((m) => m.id === viendoID)

  const acciones = {
    onEditar: setEditando,
    onEliminar: eliminar,
    onAbonar: setAbonando,
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
        <div>
          {/* El rótulo dice cuántos hay: es el dato que se busca al entrar, y
              arriba del título ocupa un renglón que de todos modos estaba. */}
          <span className="kicker">
            {total} registrado{total === 1 ? '' : 's'}
          </span>
          <h1>Movimientos</h1>
        </div>
        <div className="acciones-encabezado">
          {/* Exportar sí se puede revisando otra cuenta: es solo leer. */}
          <button className="secundario" onClick={() => setExportando(true)}>
            Exportar
          </button>
          {!soloLectura && (
            <button onClick={() => setEditando({})} disabled={categorias.length === 0}>
              Nuevo movimiento
            </button>
          )}
        </div>
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
                <option value="me_prestaron">Me prestaron</option>
                <option value="traslado">Traslados</option>
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
                <option value="parcial">Abonado a medias</option>
                <option value="pagado">Saldado</option>
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
              <ul className="lista-compacta tarjeta lista-movs">
                {movimientos.map((m) => (
                  <FilaCompactaMovimiento key={m.id} m={m} onAbrir={() => setViendoID(m.id)} />
                ))}
              </ul>
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

      {viendo && (
        <DetalleMovimientoModal
          m={viendo}
          onCerrar={() => setViendoID(null)}
          {...acciones}
        />
      )}

      {editando && (
        <MovimientoForm
          movimiento={editando}
          categorias={categorias}
          medios={medios}
          contrapartes={contrapartes}
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
          categorias={categorias}
          medios={medios}
          contrapartes={contrapartes}
          onCerrar={() => setCobrando(null)}
          onConfirmar={(medioCobroID, cubrir) => guardarEstado(cobrando, 'pagado', medioCobroID, cubrir)}
        />
      )}

      {abonando && (
        <ModalAbonos
          movimiento={abonando}
          categorias={categorias}
          medios={medios}
          contrapartes={contrapartes}
          onCerrar={() => setAbonando(null)}
          onCambio={(actualizada) => {
            // La fila de atrás se actualiza sola: si no, la lista quedaría
            // mostrando el saldo de antes del abono.
            setAbonando(actualizada)
            setMovimientos((prev) => prev.map((x) => (x.id === actualizada.id ? actualizada : x)))
          }}
        />
      )}

      {exportando && <ModalExportar filtros={filtros} onCerrar={() => setExportando(false)} />}
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
//
// En una deuda con saldo son tres, y el orden importa: "Abonos" primero
// porque un pago parcial es lo más común, "Saldar" después, y el recordatorio
// de WhatsApp al final (solo cuando te deben a ti: no tendría sentido
// recordarte a ti mismo lo que debes).
function AccionesPrincipales({ m, onVerFactura, onAlternarEstado, onAbonar, cambiandoEstado, soloLectura }) {
  const conSaldo = esDeuda(m) && m.estado !== 'pagado' && !soloLectura
  const propia = esDeudaPropia(m)
  const ocupado = cambiandoEstado === m.id

  const hayAlgo = conSaldo || m.factura || (esDeuda(m) && m.abonos > 0 && !soloLectura)
  if (!hayAlgo) return null

  return (
    <div className="acciones-principales">
      {esDeuda(m) && !soloLectura && (
        <button
          className="principal-abonar"
          onClick={() => onAbonar(m)}
          title={propia ? 'Registrar un pago o armar el acuerdo' : 'Registrar un abono o armar el acuerdo'}
        >
          Abonos{m.abonos > 0 ? ` (${m.abonos})` : ''}
        </button>
      )}
      {conSaldo && (
        <button
          className="principal-pagar"
          disabled={ocupado}
          onClick={() => onAlternarEstado(m)}
          title={propia ? 'Marcar que ya se la pagaste toda' : 'Marcar que ya te pagaron todo'}
        >
          {ocupado ? 'Guardando...' : propia ? '✓ Ya le pagué' : '✓ Ya me pagó'}
        </button>
      )}
      {conSaldo && !propia && (
        // Un enlace y no un botón: abre WhatsApp con el mensaje escrito y es
        // la persona quien decide a quién se lo manda y si lo manda. La app
        // nunca le escribe a nadie por su cuenta.
        <a
          className="principal-recordar"
          href={enlaceWhatsApp(mensajeDeCobro(m))}
          target="_blank"
          rel="noreferrer"
          title="Abrir WhatsApp con el recordatorio escrito"
        >
          Recordar
        </a>
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
  // "Volver a pendiente" solo aparece cuando hay algo que deshacer, y deshace
  // de verdad: borra los abonos.
  const conAbonos = esDeuda(m) && m.abonos > 0
  const ocupado = cambiandoEstado === m.id

  // Sin acciones que ofrecer, el contenedor tampoco: si no, quedan huecos
  // vacíos alineando las filas de una lista que solo se está mirando.
  if (soloLectura) return null

  return (
    <div className="acciones-secundarias">
      {conAbonos && (
        <button className="menor" disabled={ocupado} onClick={() => onAlternarEstado(m)}>
          {ocupado ? '...' : 'Volver a pendiente'}
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

// El renglón de detalle de una deuda o de un traslado.
//
// Muestra el SALDO y no el monto cuando ya hay abonos: si mostrara el monto,
// una deuda de 500.000 con 400.000 devueltos seguiría gritando 500.000, que
// es justamente lo que los pagos parciales vienen a arreglar.
function DetalleMovimiento({ m }) {
  if (m.tipo === 'traslado') {
    return (
      <div className="sub">
        <span className="traslado-ruta">{rutaTraslado(m)}</span>
        <span className="tenue">no cambia el total</span>
      </div>
    )
  }

  if (!esDeuda(m)) return null

  const propia = esDeudaPropia(m)
  const conSaldo = m.estado !== 'pagado'

  return (
    <div className="sub">
      <span className="deuda-quien">
        {propia ? `le debes a ${m.a_quien}` : m.a_quien}
      </span>
      <span className={`estado estado-${m.estado}`}>{ETIQUETAS_ESTADO[m.estado]}</span>

      {m.abonos > 0 && conSaldo && (
        <span className="saldo-pendiente">
          faltan {formatearMonto(m.saldo)} de {formatearMonto(m.monto)}
        </span>
      )}
      {m.cuotas > 0 && (
        <span className="tenue">
          {m.cuotas} cuota{m.cuotas === 1 ? '' : 's'}
        </span>
      )}

      {/* La fecha acordada solo importa mientras quede saldo. */}
      {conSaldo && <FechaCobro fecha={m.cobrar_el} conFecha propia={propia} />}
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
        <EtiquetaTipo m={m} />
      </td>
      <td>
        <span className="con-distintivo">
          <Distintivo nombre={m.categoria_nombre} />
          {nombreCategoria(m)}
        </span>
        {m.medio_pago_nombre && (
          <div className="sub-medio con-distintivo">
            <Distintivo nombre={m.medio_pago_nombre} clase="medio" />
            {m.medio_pago_nombre}
          </div>
        )}
      </td>
      <td>
        {m.descripcion || <span className="tenue">—</span>}
        <DetalleMovimiento m={m} />
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

// Una fila por movimiento con lo esencial: qué fue, de qué categoría,
// cuándo y cuánto. Antes cada movimiento era una tarjeta con todos sus
// botones, y en la pantalla cabían dos. Lo demás (medio, deuda, abonos,
// factura, editar, eliminar) está a un toque, en el detalle.
function FilaCompactaMovimiento({ m, onAbrir }) {
  const titulo = m.descripcion || nombreCategoria(m)
  const pendiente = esDeuda(m) && m.estado !== 'pagado'

  // Debajo del título: la categoría si el título es la descripción (si no,
  // se repetiría), y en una deuda abierta, con quién. La fecha siempre.
  const partes = []
  if (m.descripcion) partes.push(nombreCategoria(m))
  if (pendiente) partes.push(esDeudaPropia(m) ? `le debes a ${m.a_quien}` : m.a_quien)
  partes.push(formatearFechaCorta(m.fecha))

  return (
    <li
      className="fila-compacta fila-clic"
      onClick={onAbrir}
      tabIndex={0}
      role="button"
      onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), onAbrir())}
    >
      <Distintivo nombre={m.categoria_nombre} grande />
      <div className="fila-compacta-texto">
        <strong>{titulo}</strong>
        <span className="tenue">{partes.join(' · ')}</span>
      </div>
      <div className="fila-compacta-monto">
        <Monto m={m} />
        {pendiente && (
          <span className={`estado estado-${m.estado}`}>
            {m.abonos > 0 ? `faltan ${formatearMonto(m.saldo)}` : ETIQUETAS_ESTADO[m.estado]}
          </span>
        )}
        {m.factura && !pendiente && <span className="tenue fila-compacta-nota">con factura</span>}
      </div>
    </li>
  )
}

// El detalle completo de un movimiento, con todas sus acciones.
//
// Cualquier acción cierra el detalle antes de hacer lo suyo: editar, abonar
// o saldar abren su propia ventana, y dos modales encima uno del otro en un
// teléfono son un laberinto. Al terminar, la lista ya está al día.
function DetalleMovimientoModal({ m, onCerrar, ...props }) {
  const cerrarY =
    (accion) =>
    (...args) => {
      onCerrar()
      accion(...args)
    }

  const acciones = {
    ...props,
    onEditar: cerrarY(props.onEditar),
    onEliminar: cerrarY(props.onEliminar),
    onAbonar: cerrarY(props.onAbonar),
    onVerFactura: cerrarY(props.onVerFactura),
    onAlternarEstado: cerrarY(props.onAlternarEstado),
  }

  return (
    <Modal titulo={m.descripcion || nombreCategoria(m)} onCerrar={onCerrar}>
      <div className="detalle-mov">
        <div className="detalle-mov-cabeza">
          <EtiquetaTipo m={m} />
          <span className="detalle-mov-monto">
            <Monto m={m} />
          </span>
        </div>

        <dl className="detalle-mov-datos">
          <div>
            <dt>Fecha</dt>
            <dd>{formatearFecha(m.fecha)}</dd>
          </div>
          <div>
            <dt>Categoría</dt>
            <dd className="con-distintivo">
              <Distintivo nombre={m.categoria_nombre} />
              {nombreCategoria(m)}
            </dd>
          </div>
          {m.medio_pago_nombre && m.tipo !== 'traslado' && (
            <div>
              <dt>Medio</dt>
              <dd className="con-distintivo">
                <Distintivo nombre={m.medio_pago_nombre} clase="medio" />
                {m.medio_pago_nombre}
              </dd>
            </div>
          )}
          {m.descripcion && (
            <div>
              <dt>Descripción</dt>
              <dd>{m.descripcion}</dd>
            </div>
          )}
        </dl>

        <DetalleMovimiento m={m} />

        <div className="tarjeta-mov-acciones">
          <AccionesPrincipales m={m} {...acciones} />
          <AccionesSecundarias m={m} {...acciones} />
        </div>
      </div>
    </Modal>
  )
}
