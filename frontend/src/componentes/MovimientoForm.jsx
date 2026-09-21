import { useState } from 'react'
import { movimientosApi } from '../lib/api'
import { entradaAMonto, finDeMes, hoyISO, montoAEntrada, sumarDias } from '../lib/formato'
import CubrirFaltante, { CUBRIR_VACIO, cuerpoCubrir } from './CubrirFaltante'
import InputMonto from './InputMonto'
import Modal from './Modal'

const VACIO = {
  categoria_id: '',
  medio_pago_id: '',
  medio_cobro_id: '',
  tipo: 'pague',
  monto: '',
  fecha: hoyISO(),
  descripcion: '',
  a_quien: '',
  estado: 'pendiente',
  cobrar_el: '',
}

// Los cinco tipos, con el texto que los distingue. Las dos deudas se llaman
// "Presté" y "Me prestaron" y no "Préstamo dado / recibido" porque así es como
// lo dice la gente, y confundirlas es el error más caro del formulario.
const TIPOS = [
  ['recibi', 'Recibí'],
  ['pague', 'Pagué'],
  ['preste', 'Presté'],
  ['me_prestaron', 'Me prestaron'],
  ['traslado', 'Traslado'],
]

export default function MovimientoForm({
  movimiento,
  categorias,
  medios,
  // Nombres de personas y negocios con los que ya hay cuentas. Solo se usan
  // para sugerir al escribir: "Carlos" y "Carlos M" serían dos deudores
  // distintos, y eso descuadra los dos saldos sin que nadie lo note.
  contrapartes = [],
  onCerrar,
  onGuardado,
}) {
  const esNuevo = !movimiento?.id

  const [datos, setDatos] = useState(() =>
    esNuevo
      ? { ...VACIO, categoria_id: categorias[0]?.id ?? '', medio_pago_id: medios[0]?.id ?? '' }
      : {
          categoria_id: movimiento.categoria_id,
          medio_pago_id: movimiento.medio_pago_id ?? '',
          medio_cobro_id: movimiento.medio_cobro_id ?? '',
          tipo: movimiento.tipo,
          monto: montoAEntrada(movimiento.monto),
          fecha: movimiento.fecha,
          descripcion: movimiento.descripcion,
          a_quien: movimiento.a_quien ?? '',
          estado: movimiento.estado === 'parcial' ? 'pendiente' : (movimiento.estado ?? 'pendiente'),
          cobrar_el: movimiento.cobrar_el ?? '',
        }
  )

  const [archivo, setArchivo] = useState(null)
  const [quitarFactura, setQuitarFactura] = useState(false)
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [guardando, setGuardando] = useState(false)

  // Si el medio no alcanza, el backend dice cuánto falta y aquí se pregunta
  // de dónde salió. Desde ese momento cada Guardar manda también la respuesta.
  const [falta, setFalta] = useState(null)
  const [cubrir, setCubrir] = useState(CUBRIR_VACIO)

  // Las reglas del negocio en tres variables. Todo lo que el formulario
  // muestra u oculta sale de aquí.
  const esDeuda = datos.tipo === 'preste' || datos.tipo === 'me_prestaron'
  const esPropia = datos.tipo === 'me_prestaron'
  const esTraslado = datos.tipo === 'traslado'

  // Si ya hay abonos, el estado no se elige a mano: lo dicen los abonos. El
  // formulario ni siquiera lo ofrece, para no prometer algo que el backend
  // va a ignorar.
  const tieneAbonos = (movimiento?.abonos ?? 0) > 0

  function cambiar(campo, valor) {
    setDatos((prev) => ({ ...prev, [campo]: valor }))
  }

  // Se valida aquí ADEMÁS de en el backend: aquí para que el error salga al
  // instante sin ir al servidor, y allá porque el navegador no es de fiar
  // (cualquiera puede mandar una petición saltándose el formulario).
  function faltantes() {
    const errores = {}
    if (!datos.categoria_id) errores.categoria_id = 'Selecciona una categoría'
    if (!datos.medio_pago_id) errores.medio_pago_id = 'Indica cómo fue el pago'
    if (!String(datos.monto).trim()) errores.monto = 'Este campo es obligatorio'
    if (!datos.fecha) errores.fecha = 'Este campo es obligatorio'

    if (esDeuda && !datos.a_quien.trim()) errores.a_quien = 'Este campo es obligatorio'
    // Las fechas AAAA-MM-DD se comparan bien como texto.
    if (esDeuda && datos.cobrar_el && datos.fecha && datos.cobrar_el < datos.fecha) {
      errores.cobrar_el = 'No puede ser antes de la fecha del movimiento'
    }

    if (esTraslado) {
      if (!datos.medio_cobro_id) errores.medio_cobro_id = 'Indica a qué medio pasó la plata'
      else if (String(datos.medio_cobro_id) === String(datos.medio_pago_id)) {
        errores.medio_cobro_id = 'El origen y el destino no pueden ser el mismo medio'
      }
    }
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
        // El backend ignora los campos que no aplican al tipo, pero los
        // mandamos vacíos para dejar clara la intención.
        medio_cobro_id: esTraslado ? Number(datos.medio_cobro_id) : 0,
        tipo: datos.tipo,
        // De "1.500.000,50" a "1500000.50": la API siempre recibe el número crudo.
        monto: entradaAMonto(datos.monto),
        fecha: datos.fecha,
        descripcion: datos.descripcion,
        a_quien: esDeuda ? datos.a_quien : '',
        estado: esDeuda ? datos.estado : '',
        cobrar_el: esDeuda ? datos.cobrar_el : '',
        // Si con los cambios ya alcanza, el backend lo ignora.
        cubrir: falta ? cuerpoCubrir(cubrir, falta) : undefined,
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
      if (err.faltaPlata) {
        // El recuadro ya dice lo que pasa: repetirlo arriba sería ruido.
        setFalta(err.faltaPlata)
        setError('')
      } else {
        setError(err.message)
      }
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
          {TIPOS.map(([valor, etiqueta]) => (
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

        {esTraslado && (
          <p className="ayuda-campo tenue">
            Un traslado no cambia cuánta plata tienes: solo la mueve de un medio a otro.
            No cuenta como ingreso ni como gasto.
          </p>
        )}

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

        <label htmlFor="medio">
          {esTraslado ? '¿De dónde sale?' : '¿Cómo fue el pago?'} <span className="req">*</span>
        </label>
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

        {esTraslado && (
          <>
            <label htmlFor="destino">¿A dónde entra? <span className="req">*</span></label>
            <select
              id="destino"
              value={datos.medio_cobro_id}
              onChange={(e) => cambiar('medio_cobro_id', e.target.value)}
            >
              <option value="">Selecciona un medio</option>
              {medios
                // El origen no puede ser también el destino: ofrecerlo solo
                // sirve para que alguien lo escoja y reciba un error.
                .filter((m) => String(m.id) !== String(datos.medio_pago_id))
                .map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.nombre}
                  </option>
                ))}
            </select>
            {campos.medio_cobro_id && <span className="error-campo">{campos.medio_cobro_id}</span>}
          </>
        )}

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

        {esDeuda && (
          <div className="bloque-prestamo">
            <label htmlFor="a_quien">
              {esPropia ? '¿Quién te prestó?' : '¿A quién le prestaste?'}{' '}
              <span className="req">*</span>
            </label>
            <input
              id="a_quien"
              list="contrapartes-conocidas"
              value={datos.a_quien}
              onChange={(e) => cambiar('a_quien', e.target.value)}
              placeholder="Nombre de la persona o del negocio"
            />
            {/* Un datalist y no un select: puede ser alguien nuevo. Sugerir los
                que ya existen evita que "Carlos" y "carlos " terminen siendo
                dos deudores distintos con la mitad del saldo cada uno. */}
            <datalist id="contrapartes-conocidas">
              {contrapartes.map((nombre) => (
                <option key={nombre} value={nombre} />
              ))}
            </datalist>
            {campos.a_quien && <span className="error-campo">{campos.a_quien}</span>}

            {tieneAbonos ? (
              <p className="ayuda-campo tenue">
                Esta deuda ya tiene {movimiento.abonos} abono
                {movimiento.abonos === 1 ? '' : 's'}: el estado lo deciden ellos, no este
                formulario. Para registrar otro pago, usa <strong>Abonos</strong> en la lista.
              </p>
            ) : (
              <>
                <label htmlFor="estado">Estado</label>
                <div className="grupo-tipos">
                  {[
                    ['pendiente', 'Pendiente'],
                    ['pagado', esPropia ? 'Ya le pagué' : 'Ya me pagó'],
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
              </>
            )}

            <label htmlFor="cobrar_el">
              {esPropia ? '¿Cuándo le pagas?' : '¿Cuándo te paga?'}{' '}
              <span className="tenue">(fecha acordada, opcional)</span>
            </label>
            <input
              id="cobrar_el"
              type="date"
              value={datos.cobrar_el}
              min={datos.fecha || undefined}
              onChange={(e) => cambiar('cobrar_el', e.target.value)}
            />
            {/* Atajos para lo que se suele acordar, contados desde el día del
                préstamo. */}
            {datos.fecha && (
              <div className="grupo-tipos atajos-cobro">
                {[
                  ['En 8 días', sumarDias(datos.fecha, 8)],
                  ['En 15 días', sumarDias(datos.fecha, 15)],
                  ['En un mes', sumarDias(datos.fecha, 30)],
                  ['Fin de mes', finDeMes(datos.fecha)],
                ].map(([etiqueta, valor]) => (
                  <button
                    key={etiqueta}
                    type="button"
                    className={`chip ${datos.cobrar_el === valor ? 'chip-activo' : ''}`}
                    onClick={() => cambiar('cobrar_el', datos.cobrar_el === valor ? '' : valor)}
                  >
                    {etiqueta}
                  </button>
                ))}
              </div>
            )}
            {campos.cobrar_el ? (
              <span className="error-campo">{campos.cobrar_el}</span>
            ) : (
              <span className="ayuda-campo tenue">
                {datos.cobrar_el
                  ? 'Ese día te llega un aviso.'
                  : 'Si quedaron en una fecha, ponla y ese día te avisamos.'}
              </span>
            )}

            {esNuevo && (
              <p className="ayuda-campo tenue">
                Si van a pagar por cuotas, guarda primero y luego abre{' '}
                <strong>Abonos</strong> en la lista para armar el acuerdo.
              </p>
            )}
          </div>
        )}

        <label htmlFor="factura">
          {datos.tipo === 'pague' ? '¿Tienes la factura? Sube la foto o el PDF' : 'Factura'}{' '}
          <span className="tenue">(opcional)</span>
        </label>
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

        {falta && (
          <CubrirFaltante
            falta={falta}
            valor={cubrir}
            onCambio={setCubrir}
            categorias={categorias}
            medios={medios}
            contrapartes={contrapartes}
            fecha={datos.fecha}
            campos={campos}
          />
        )}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {guardando ? 'Guardando...' : falta ? 'Guardar todo' : 'Guardar'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
