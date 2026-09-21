import { useCallback, useEffect, useState } from 'react'
import { categoriasApi, mediosApi, recurrentesApi } from '../lib/api'
import { diasHasta, entradaAMonto, formatearFecha, formatearMonto, montoAEntrada } from '../lib/formato'
import { avisarMovimientoGuardado, useAlGuardarMovimiento } from '../lib/eventos'
import CubrirFaltante, { CUBRIR_VACIO, cuerpoCubrir } from './CubrirFaltante'
import InputMonto from './InputMonto'
import Modal from './Modal'

// Lo que toca confirmar de los gastos e ingresos que se repiten, y —con
// `conProximas`— lo que se viene en los próximos días.
//
// Sale en el resumen y en la página de Recurrentes, arriba de todo, porque es
// lo único de esas pantallas que pide una acción HOY. Si no hay nada
// pendiente, no ocupa ni un pixel.
//
// La app no registra el arriendo sola a propósito: un gasto inventado no se
// nota nunca, mientras que uno olvidado salta al cuadrar el mes. Aquí se ve lo
// que tocaba y se confirma con un clic — o se corrige el monto antes, que es
// lo que pasa con el recibo de la luz todos los meses.
//
// "Lo que viene" es esa misma idea corrida unos días: el gasto ya está encima
// aunque todavía no venza, y verlo es la mitad de poder prepararse. No toca
// ninguna cifra hasta que se confirma — y se puede confirmar antes, porque el
// arriendo del 5 a veces se paga el 2.
export default function RecurrentesPendientes({ onConfirmado, conProximas = false }) {
  const [pendientes, setPendientes] = useState([])
  const [proximas, setProximas] = useState([])
  const [diasFuturos, setDiasFuturos] = useState(30)
  const [error, setError] = useState('')
  const [ocupado, setOcupado] = useState(null)
  const [corrigiendo, setCorrigiendo] = useState(null)

  // Si el medio no alcanza para lo que se confirma, se pregunta de dónde
  // salió el resto en un modal. Las categorías y los medios solo se cargan
  // entonces: el resto del tiempo este componente no los necesita.
  const [cubriendo, setCubriendo] = useState(null) // { o, monto, falta }
  const [cubrir, setCubrir] = useState(CUBRIR_VACIO)
  const [camposCubrir, setCamposCubrir] = useState({})
  const [listas, setListas] = useState({ categorias: [], medios: [] })

  const cargar = useCallback(async (senal) => {
    try {
      const datos = await recurrentesApi.pendientes(senal)
      setPendientes(datos.pendientes)
      // Un servidor viejo no manda estas dos: la sección simplemente no sale.
      setProximas(datos.proximas ?? [])
      if (datos.dias_futuros) setDiasFuturos(datos.dias_futuros)
    } catch (err) {
      if (err.name === 'AbortError') return
      // Un 404 significa que la ruta no existe (servidor viejo): no es un
      // error que valga la pena mostrarle a nadie.
      if (err.status !== 404) setError(err.message)
    }
  }, [])

  useEffect(() => {
    const control = new AbortController()
    cargar(control.signal)
    return () => control.abort()
  }, [cargar])

  // El asistente también puede confirmar uno de estos ("ya pagué el internet").
  // Sin esto, el que acaba de quedar registrado seguiría pintado aquí hasta
  // recargar la página, y parecería que hay que confirmarlo otra vez.
  useAlGuardarMovimiento(() => cargar())

  async function confirmar(o, monto, datosCubrir) {
    setOcupado(o.id)
    setError('')
    try {
      // Sin monto se confirma la plantilla tal cual; con monto, se manda el
      // movimiento completo porque el backend valida la entrada entera. El
      // backend lee el cuerpo ENCIMA de la plantilla, así que `cubrir` solo
      // también sirve.
      let cuerpo = monto
        ? {
            categoria_id: o.categoria_id,
            medio_pago_id: o.medio_pago_id,
            tipo: o.tipo,
            monto,
            fecha: o.fecha,
            descripcion: o.descripcion,
          }
        : undefined
      if (datosCubrir) cuerpo = { ...cuerpo, cubrir: cuerpoCubrir(datosCubrir) }

      await recurrentesApi.confirmar(o.id, cuerpo)
      setCubriendo(null)
      setPendientes((prev) => prev.filter((x) => x.id !== o.id))
      setProximas((prev) => prev.filter((x) => x.id !== o.id))
      setCorrigiendo(null)
      // El mismo evento que usa el asistente: el resumen y la lista de
      // movimientos se actualizan solos, sin recargar la página.
      avisarMovimientoGuardado()
      onConfirmado?.()
    } catch (err) {
      if (err.faltaPlata) {
        await preguntarDeDonde(o, monto, err.faltaPlata)
        setCamposCubrir(err.campos ?? {})
      } else if (datosCubrir && Object.keys(err.campos ?? {}).length > 0) {
        setCamposCubrir(err.campos)
      } else {
        setError(err.message)
        setCubriendo(null)
      }
    } finally {
      setOcupado(null)
    }
  }

  async function preguntarDeDonde(o, monto, falta) {
    if (listas.medios.length === 0) {
      try {
        const [categorias, medios] = await Promise.all([categoriasApi.listar(), mediosApi.listar()])
        setListas({ categorias, medios })
      } catch (err) {
        setError(err.message)
        return
      }
    }
    if (cubriendo?.o.id !== o.id) setCubrir(CUBRIR_VACIO)
    setCubriendo({ o, monto, falta })
  }

  async function descartar(o) {
    if (!confirm(`¿Descartar "${o.descripcion}" del ${formatearFecha(o.fecha)}? No se registra nada.`)) {
      return
    }
    setOcupado(o.id)
    setError('')
    try {
      await recurrentesApi.descartar(o.id)
      setPendientes((prev) => prev.filter((x) => x.id !== o.id))
      setProximas((prev) => prev.filter((x) => x.id !== o.id))
    } catch (err) {
      setError(err.message)
    } finally {
      setOcupado(null)
    }
  }

  const mostrarProximas = conProximas && proximas.length > 0
  if (pendientes.length === 0 && !mostrarProximas && !error) return null

  // La fila es la misma en las dos secciones. Lo único que cambia es cómo se
  // dice la fecha y el verbo del botón: "lo pagué" ya pasó, "ya lo pagué" es
  // adelantarse.
  const fila = (o, futura) => (
    <article key={o.id} className="pendiente">
      <div className="pendiente-datos">
        <strong>{o.descripcion}</strong>
        <span className="tenue">
          {futura ? cuandoToca(o.fecha) : formatearFecha(o.fecha)} · {o.categoria_nombre} ·{' '}
          {o.medio_pago_nombre}
        </span>
      </div>

      {corrigiendo === o.id ? (
        <CorregirMonto
          ocurrencia={o}
          ocupado={ocupado === o.id}
          onCancelar={() => setCorrigiendo(null)}
          onConfirmar={(monto) => confirmar(o, monto)}
        />
      ) : (
        <div className="pendiente-acciones">
          <strong className={o.tipo === 'recibi' ? 'positivo' : 'negativo'}>
            {o.tipo === 'recibi' ? '+' : '−'} {formatearMonto(o.monto)}
          </strong>
          <button
            className="principal-pagar"
            disabled={ocupado === o.id}
            onClick={() => confirmar(o)}
          >
            {ocupado === o.id ? '...' : etiquetaConfirmar(o.tipo, futura)}
          </button>
          <button className="menor" onClick={() => setCorrigiendo(o.id)}>
            Otro monto
          </button>
          <button className="menor menor-peligro" onClick={() => descartar(o)}>
            {futura ? 'Este no va' : 'Este mes no'}
          </button>
        </div>
      )}
    </article>
  )

  return (
    <>
      {cubriendo && (
        <Modal titulo="¿De dónde salió la plata?" onCerrar={() => setCubriendo(null)}>
          <p>
            <strong>{cubriendo.o.descripcion}</strong> · {formatearFecha(cubriendo.o.fecha)}
          </p>
          <CubrirFaltante
            falta={cubriendo.falta}
            valor={cubrir}
            onCambio={setCubrir}
            categorias={listas.categorias}
            medios={listas.medios}
            fecha={cubriendo.o.fecha}
            campos={camposCubrir}
          />
          <div className="acciones-modal">
            <button type="button" className="secundario" onClick={() => setCubriendo(null)}>
              Cancelar
            </button>
            <button
              type="button"
              disabled={ocupado === cubriendo.o.id}
              onClick={() => confirmar(cubriendo.o, cubriendo.monto, cubrir)}
            >
              {ocupado === cubriendo.o.id ? 'Guardando...' : 'Guardar las dos cosas'}
            </button>
          </div>
        </Modal>
      )}

      {(pendientes.length > 0 || error) && (
        <section className="tarjeta pendientes-recurrentes">
          <h2>
            Por confirmar{' '}
            <span className="contador">{pendientes.length}</span>
          </h2>
          <p className="subtitulo">Gastos e ingresos que se repiten y ya les tocaba</p>

          {error && <div className="alerta">{error}</div>}

          {pendientes.map((o) => fila(o, false))}
        </section>
      )}

      {mostrarProximas && (
        <section className="tarjeta pendientes-recurrentes proximas-recurrentes">
          <h2>
            Lo que viene{' '}
            <span className="contador">{proximas.length}</span>
          </h2>
          <p className="subtitulo">
            Se repite en los próximos {diasFuturos} días. Nada de esto se ha descontado todavía:
            confírmalo el día que toque, o ya mismo si lo pagaste antes.
          </p>

          <Totales ocurrencias={proximas} />

          {proximas.map((o) => fila(o, true))}
        </section>
      )}
    </>
  )
}

