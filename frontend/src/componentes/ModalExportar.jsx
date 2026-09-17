import { useState } from 'react'
import { exportarMovimientos } from '../lib/api'
import { guardarArchivo } from '../lib/archivos'
import { hoyISO } from '../lib/formato'
import Modal from './Modal'

const FORMATOS = [
  { valor: 'xlsx', etiqueta: 'Excel', detalle: 'Para sumar, filtrar o pasárselo al contador' },
  { valor: 'pdf', etiqueta: 'PDF', detalle: 'Para imprimir o guardar como extracto' },
]

// Los atajos de rango, armados como texto "AAAA-MM-DD" a partir de la fecha
// local de hoy. Nada de toISOString: en Colombia (UTC-5) después de las 7 de
// la noche ya daría el día siguiente.
function rangos() {
  const hoy = hoyISO()
  const [anio, mes] = hoy.split('-').map(Number)
  const dos = (n) => String(n).padStart(2, '0')
  const ultimoDia = (a, m) => new Date(a, m, 0).getDate() // día 0 del mes siguiente

  const mesPasado = mes === 1 ? { a: anio - 1, m: 12 } : { a: anio, m: mes - 1 }

  return [
    { etiqueta: 'Este mes', desde: `${anio}-${dos(mes)}-01`, hasta: hoy },
    {
      etiqueta: 'Mes pasado',
      desde: `${mesPasado.a}-${dos(mesPasado.m)}-01`,
      hasta: `${mesPasado.a}-${dos(mesPasado.m)}-${dos(ultimoDia(mesPasado.a, mesPasado.m))}`,
    },
    { etiqueta: 'Este año', desde: `${anio}-01-01`, hasta: hoy },
    { etiqueta: 'Año pasado', desde: `${anio - 1}-01-01`, hasta: `${anio - 1}-12-31` },
  ]
}

const NOMBRES_FILTROS = {
  categoria_id: 'categoría',
  medio_pago_id: 'medio',
  tipo: 'tipo',
  estado: 'estado',
  q: 'búsqueda',
}

// Exportar los movimientos de un rango de fechas a Excel o PDF.
//
// Arranca con el rango que ya esté filtrado en la lista (o este mes), y
// respeta los demás filtros: se exporta lo mismo que se está viendo.
export default function ModalExportar({ filtros, onCerrar }) {
  const atajos = rangos()
  const [desde, setDesde] = useState(filtros.desde || atajos[0].desde)
  const [hasta, setHasta] = useState(filtros.hasta || atajos[0].hasta)
  const [formato, setFormato] = useState('xlsx')
  const [generando, setGenerando] = useState(false)
  const [error, setError] = useState('')
  const [campos, setCampos] = useState({})

  const otrosFiltros = Object.keys(NOMBRES_FILTROS)
    .filter((k) => filtros[k])
    .map((k) => NOMBRES_FILTROS[k])

  async function descargar(e) {
    e.preventDefault()
    setError('')
    setCampos({})
    setGenerando(true)
    try {
      const archivo = await exportarMovimientos({ ...filtros, desde, hasta }, formato)
      guardarArchivo(archivo, `movimientos-${desde}-a-${hasta}.${formato}`)
      onCerrar()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGenerando(false)
    }
  }

  return (
    <Modal titulo="Exportar movimientos" onCerrar={onCerrar}>
      <form onSubmit={descargar} className="formulario-exportar">
        {error && <div className="alerta">{error}</div>}

        <div className="chat-sugerencias atajos-rango">
          {atajos.map((a) => (
            <button
              key={a.etiqueta}
              type="button"
              className={`chip ${a.desde === desde && a.hasta === hasta ? 'chip-activo' : ''}`}
              onClick={() => {
                setDesde(a.desde)
                setHasta(a.hasta)
              }}
            >
              {a.etiqueta}
            </button>
          ))}
        </div>

        <div className="dos-columnas">
          <div className="campo">
            <label htmlFor="exp-desde">Desde</label>
            <input
              id="exp-desde"
              type="date"
              required
              value={desde}
              max={hasta || undefined}
              onChange={(e) => setDesde(e.target.value)}
            />
            {campos.desde && <span className="error-campo">{campos.desde}</span>}
          </div>
          <div className="campo">
            <label htmlFor="exp-hasta">Hasta</label>
            <input
              id="exp-hasta"
              type="date"
              required
              value={hasta}
              min={desde || undefined}
              onChange={(e) => setHasta(e.target.value)}
            />
            {campos.hasta && <span className="error-campo">{campos.hasta}</span>}
          </div>
        </div>

        <fieldset className="opciones-formato">
          <legend>Formato</legend>
          {FORMATOS.map((f) => (
            <label
              key={f.valor}
              className={`opcion-formato ${formato === f.valor ? 'activa' : ''}`}
            >
              <input
                type="radio"
                name="formato"
                value={f.valor}
                checked={formato === f.valor}
                onChange={() => setFormato(f.valor)}
              />
              <span>
                <strong>{f.etiqueta}</strong>
                <span className="tenue">{f.detalle}</span>
              </span>
            </label>
          ))}
        </fieldset>

        {otrosFiltros.length > 0 && (
          <p className="tenue nota-filtros">
            También se aplican los filtros de la lista ({otrosFiltros.join(', ')}). Para exportar
            todo, límpialos primero.
          </p>
        )}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={generando || !desde || !hasta}>
            {generando ? 'Generando...' : 'Descargar'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
