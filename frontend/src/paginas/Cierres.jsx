import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { cierresApi, exportarCierres, tiendasApi } from '../lib/api'
import { guardarArchivo } from '../lib/archivos'
import { useAuth } from '../lib/AuthContext'
import { formatearFecha, formatearMonto } from '../lib/formato'
import CierreForm from '../componentes/CierreForm'
import GraficasCierres from '../componentes/GraficasCierres'
import Icono from '../componentes/Icono'
import Vacio from '../componentes/Vacio'

// Los cierres de caja de TODAS las tiendas, en una sola lista, como los
// movimientos: lo último arriba, el nombre de la tienda en cada renglón,
// filtros por local y por fechas, la comparación entre locales y el Excel.
//
// Hacer uno pide primero la tienda, porque una hoja siempre es de un local.
export default function Cierres() {
  // Los filtros viven en la URL para que /cierres?tienda=3 sea un enlace
  // válido: es lo que usan las fichas de la sección Tiendas.
  const [params, setParams] = useSearchParams()
  const filtros = {
    tienda_id: params.get('tienda') ?? '',
    desde: params.get('desde') ?? '',
    hasta: params.get('hasta') ?? '',
  }

  const [cierres, setCierres] = useState([])
  const [tiendas, setTiendas] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  // Un "hecho a medias": la hoja se guardó pero la foto no subió. No es un
  // fallo —el cierre ya está en la lista—, así que no va en la alerta roja.
  const [aviso, setAviso] = useState('')
  const [exportando, setExportando] = useState(false)
  // null = cerrado; {} = uno nuevo; un cierre = abrirlo.
  const [editando, setEditando] = useState(null)
  const { soloLectura } = useAuth()

  function cambiarFiltro(clave, valor) {
    const nuevos = new URLSearchParams(params)
    if (valor) nuevos.set(clave, valor)
    else nuevos.delete(clave)
    setParams(nuevos, { replace: true })
  }

  async function recargar() {
    setError('')
    setAviso('')
    try {
      const [hojas, lista] = await Promise.all([cierresApi.todos(filtros), tiendasApi.listar()])
      setCierres(hojas)
      setTiendas(lista)
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
    // El filtro lo aplica el servidor: es el que sabe cuáles son las hojas de
    // esta cuenta, y así el Excel exporta exactamente lo que se ve.
  }, [filtros.tienda_id, filtros.desde, filtros.hasta])

  async function abrir(cierre) {
    // La lista no trae las líneas: se piden al abrir la hoja.
    try {
      setEditando(await cierresApi.ver(cierre.tienda_id, cierre.id))
    } catch (err) {
      setError(err.message)
    }
  }

  async function eliminar(cierre) {
    if (
      !confirm(`¿Eliminar el cierre de ${cierre.tienda_nombre} del ${formatearFecha(cierre.fecha)}?`)
    )
      return
    try {
      await cierresApi.eliminar(cierre.tienda_id, cierre.id)
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  async function exportar() {
    setError('')
    setExportando(true)
    try {
      const { blob, nombre } = await exportarCierres(filtros)
      guardarArchivo(blob, nombre)
    } catch (err) {
      setError(err.message)
    } finally {
      setExportando(false)
    }
  }

  const hayFiltros = Boolean(filtros.tienda_id || filtros.desde || filtros.hasta)
  // Un rango invertido no es invalido para el servidor (simplemente no
  // encuentra nada), pero avisar aqui evita que "Nada con esos filtros"
  // parezca un error de la lista.
  const rangoInvertido = Boolean(filtros.desde && filtros.hasta && filtros.desde > filtros.hasta)

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <span className="kicker">El arqueo de cada día</span>
          <h1>Cierres</h1>
        </div>
        {tiendas.length > 0 && (
          <div className="acciones-encabezado">
            <button className="secundario" onClick={exportar} disabled={exportando || cargando}>
              {exportando ? 'Generando...' : 'Exportar a Excel'}
            </button>
            {!soloLectura && <button onClick={() => setEditando({})}>Nuevo cierre</button>}
          </div>
        )}
      </div>

      {error && <div className="alerta">{error}</div>}
      {aviso && <div className="alerta aviso">{aviso}</div>}

      {cargando ? (
        <section className="tarjeta">
          <p className="tenue">Cargando...</p>
        </section>
      ) : tiendas.length === 0 ? (
        <section className="tarjeta">
          <Vacio
            icono="tiendas"
            titulo="Primero una tienda"
            accion={<Link to="/categorias">Crear una tienda</Link>}
          >
            Un cierre de caja siempre es de un local, así que hay que tener al menos uno. Se crean
            junto a las categorías.
          </Vacio>
        </section>
      ) : (
        <>
          <section className="tarjeta">
            <div className="fila-filtros">
              <div>
                <label htmlFor="filtro-tienda">Tienda</label>
                <select
                  id="filtro-tienda"
                  value={filtros.tienda_id}
                  onChange={(e) => cambiarFiltro('tienda', e.target.value)}
                >
                  <option value="">Todas</option>
                  {tiendas.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.nombre}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor="filtro-desde">Desde</label>
                <input
                  id="filtro-desde"
                  type="date"
                  value={filtros.desde}
                  onChange={(e) => cambiarFiltro('desde', e.target.value)}
                />
              </div>
              <div>
                <label htmlFor="filtro-hasta">Hasta</label>
                <input
                  id="filtro-hasta"
                  type="date"
                  value={filtros.hasta}
                  onChange={(e) => cambiarFiltro('hasta', e.target.value)}
                />
                {rangoInvertido && (
                  <span className="error-campo">La fecha de inicio es posterior a la de fin</span>
                )}
              </div>
              {hayFiltros && (
                <button className="secundario" onClick={() => setParams({}, { replace: true })}>
                  Quitar filtros
                </button>
              )}
            </div>
          </section>

          {cierres.length > 0 && <GraficasCierres cierres={cierres} />}

          <section className="tarjeta">
            {cierres.length === 0 ? (
              <Vacio
                icono="tiendas"
                titulo={hayFiltros ? 'Nada con esos filtros' : 'Todavía no hay cierres'}
                accion={
                  hayFiltros ? (
                    <button className="secundario" onClick={() => setParams({}, { replace: true })}>
                      Quitar filtros
                    </button>
                  ) : (
                    !soloLectura && <button onClick={() => setEditando({})}>Hacer el primero</button>
                  )
                }
              >
                Es la hoja del final del día: lo que reportó el banco y el datáfono contra lo que
                contaste, más las compras, gastos, descuentos y vales.
              </Vacio>
            ) : (
              <div className="tabla-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Día</th>
                      <th>Tienda</th>
                      <th>Responsable</th>
                      <th className="num">Ventas</th>
                      <th className="num">Salió</th>
                      <th className="num">Debería quedar</th>
                      <th className="num">Hay</th>
                      <th className="num">Diferencia</th>
                      <th className="acciones"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {cierres.map((c) => {
                      // Lo que dice si el día cuadró: la venta menos lo que
                      // salió, contra lo que de verdad hay en caja y bancos.
                      const descuadre = Math.round(Number(c.totales.queda_diferencia) || 0) !== 0
                      return (
                        <tr key={c.id}>
                          <td className="nowrap">{formatearFecha(c.fecha)}</td>
                          <td className="nowrap">
                            <button
                              type="button"
                              className="boton-enlace"
                              onClick={() => cambiarFiltro('tienda', String(c.tienda_id))}
                              title={`Ver solo lo de ${c.tienda_nombre}`}
                            >
                              {c.tienda_nombre}
                            </button>
                          </td>
                          <td className="tenue">
                            {c.responsable || '—'}
                            {c.foto && <span className="tenue"> · con foto</span>}
                          </td>
                          <td className="num fig">{formatearMonto(c.venta_tienda)}</td>
                          <td className="num fig tenue">{formatearMonto(c.totales.salidas)}</td>
                          <td className="num fig">{formatearMonto(c.totales.deberia_quedar)}</td>
                          <td className="num fig">{formatearMonto(c.totales.metodos_pago)}</td>
                          <td className={`num fig ${descuadre ? 'negativo' : 'tenue'}`}>
                            {formatearMonto(c.totales.queda_diferencia)}
                          </td>
                          <td className="acciones">
                            <button className="secundario" onClick={() => abrir(c)}>
                              {soloLectura ? 'Ver' : 'Abrir'}
                            </button>
                            {!soloLectura && (
                              <button
                                className="boton-tema boton-eliminar"
                                onClick={() => eliminar(c)}
                                title="Eliminar este cierre"
                                aria-label="Eliminar este cierre"
                              >
                                <Icono nombre="eliminar" tamano={18} />
                              </button>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>
      )}

      {editando && (
        <CierreForm
          tiendas={tiendas}
          // La del filtro viene preseleccionada: si estás mirando una tienda,
          // el cierre que vas a hacer casi siempre es de esa.
          tiendaID={editando.tienda_id ?? filtros.tienda_id}
          cierre={editando.id ? editando : null}
          onCerrar={() => setEditando(null)}
          onGuardado={async (avisoFoto) => {
            setEditando(null)
            await recargar()
            // Después de recargar: la lista ya trae la hoja, y recargar limpia
            // los mensajes de la pantalla.
            if (avisoFoto) setAviso(avisoFoto)
          }}
        />
      )}
    </>
  )
}