function etiquetaConfirmar(tipo, futura) {
  if (tipo === 'recibi') return futura ? '✓ Ya lo recibí' : '✓ Lo recibí'
  return futura ? '✓ Ya lo pagué' : '✓ Lo pagué'
}

// Cuánto falta, dicho como lo diría una persona. La fecha sola ("5 de
// octubre") obliga a hacer la cuenta mental; "en 3 días" no.
function cuandoToca(iso) {
  const dias = diasHasta(iso)
  if (dias <= 0) return 'hoy'
  if (dias === 1) return `mañana, ${formatearFecha(iso)}`
  if (dias <= 7) return `en ${dias} días, ${formatearFecha(iso)}`
  return formatearFecha(iso)
}

// Lo que suma lo que viene. Los gastos y los ingresos van por separado a
// propósito: restarlos daría un número que no es ninguna de las dos cosas, y
// lo que la persona necesita saber es cuánto tiene que tener listo.
function Totales({ ocurrencias }) {
  const suma = (tipo) =>
    ocurrencias
      .filter((o) => o.tipo === tipo)
      // En centavos, que son enteros: ir sumando "1200000.00" como decimal
      // acumula el error de la coma flotante peso a peso.
      .reduce((total, o) => total + Math.round(Number(o.monto) * 100), 0) / 100

  const pagar = suma('pague')
  const recibir = suma('recibi')

  return (
    <div className="proximas-totales">
      {pagar > 0 && (
        <p>
          Vas a pagar <strong className="negativo">{formatearMonto(pagar)}</strong>
        </p>
      )}
      {recibir > 0 && (
        <p>
          Vas a recibir <strong className="positivo">{formatearMonto(recibir)}</strong>
        </p>
      )}
    </div>
  )
}

// El recibo de la luz nunca llega por el mismo valor. Corregir el monto antes
// de confirmar es el caso normal, no la excepción.
function CorregirMonto({ ocurrencia, ocupado, onCancelar, onConfirmar }) {
  const [monto, setMonto] = useState(montoAEntrada(ocurrencia.monto))

  return (
    <div className="pendiente-corregir">
      <InputMonto
        valor={monto}
        onCambio={setMonto}
        placeholder={montoAEntrada(ocurrencia.monto)}
      />
      <button disabled={ocupado} onClick={() => onConfirmar(entradaAMonto(monto))}>
        {ocupado ? '...' : 'Confirmar'}
      </button>
      <button className="secundario" onClick={onCancelar}>
        Cancelar
      </button>
    </div>
  )
}
