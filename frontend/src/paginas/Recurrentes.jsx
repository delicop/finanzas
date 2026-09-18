import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { categoriasApi, mediosApi, recurrentesApi } from '../lib/api'
import {
  entradaAMonto,
  formatearFecha,
  formatearMonto,
  hoyISO,
  montoAEntrada,
} from '../lib/formato'
import { useAuth } from '../lib/AuthContext'
import { useEsMovil } from '../lib/useEsMovil'
import InputMonto from '../componentes/InputMonto'
import Modal from '../componentes/Modal'
import RecurrentesPendientes from '../componentes/RecurrentesPendientes'

const DIAS_SEMANA = [
  [1, 'Lunes'],
  [2, 'Martes'],
  [3, 'Miércoles'],
  [4, 'Jueves'],
  [5, 'Viernes'],
  [6, 'Sábado'],
  [7, 'Domingo'],
]

// Los gastos e ingresos que se repiten: el arriendo, el internet, el sueldo.
//
// Esta pantalla es el CATÁLOGO de plantillas. Lo que de verdad se registra
// pasa arriba, en "Por confirmar": la app propone y la persona confirma.
export default function Recurrentes() {
  const [recurrentes, setRecurrentes] = useState([])
  const [categorias, setCategorias] = useState([])
  const [medios, setMedios] = useState([])
  const [cargando, setCargando] = useState(true)
  const [error, setError] = useState('')
  const [editando, setEditando] = useState(null)
  const esMovil = useEsMovil()
  const { soloLectura } = useAuth()

  async function recargar() {
    setError('')
    try {
      const datos = await recurrentesApi.listar()
      setRecurrentes(datos.recurrentes)
    } catch (err) {
      setError(err.message)
    } finally {
      setCargando(false)
    }
  }

  useEffect(() => {
    recargar()
    categoriasApi.listar().then(setCategorias).catch(() => {})
    mediosApi.listar().then(setMedios).catch(() => {})
  }, [])

  async function eliminar(r) {
    if (
      !confirm(
        `¿Eliminar "${r.descripcion}"? Los movimientos que ya confirmaste NO se borran: eran plata que sí se movió.`,
      )
    ) {
      return
    }
    try {
      await recurrentesApi.eliminar(r.id)
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  async function alternarActivo(r) {
    try {
      await recurrentesApi.actualizar(r.id, {
        categoria_id: r.categoria_id,
        medio_pago_id: r.medio_pago_id,
        tipo: r.tipo,
        monto: r.monto,
        descripcion: r.descripcion,
        frecuencia: r.frecuencia,
        dia: r.dia,
        desde: r.desde,
        hasta: r.hasta ?? '',
        activo: !r.activo,
      })
      await recargar()
    } catch (err) {
      setError(err.message)
    }
  }

  const listo = categorias.length > 0 && medios.length > 0

  return (
    <>
      <div className="encabezado-pagina">
        <div>
          <span className="kicker">Plantillas del mes</span>
          <h1>Se repiten</h1>
        </div>
        {!soloLectura && (
          <button onClick={() => setEditando({})} disabled={!listo}>
            Nuevo recurrente
          </button>
        )}
      </div>

      {!listo && !cargando && (
        <div className="alerta aviso">
          Necesitas al menos una <Link to="/categorias">categoría</Link> y un{' '}
          <Link to="/medios-pago">medio de pago</Link>.
        </div>
      )}

      {!soloLectura && <RecurrentesPendientes onConfirmado={recargar} />}

      {error && <div className="alerta">{error}</div>}

      <section className="tarjeta">
        <h2>Plantillas</h2>
        <p className="subtitulo">
          Cuando a una le toca, aparece arriba para que la confirmes. La app nunca registra
          plata sola.
        </p>

        {cargando ? (
          <p className="tenue">Cargando...</p>
        ) : recurrentes.length === 0 ? (
          <p className="tenue">
            Aún no tienes nada que se repita. Crea el arriendo, el internet o el sueldo y
            deja de anotarlos a mano cada mes.
          </p>
        ) : esMovil ? (
          <div className="lista-movil">
            {recurrentes.map((r) => (
              <TarjetaRecurrente
                key={r.id}
                r={r}
                soloLectura={soloLectura}
                onEditar={setEditando}
                onEliminar={eliminar}
                onAlternar={alternarActivo}
              />
            ))}
          </div>
        ) : (
          <div className="tabla-scroll">
            <table>
              <thead>
                <tr>
                  <th>Qué</th>
                  <th>Cada cuánto</th>
                  <th>Próxima</th>
                  <th className="num">Monto</th>
                  <th className="acciones"></th>
                </tr>
              </thead>
              <tbody>
                {recurrentes.map((r) => (
                  <tr key={r.id} className={r.activo ? '' : 'fila-pausada'}>
                    <td>
                      {r.descripcion}
                      <div className="sub-medio">
                        {r.categoria_nombre} · {r.medio_pago_nombre}
                      </div>
                    </td>
                    <td>{describirFrecuencia(r)}</td>
                    <td className="nowrap">
                      {r.activo ? (
                        r.proxima_fecha ? (
                          formatearFecha(r.proxima_fecha)
                        ) : (
                          <span className="tenue">ya terminó</span>
                        )
                      ) : (
                        <span className="tenue">pausado</span>
                      )}
                    </td>
                    <td className="num nowrap">
                      <span
                        className={`fig ${r.tipo === 'recibi' ? 'positivo' : 'negativo'}`}
                        style={{ fontSize: 17 }}
                      >
                        {r.tipo === 'recibi' ? '+' : '−'} {formatearMonto(r.monto)}
                      </span>
                    </td>
                    <td className="acciones">
                      <Acciones
                        r={r}
                        soloLectura={soloLectura}
                        onEditar={setEditando}
                        onEliminar={eliminar}
                        onAlternar={alternarActivo}
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
        <FormRecurrente
          recurrente={editando}
          categorias={categorias}
          medios={medios}
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

/* ------------------------------------------------------------------------ */

// "Cada mes el día 5", "Cada 15 días desde el 1", "Todos los lunes".
function describirFrecuencia(r) {
  if (r.frecuencia === 'semanal') {
    const nombre = DIAS_SEMANA.find(([n]) => n === r.dia)?.[1] ?? ''
    return `Todos los ${nombre.toLowerCase()}`
  }
  if (r.frecuencia === 'quincenal') {
    return `Cada 15 días, desde el ${r.dia}`
  }
  return `Cada mes, el día ${r.dia}`
}

function Acciones({ r, soloLectura, onEditar, onEliminar, onAlternar }) {
  if (soloLectura) return null
  return (
    <div className="acciones-secundarias">
      <button className="menor" onClick={() => onAlternar(r)}>
        {r.activo ? 'Pausar' : 'Reanudar'}
      </button>
      <button className="menor" onClick={() => onEditar(r)}>
        Editar
      </button>
      <button className="menor menor-peligro" onClick={() => onEliminar(r)}>
        Eliminar
      </button>
    </div>
  )
}

function TarjetaRecurrente({ r, ...acciones }) {
  return (
    <article className={`tarjeta-mov ${r.activo ? '' : 'fila-pausada'}`}>
      <div className="tarjeta-mov-arriba">
        <span className={`etiqueta tipo-${r.tipo}`}>{r.tipo === 'recibi' ? 'Recibí' : 'Pagué'}</span>
        <span className="tenue fecha">{describirFrecuencia(r)}</span>
      </div>

      <p className="tarjeta-mov-desc">{r.descripcion}</p>

      <div className="tarjeta-mov-meta">
        <span className="tenue">
          {r.categoria_nombre} · {r.medio_pago_nombre}
        </span>
        <strong className={`fig ${r.tipo === 'recibi' ? 'positivo' : 'negativo'}`}>
          {r.tipo === 'recibi' ? '+' : '−'} {formatearMonto(r.monto)}
        </strong>
      </div>

      <div className="sub">
        {r.activo ? (
          r.proxima_fecha ? (
            <span>próxima: {formatearFecha(r.proxima_fecha)}</span>
          ) : (
            <span className="tenue">ya terminó</span>
          )
        ) : (
          <span className="tenue">pausado</span>
        )}
      </div>

      <div className="tarjeta-mov-acciones">
        <Acciones r={r} {...acciones} />
      </div>
    </article>
  )
}

/* ------------------------------------------------------------------------ */

function FormRecurrente({ recurrente, categorias, medios, onCerrar, onGuardado }) {
  const esNuevo = !recurrente?.id

  const [datos, setDatos] = useState(() =>
    esNuevo
      ? {
          categoria_id: categorias[0]?.id ?? '',
          medio_pago_id: medios[0]?.id ?? '',
          tipo: 'pague',
          monto: '',
          descripcion: '',
          frecuencia: 'mensual',
          dia: 1,
          desde: hoyISO(),
          hasta: '',
          activo: true,
        }
      : {
          categoria_id: recurrente.categoria_id,
          medio_pago_id: recurrente.medio_pago_id,
          tipo: recurrente.tipo,
          monto: montoAEntrada(recurrente.monto),
          descripcion: recurrente.descripcion,
          frecuencia: recurrente.frecuencia,
          dia: recurrente.dia,
          desde: recurrente.desde,
          hasta: recurrente.hasta ?? '',
          activo: recurrente.activo,
        }
  )
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  function cambiar(campo, valor) {
    setDatos((prev) => ({ ...prev, [campo]: valor }))
  }

  // Al cambiar de frecuencia el día cambia de significado (día del mes vs.
  // día de la semana), así que se reinicia: dejar el 23 en "semanal" sería un
  // valor que la base rechaza.
  function cambiarFrecuencia(valor) {
    setDatos((prev) => ({ ...prev, frecuencia: valor, dia: valor === 'semanal' ? 1 : 1 }))
  }

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGuardando(true)

    try {
      const cuerpo = {
        categoria_id: Number(datos.categoria_id),
        medio_pago_id: Number(datos.medio_pago_id),
        tipo: datos.tipo,
        monto: entradaAMonto(datos.monto),
        descripcion: datos.descripcion,
        frecuencia: datos.frecuencia,
        dia: Number(datos.dia),
        desde: datos.desde,
        hasta: datos.hasta,
        activo: datos.activo,
      }

      if (esNuevo) await recurrentesApi.crear(cuerpo)
      else await recurrentesApi.actualizar(recurrente.id, cuerpo)

      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={esNuevo ? 'Nuevo recurrente' : 'Editar recurrente'} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="r-tipo">Tipo <span className="req">*</span></label>
        <div className="grupo-tipos" id="r-tipo">
          {[
            ['pague', 'Pagué'],
            ['recibi', 'Recibí'],
          ].map(([valor, etiqueta]) => (
            <button
              key={valor}
              type="button"
              className={`chip ${datos.tipo === valor ? 'chip-activo' : ''}`}
              onClick={() => cambiar('tipo', valor)}
            >
              {etiqueta}
            </button>
          ))}
        </div>
        <span className="ayuda-campo tenue">
          Los préstamos no se repiten: cada uno es un acuerdo distinto, con su persona y su
          fecha.
        </span>

        <label htmlFor="r-desc">¿Qué es? <span className="req">*</span></label>
        <input
          id="r-desc"
          value={datos.descripcion}
          onChange={(e) => cambiar('descripcion', e.target.value)}
          placeholder="Arriendo, Internet, Sueldo..."
        />
        {campos.descripcion && <span className="error-campo">{campos.descripcion}</span>}

        <label htmlFor="r-categoria">Categoría <span className="req">*</span></label>
        <select
          id="r-categoria"
          value={datos.categoria_id}
          onChange={(e) => cambiar('categoria_id', e.target.value)}
        >
          {categorias.map((c) => (
            <option key={c.id} value={c.id}>
              {c.nombre}
            </option>
          ))}
        </select>
        {campos.categoria_id && <span className="error-campo">{campos.categoria_id}</span>}

        <label htmlFor="r-medio">¿Cómo se paga? <span className="req">*</span></label>
        <select
          id="r-medio"
          value={datos.medio_pago_id}
          onChange={(e) => cambiar('medio_pago_id', e.target.value)}
        >
          {medios.map((m) => (
            <option key={m.id} value={m.id}>
              {m.nombre}
            </option>
          ))}
        </select>
        {campos.medio_pago_id && <span className="error-campo">{campos.medio_pago_id}</span>}

        <label htmlFor="r-monto">Monto de siempre <span className="req">*</span></label>
        <InputMonto
          id="r-monto"
          valor={datos.monto}
          onCambio={(v) => cambiar('monto', v)}
          placeholder="900.000"
        />
        {campos.monto ? (
          <span className="error-campo">{campos.monto}</span>
        ) : (
          <span className="ayuda-campo tenue">
            Si un mes llega distinto, lo corriges al confirmarlo. No hace falta editar esto.
          </span>
        )}

        <div className="dos-columnas">
          <div>
            <label htmlFor="r-frecuencia">¿Cada cuánto? <span className="req">*</span></label>
            <select
              id="r-frecuencia"
              value={datos.frecuencia}
              onChange={(e) => cambiarFrecuencia(e.target.value)}
            >
              <option value="mensual">Cada mes</option>
              <option value="quincenal">Cada 15 días</option>
              <option value="semanal">Cada semana</option>
            </select>
            {campos.frecuencia && <span className="error-campo">{campos.frecuencia}</span>}
          </div>

          <div>
            <label htmlFor="r-dia">
              {datos.frecuencia === 'semanal' ? '¿Qué día?' : '¿Qué día del mes?'}{' '}
              <span className="req">*</span>
            </label>
            {datos.frecuencia === 'semanal' ? (
              <select id="r-dia" value={datos.dia} onChange={(e) => cambiar('dia', e.target.value)}>
                {DIAS_SEMANA.map(([n, nombre]) => (
                  <option key={n} value={n}>
                    {nombre}
                  </option>
                ))}
              </select>
            ) : (
              <input
                id="r-dia"
                type="number"
                min="1"
                max="31"
                value={datos.dia}
                onChange={(e) => cambiar('dia', e.target.value)}
              />
            )}
            {campos.dia && <span className="error-campo">{campos.dia}</span>}
          </div>
        </div>

        {datos.frecuencia !== 'semanal' && Number(datos.dia) > 28 && (
          <span className="ayuda-campo tenue">
            En los meses que no tienen ese día, cae en el último. Febrero no se salta.
          </span>
        )}

        <div className="dos-columnas">
          <div>
            <label htmlFor="r-desde">Desde <span className="req">*</span></label>
            <input
              id="r-desde"
              type="date"
              value={datos.desde}
              onChange={(e) => cambiar('desde', e.target.value)}
            />
            {campos.desde && <span className="error-campo">{campos.desde}</span>}
          </div>

          <div>
            <label htmlFor="r-hasta">Hasta <span className="tenue">(opcional)</span></label>
            <input
              id="r-hasta"
              type="date"
              value={datos.hasta}
              min={datos.desde || undefined}
              onChange={(e) => cambiar('hasta', e.target.value)}
            />
            {campos.hasta && <span className="error-campo">{campos.hasta}</span>}
          </div>
        </div>

        <label className="checkbox">
          <input
            type="checkbox"
            checked={datos.activo}
            onChange={(e) => cambiar('activo', e.target.checked)}
          />
          Activo
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
