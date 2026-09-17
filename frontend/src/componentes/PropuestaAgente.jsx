import { useState } from 'react'
import { agenteApi, movimientosApi } from '../lib/api'
import { entradaAMonto, formatearMonto, montoAEntrada, ETIQUETAS_TIPO } from '../lib/formato'
import { BotonAdjuntar, VistaAdjunto } from './AdjuntoFactura'
import InputMonto from './InputMonto'

// La tarjeta que aparece cuando el asistente prepara algo.
//
// Es editable a propósito, y esa es media gracia del asunto: el modelo oye
// "cuarenta y cinco mil" y casi siempre escribe 45.000, pero el día que
// escriba 450.000 el usuario tiene que poder arreglarlo ANTES de guardar, no
// descubrirlo cuadrando el mes. Hasta que no le dé a Guardar, en la base del
// dinero no ha pasado nada.
//
// `factura` es una foto que el usuario ya adjuntó en el chat: la tarjeta la
// muestra y la sube como factura del movimiento al guardarlo.
export default function PropuestaAgente({ propuesta, categorias, medios, factura, onResuelta }) {
  if (propuesta.tipo === 'marcar_pagado') {
    return <CobroDePrestamo propuesta={propuesta} medios={medios} onResuelta={onResuelta} />
  }
  return (
    <MovimientoNuevo
      propuesta={propuesta}
      categorias={categorias}
      medios={medios}
      facturaInicial={factura}
      onResuelta={onResuelta}
    />
  )
}

