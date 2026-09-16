import { useState } from 'react'
import { movimientosApi } from '../lib/api'
import { entradaAMonto, hoyISO, montoAEntrada } from '../lib/formato'
import InputMonto from './InputMonto'
import Modal from './Modal'

const VACIO = {
  categoria_id: '',
  medio_pago_id: '',
  tipo: 'pague',
  monto: '',
  fecha: hoyISO(),
  descripcion: '',
  a_quien: '',
  estado: 'pendiente',
}

export default function MovimientoForm({ movimiento, categorias, medios, onCerrar, onGuardado }) {
  const esNuevo = !movimiento?.id

  const [datos, setDatos] = useState(() =>
    esNuevo
      ? { ...VACIO, categoria_id: categorias[0]?.id ?? '', medio_pago_id: medios[0]?.id ?? '' }
      : {
          categoria_id: movimiento.categoria_id,
          medio_pago_id: movimiento.medio_pago_id ?? '',
          tipo: movimiento.tipo,
          monto: montoAEntrada(movimiento.monto),
          fecha: movimiento.fecha,
          descripcion: movimiento.descripcion,
          a_quien: movimiento.a_quien ?? '',
          estado: movimiento.estado ?? 'pendiente',
        }
  )

  const [archivo, setArchivo] = useState(null)
  const [quitarFactura, setQuitarFactura] = useState(false)
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  // La regla del negocio en una sola variable: solo 'presté' pide
  // a quién le prestaste y si ya te pagó.
  const esPrestamo = datos.tipo === 'preste'

  function cambiar(campo, valor) {
    setDatos((prev) => ({ ...prev, [campo]: valor }))
  }

  // Obligatorios: tipo, categoría, medio de pago, monto y fecha.
  // Opcionales: descripción y factura.
  //
  // Se valida aquí ADEMÁS de en el backend: aquí para que el error salga al
  // instante sin ir al servidor, y allá porque el navegador no es de fiar
  // (cualquiera puede mandar una petición saltándose el formulario).
  function faltantes() {
    const errores = {}
    if (!datos.categoria_id) errores.categoria_id = 'Selecciona una categoría'
    if (!datos.medio_pago_id) errores.medio_pago_id = 'Indica cómo fue el pago'
    if (!String(datos.monto).trim()) errores.monto = 'Este campo es obligatorio'
    if (!datos.fecha) errores.fecha = 'Este campo es obligatorio'
    if (esPrestamo && !datos.a_quien.trim()) errores.a_quien = 'Este campo es obligatorio'
    return errores
  }

  async function onSubmit(e) {
    e.preventDefault()
    setError('')

    const errores = faltantes()
    if (Object.keys(errores).length > 0) {
      setCampos(errores)
      setError('Faltan campos obligatorios')
      return
    }

    setCampos({})
    setGuardando(true)

    try {
      const cuerpo = {
        categoria_id: Number(datos.categoria_id),
        medio_pago_id: Number(datos.medio_pago_id),
        tipo: datos.tipo,
        // De "1.500.000,50" a "1500000.50": la API siempre recibe el número crudo.
        monto: entradaAMonto(datos.monto),
        fecha: datos.fecha,
        descripcion: datos.descripcion,
        // El backend ignora estos dos campos si el tipo no es 'preste',
        // pero los mandamos vacíos para dejar clara la intención.
        a_quien: esPrestamo ? datos.a_quien : '',
        estado: esPrestamo ? datos.estado : '',
      }

      const guardado = esNuevo
        ? await movimientosApi.crear(cuerpo)
        : await movimientosApi.actualizar(movimiento.id, cuerpo)

      // La factura va en una segunda petición porque es multipart.
      // Si esta falla, el movimiento YA quedó guardado: se avisa y el
      // usuario puede reintentar solo el adjunto, sin volver a escribir todo.
      if (archivo) {
        await movimientosApi.subirFactura(guardado.id, archivo)
      } else if (quitarFactura && movimiento?.factura) {
        await movimientosApi.eliminarFactura(guardado.id)
      }

      await onGuardado()
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setGuardando(false)
    }
  }

  return (
    <Modal titulo={esNuevo ? 'Nuevo movimiento' : 'Editar movimiento'} onCerrar={onCerrar}>
      <form onSubmit={onSubmit} noValidate>
        {error && <div className="alerta">{error}</div>}

        <label htmlFor="tipo">Tipo <span className="req">*</span></label>
        <div className="grupo-tipos" id="tipo">
          {[
            ['recibi', 'Recibí'],
            ['pague', 'Pagué'],
            ['preste', 'Presté'],
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
        {campos.tipo && <span className="error-campo">{campos.tipo}</span>}

        <label htmlFor="categoria">Categoría <span className="req">*</span></label>
        <select
          id="categoria"
          value={datos.categoria_id}
          onChange={(e) => cambiar('categoria_id', e.target.value)}
        >
          <option value="">Selecciona una categoría</option>
          {categorias.map((c) => (
            <option key={c.id} value={c.id}>
              {c.nombre}
            </option>
          ))}
        </select>
        {campos.categoria_id && <span className="error-campo">{campos.categoria_id}</span>}

        <label htmlFor="medio">¿Cómo fue el pago? <span className="req">*</span></label>
        <select
          id="medio"
          value={datos.medio_pago_id}
          onChange={(e) => cambiar('medio_pago_id', e.target.value)}
        >
          <option value="">Selecciona un medio</option>
          {medios.map((m) => (
            <option key={m.id} value={m.id}>
              {m.nombre}
            </option>
          ))}
        </select>
        {campos.medio_pago_id && <span className="error-campo">{campos.medio_pago_id}</span>}

        <div className="dos-columnas">
          <div>
            <label htmlFor="monto">Monto <span className="req">*</span></label>
            <InputMonto
              id="monto"
              valor={datos.monto}
              onCambio={(v) => cambiar('monto', v)}
              placeholder="150.000"
            />
            {campos.monto && <span className="error-campo">{campos.monto}</span>}
          </div>

          <div>
            <label htmlFor="fecha">Fecha <span className="req">*</span></label>
            <input
              id="fecha"
              type="date"
              value={datos.fecha}
              onChange={(e) => cambiar('fecha', e.target.value)}
            />
            {campos.fecha && <span className="error-campo">{campos.fecha}</span>}
          </div>
        </div>

        <label htmlFor="descripcion">Descripción <span className="tenue">(opcional)</span></label>
        <input
          id="descripcion"
          value={datos.descripcion}
          onChange={(e) => cambiar('descripcion', e.target.value)}
          placeholder="Opcional"
        />
        {campos.descripcion && <span className="error-campo">{campos.descripcion}</span>}

        {esPrestamo && (
          <div className="bloque-prestamo">
            <label htmlFor="a_quien">¿A quién le prestaste? <span className="req">*</span></label>
            <input
              id="a_quien"
              value={datos.a_quien}
              onChange={(e) => cambiar('a_quien', e.target.value)}
              placeholder="Nombre de la persona"
            />
            {campos.a_quien && <span className="error-campo">{campos.a_quien}</span>}

            <label htmlFor="estado">Estado</label>
            <div className="grupo-tipos">
              {[
                ['pendiente', 'Pendiente'],
                ['pagado', 'Ya me pagó'],
              ].map(([valor, etiqueta]) => (
                <button
                  key={valor}
                  type="button"
                  className={`chip ${datos.estado === valor ? 'chip-activo' : ''}`}
                  onClick={() => cambiar('estado', valor)}
                >
                  {etiqueta}
                </button>
              ))}
            </div>
            {campos.estado && <span className="error-campo">{campos.estado}</span>}
          </div>
        )}

        <label htmlFor="factura">Factura (opcional)</label>
        <input
          id="factura"
          type="file"
          accept="image/jpeg,image/png,image/webp,image/heic,application/pdf"
          onChange={(e) => {
            setArchivo(e.target.files?.[0] ?? null)
            setQuitarFactura(false)
          }}
        />
        {campos.factura && <span className="error-campo">{campos.factura}</span>}

        {movimiento?.factura && !archivo && (
          <label className="checkbox">
            <input
              type="checkbox"
              checked={quitarFactura}
              onChange={(e) => setQuitarFactura(e.target.checked)}
            />
            Quitar la factura actual ({movimiento.factura.nombre})
          </label>
        )}

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