function MovimientoNuevo({ propuesta, categorias, medios, facturaInicial, onResuelta }) {
  const datos = propuesta.datos

  const [tipo, setTipo] = useState(datos.tipo)
  const [monto, setMonto] = useState(montoAEntrada(datos.monto))
  const [fecha, setFecha] = useState(datos.fecha)
  const [descripcion, setDescripcion] = useState(datos.descripcion ?? '')
  const [categoriaID, setCategoriaID] = useState(datos.categoria_id ?? 0)
  const [medioID, setMedioID] = useState(datos.medio_pago_id ?? 0)
  const [aQuien, setAQuien] = useState(datos.a_quien ?? '')
  const [estado, setEstado] = useState(datos.estado || 'pendiente')
  const [cobrarEl, setCobrarEl] = useState(datos.cobrar_el ?? '')
  const [factura, setFactura] = useState(facturaInicial ?? null)
  // Si el usuario usa el clip del chat con la tarjeta ya abierta, la foto
  // llega después: se toma en cuanto cambia.
  const [ultimaRecibida, setUltimaRecibida] = useState(facturaInicial)
  if (facturaInicial !== ultimaRecibida) {
    setUltimaRecibida(facturaInicial)
    if (facturaInicial) setFactura(facturaInicial)
  }

  const esPrestamo = tipo === 'preste'

  function cuerpo() {
    return {
      categoria_id: Number(categoriaID),
      medio_pago_id: Number(medioID),
      tipo,
      monto: entradaAMonto(monto),
      fecha,
      descripcion,
      // El backend ignora estos dos cuando el tipo no es "presté", así que
      // cambiar de tipo no obliga a limpiar nada.
      a_quien: esPrestamo ? aQuien : '',
      estado: esPrestamo ? estado : '',
      cobrar_el: esPrestamo ? cobrarEl : '',
    }
  }

  return (
    <Tarjeta
      propuesta={propuesta}
      onResuelta={onResuelta}
      cuerpo={cuerpo}
      etiqueta="Guardar"
      despues={factura ? (movimiento) => movimientosApi.subirFactura(movimiento.id, factura) : null}
    >
      {(campos) => (
        <div className="propuesta-campos">
          <div className="propuesta-campo">
            <label htmlFor={`tipo-${propuesta.id}`}>Tipo</label>
            <select id={`tipo-${propuesta.id}`} value={tipo} onChange={(e) => setTipo(e.target.value)}>
              {Object.entries(ETIQUETAS_TIPO).map(([valor, etiqueta]) => (
                <option key={valor} value={valor}>
                  {etiqueta}
                </option>
              ))}
            </select>
            {campos.tipo && <span className="error-campo">{campos.tipo}</span>}
          </div>

          <div className="propuesta-campo">
            <label htmlFor={`monto-${propuesta.id}`}>Monto</label>
            <InputMonto id={`monto-${propuesta.id}`} valor={monto} onCambio={setMonto} />
            {campos.monto && <span className="error-campo">{campos.monto}</span>}
          </div>

          <div className="propuesta-campo">
            <label htmlFor={`fecha-${propuesta.id}`}>Fecha</label>
            <input
              id={`fecha-${propuesta.id}`}
              type="date"
              value={fecha}
              onChange={(e) => setFecha(e.target.value)}
            />
            {campos.fecha && <span className="error-campo">{campos.fecha}</span>}
          </div>

          <div className="propuesta-campo">
            <label htmlFor={`categoria-${propuesta.id}`}>Categoría</label>
            <select
              id={`categoria-${propuesta.id}`}
              value={categoriaID}
              onChange={(e) => setCategoriaID(e.target.value)}
            >
              {categorias.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.nombre}
                </option>
              ))}
            </select>
            {campos.categoria_id && <span className="error-campo">{campos.categoria_id}</span>}
          </div>

          <div className="propuesta-campo">
            <label htmlFor={`medio-${propuesta.id}`}>Medio</label>
            <select
              id={`medio-${propuesta.id}`}
              value={medioID}
              onChange={(e) => setMedioID(e.target.value)}
            >
              {medios.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.nombre}
                </option>
              ))}
            </select>
            {campos.medio_pago_id && <span className="error-campo">{campos.medio_pago_id}</span>}
          </div>

          <div className="propuesta-campo crece">
            <label htmlFor={`descripcion-${propuesta.id}`}>Descripción</label>
            <input
              id={`descripcion-${propuesta.id}`}
              value={descripcion}
              onChange={(e) => setDescripcion(e.target.value)}
            />
            {campos.descripcion && <span className="error-campo">{campos.descripcion}</span>}
          </div>

          {esPrestamo && (
            <>
              <div className="propuesta-campo">
                <label htmlFor={`aquien-${propuesta.id}`}>¿A quién?</label>
                <input
                  id={`aquien-${propuesta.id}`}
                  value={aQuien}
                  onChange={(e) => setAQuien(e.target.value)}
                />
                {campos.a_quien && <span className="error-campo">{campos.a_quien}</span>}
              </div>

              <div className="propuesta-campo">
                <label htmlFor={`estado-${propuesta.id}`}>Estado</label>
                <select
                  id={`estado-${propuesta.id}`}
                  value={estado}
                  onChange={(e) => setEstado(e.target.value)}
                >
                  <option value="pendiente">Pendiente</option>
                  <option value="pagado">Pagado</option>
                </select>
                {campos.estado && <span className="error-campo">{campos.estado}</span>}
              </div>

              <div className="propuesta-campo">
                <label htmlFor={`cobrar-${propuesta.id}`}>¿Cuándo te paga?</label>
                <input
                  id={`cobrar-${propuesta.id}`}
                  type="date"
                  value={cobrarEl}
                  min={fecha || undefined}
                  onChange={(e) => setCobrarEl(e.target.value)}
                />
                {campos.cobrar_el && <span className="error-campo">{campos.cobrar_el}</span>}
              </div>
            </>
          )}

          {/* La factura: en un gasto se pide de frente (es lo que el
              contador quiere ver); en lo demás queda como opción discreta. */}
          <div className={`propuesta-campo crece ${tipo === 'pague' && !factura ? 'pide-factura' : ''}`}>
            {factura ? (
              <VistaAdjunto archivo={factura} texto="Factura adjunta" onQuitar={() => setFactura(null)} />
            ) : (
              <>
                {tipo === 'pague' && (
                  <span className="pide-factura-texto">
                    ¿Tienes la factura? Adjunta la foto o el PDF.
                  </span>
                )}
                <BotonAdjuntar onArchivo={setFactura} className="secundario boton-factura">
                  📎 Adjuntar factura
                </BotonAdjuntar>
              </>
            )}
          </div>
        </div>
      )}
    </Tarjeta>
  )
}

function CobroDePrestamo({ propuesta, medios, onResuelta }) {
  const datos = propuesta.datos
  const [medioID, setMedioID] = useState(datos.medio_cobro_id ?? 0)

  return (
    <Tarjeta
      propuesta={propuesta}
      onResuelta={onResuelta}
      cuerpo={() => ({ medio_cobro_id: Number(medioID) })}
      etiqueta="Marcar pagado"
    >
      {(campos) => (
        <>
          <p className="propuesta-resumen">
            <strong>{datos.a_quien}</strong> te devolvió {formatearMonto(datos.monto)}
            {datos.descripcion ? ` (${datos.descripcion})` : ''}.
          </p>

          <div className="propuesta-campos">
            <div className="propuesta-campo crece">
              <label htmlFor={`cobro-${propuesta.id}`}>¿Por dónde te pagaron?</label>
              <select
                id={`cobro-${propuesta.id}`}
                value={medioID}
                onChange={(e) => setMedioID(e.target.value)}
              >
                {/* Sin registrar es una opción de verdad: a veces no se sabe,
                    y obligar a inventarlo descuadraría el saldo de un medio. */}
                <option value={0}>Sin registrar</option>
                {medios.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.nombre}
                  </option>
                ))}
              </select>
              {campos.medio_cobro_id && <span className="error-campo">{campos.medio_cobro_id}</span>}
            </div>
          </div>
        </>
      )}
    </Tarjeta>
  )
}

// Tarjeta es la parte común: el marco, los dos botones y el manejo de errores.
// Los campos los pone cada tipo de propuesta, que es lo único que cambia.
//
// `despues` es opcional: lo que hay que hacer con el movimiento ya creado (subir
// su factura). Si falla, el movimiento YA quedó guardado: la tarjeta se cierra
// igual y se avisa, para que no se intente guardar dos veces.
function Tarjeta({ propuesta, onResuelta, cuerpo, etiqueta, despues, children }) {
  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  const [ocupado, setOcupado] = useState(false)

  async function guardar() {
    setError('')
    setCampos({})
    setOcupado(true)
    try {
      let movimiento = await agenteApi.confirmarPropuesta(propuesta.id, cuerpo())
      let aviso = ''
      if (despues) {
        try {
          movimiento = (await despues(movimiento)) ?? movimiento
        } catch (err) {
          aviso = `El movimiento quedó guardado, pero la factura no se pudo subir (${err.message}). Puedes adjuntarla desde Movimientos.`
        }
      }
      onResuelta(propuesta.id, movimiento, aviso)
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setOcupado(false)
    }
  }

  async function descartar() {
    setOcupado(true)
    try {
      await agenteApi.descartarPropuesta(propuesta.id)
      onResuelta(propuesta.id, null)
    } catch (err) {
      setError(err.message)
      setOcupado(false)
    }
  }

  return (
    <section className="propuesta">
      <header className="propuesta-encabezado">
        <strong>Revisa antes de guardar</strong>
        <span className="tenue">Nada se ha guardado todavía</span>
      </header>

      {error && <div className="alerta">{error}</div>}

      {children(campos)}

      <div className="propuesta-acciones">
        <button className="secundario" onClick={descartar} disabled={ocupado}>
          Descartar
        </button>
        <button onClick={guardar} disabled={ocupado}>
          {ocupado ? 'Guardando...' : etiqueta}
        </button>
      </div>
    </section>
  )
}
