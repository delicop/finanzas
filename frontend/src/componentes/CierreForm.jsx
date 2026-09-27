import { useState } from 'react'
import { cierresApi, descargarFotoCierre } from '../lib/api'
import { entradaAMonto, formatearFecha, formatearMonto, hoyISO, montoAEntrada } from '../lib/formato'
import InputMonto from './InputMonto'
import Modal from './Modal'
import Icono from './Icono'

// La hoja del cierre de caja, igual que la de papel.
//
// Las casillas que se escriben son las que guarda el servidor. Las
// diferencias y los totales NO se escriben: se calculan aquí mientras llenas
// y los vuelve a calcular el backend al leer, siempre a partir de las mismas
// cifras. Una resta guardada es un dato repetido que el día de una corrección
// queda mintiendo.
//
// La columna de la derecha en cada bloque lleva el nombre de la tienda: es lo
// que contó el local, contra lo que reporta el banco o el datáfono.

// Las cinco listas de la hoja, en el mismo orden.
const LISTAS = [
  ['pago_nequi', 'Pagos Nequi'],
  ['compra', 'Compras'],
  ['gasto', 'Gastos'],
  ['descuento', 'Descuentos'],
  ['vale', 'Vales empleados'],
]

const MONTOS = [
  'qr_banco',
  'qr_tienda',
  'datafono_reporte',
  'datafono_tienda',
  'efectivo_billete',
  'efectivo_moneda',
  'efectivo_tienda',
  'venta_tienda',
]

// Una fila en blanco al final de cada lista para que siempre se pueda seguir
// escribiendo sin tener que darle antes a "Agregar".
const FILA_VACIA = { descripcion: '', monto: '' }

function listasDe(cierre) {
  const grupos = Object.fromEntries(LISTAS.map(([clave]) => [clave, []]))
  for (const l of cierre?.lineas ?? []) {
    if (grupos[l.grupo]) {
      grupos[l.grupo].push({ descripcion: l.descripcion, monto: montoAEntrada(l.monto) })
    }
  }
  for (const [clave] of LISTAS) {
    if (grupos[clave].length === 0) grupos[clave] = [{ ...FILA_VACIA }]
  }
  return grupos
}

// De "1.441.000" al número con el que se hacen las restas de la pantalla.
// Para guardar se manda el texto crudo; la plata de verdad la calcula el
// servidor sobre columnas NUMERIC.
function num(entrada) {
  return Number(entradaAMonto(entrada || '')) || 0
}

// Un monto escrito y ningún nombre: ese renglón no se suma ni se guarda, así
// que no puede quedar invisible. No bloquea el guardado — la hoja de papel
// también se entrega con renglones a medio llenar —, solo se marca.
function aMedias(fila) {
  return num(fila.monto) > 0 && fila.descripcion.trim() === ''
}

export default function CierreForm({ tiendas, tiendaID, cierre, onCerrar, onGuardado }) {
  const esNuevo = !cierre?.id

  // De qué tienda es la hoja. Al editar no se cambia: el cierre vive colgado
  // de su local y moverlo sería otra hoja, no la misma.
  //
  // Tres candidatos, en orden: la tienda del cierre que se edita, la que venga
  // preseleccionada, y la primera de la lista. Ojo con `??`: `tiendaID` llega
  // como cadena vacía cuando no hay filtro en la URL, y `??` no la salta. Y
  // solo vale el candidato que siga en la lista, porque el de la URL puede
  // apuntar a una tienda ya borrada: un valor de la URL no es estado del
  // formulario.
  const tiendaInicial =
    [cierre?.tienda_id, tiendaID, tiendas[0]?.id]
      .map((v) => (v === null || v === undefined ? '' : String(v)))
      .find((v) => v !== '' && tiendas.some((t) => String(t.id) === v)) ?? ''
  const [elegida, setElegida] = useState(tiendaInicial)
  const tienda = tiendas.find((t) => String(t.id) === elegida) ?? tiendas[0] ?? { nombre: 'la tienda' }

  const [fecha, setFecha] = useState(cierre?.fecha ?? hoyISO())
  const [responsable, setResponsable] = useState(cierre?.responsable ?? '')
  const [novedades, setNovedades] = useState(cierre?.novedades ?? '')
  const [montos, setMontos] = useState(() =>
    Object.fromEntries(MONTOS.map((clave) => [clave, montoAEntrada(cierre?.[clave] ?? '')])),
  )
  const [listas, setListas] = useState(() => listasDe(cierre))

  const [campos, setCampos] = useState({})
  const [error, setError] = useState('')
  // '' = quieto, 'hoja' = guardando el cierre, 'foto' = subiendo la imagen.
  // Son dos peticiones y la segunda puede tardar, así que el botón dice en cuál
  // va en vez de quedarse en "Guardando..." como si se hubiera trabado.
  const [fase, setFase] = useState('')
  const guardando = fase !== ''

  // La foto de la hoja firmada. Se sube aparte, después de guardar, porque va
  // en multipart y porque un cierre nuevo todavía no tiene id al que pegarla.
  const [archivo, setArchivo] = useState(null)
  const [quitarFoto, setQuitarFoto] = useState(false)

  // Si algo se tocó desde que se abrió la hoja. No compara estado por
  // estado: basta con marcarla la primera vez que se escribe algo, para
  // preguntar antes de perderla al salir sin guardar.
  const [sucio, setSucio] = useState(false)

  function cambiarMonto(clave, valor) {
    setSucio(true)
    setMontos((m) => ({ ...m, [clave]: valor }))
  }

  function cambiarLinea(grupo, indice, campo, valor) {
    setSucio(true)
    setListas((l) => ({
      ...l,
      [grupo]: l[grupo].map((fila, i) => (i === indice ? { ...fila, [campo]: valor } : fila)),
    }))
  }

  function agregarFila(grupo) {
    setSucio(true)
    setListas((l) => ({ ...l, [grupo]: [...l[grupo], { ...FILA_VACIA }] }))
  }

  function quitarFila(grupo, indice) {
    setSucio(true)
    setListas((l) => {
      const quedan = l[grupo].filter((_, i) => i !== indice)
      return { ...l, [grupo]: quedan.length ? quedan : [{ ...FILA_VACIA }] }
    })
  }

  // --- lo calculado ---------------------------------------------------
  const qrDiferencia = num(montos.qr_tienda) - num(montos.qr_banco)
  const datafonoDiferencia = num(montos.datafono_tienda) - num(montos.datafono_reporte)
  const efectivoTotal = num(montos.efectivo_billete) + num(montos.efectivo_moneda)
  const efectivoDiferencia = efectivoTotal - num(montos.efectivo_tienda)
  const metodosPago = num(montos.qr_banco) + num(montos.datafono_reporte) + efectivoTotal
  const ventaDiferencia = metodosPago - num(montos.venta_tienda)

  // Un renglón cuenta cuando tiene descripción: un monto sin nombre no es un
  // gasto, es un renglón a medio escribir. La usan el total que se pinta y el
  // cuerpo que se envía: si cada uno filtrara por su cuenta, la pantalla
  // podría cuadrar una hoja que el servidor guarda descuadrada.
  const filasValidas = (grupo) => listas[grupo].filter((f) => f.descripcion.trim() !== '')

  // El servidor nombra el error de un renglón por su posición dentro del grupo
  // en lo que se envió, y lo que se envió son solo las filas con descripción:
  // la fila i de la pantalla es la n-ésima con nombre de su lista.
  const errorDeFila = (grupo, i) => {
    if (listas[grupo][i].descripcion.trim() === '') return undefined
    const n = listas[grupo].slice(0, i).filter((f) => f.descripcion.trim() !== '').length
    return campos[`lineas.${grupo}.${n}`]
  }

  const totalDe = (grupo) => filasValidas(grupo).reduce((suma, fila) => suma + num(fila.monto), 0)

  // La cuenta que cierra la hoja: la venta del día menos lo que salió de la
  // caja tiene que dar lo que hay en caja y bancos. Los pagos por Nequi no se
  // restan (ver docs/decisiones.md).
  const salidas = totalDe('compra') + totalDe('gasto') + totalDe('descuento') + totalDe('vale')
  const deberiaQuedar = num(montos.venta_tienda) - salidas
  const quedaDiferencia = metodosPago - deberiaQuedar

  // Abrir la foto en otra pestaña: el navegador no puede pedirla solo porque
  // la ruta necesita el token, así que se baja aquí y se abre desde memoria.
  async function verFoto() {
    try {
      const { url } = await descargarFotoCierre(cierre.tienda_id, cierre.id)
      window.open(url, '_blank', 'noopener')
      // Se suelta después: revocarla de una cerraría la pestaña recién abierta.
      setTimeout(() => URL.revokeObjectURL(url), 60_000)
    } catch (err) {
      setError(err.message)
    }
  }

  async function onSubmit(e) {
    e.preventDefault()
    setError('')
    setCampos({})

    // Sin tienda no hay a dónde mandar la hoja: la URL saldría partida
    // (`/api/tiendas//cierres`) y el servidor contestaría 400 después de que
    // la persona llenó cuarenta campos.
    if (esNuevo && !elegida) {
      setCampos({ tienda: 'Elige la tienda' })
      return
    }

    setFase('hoja')

    const cuerpo = {
      fecha,
      responsable,
      novedades,
      ...Object.fromEntries(MONTOS.map((clave) => [clave, entradaAMonto(montos[clave] || '')])),
      // Las filas sin descripción no se mandan: son renglones que no se usaron.
      // Es la misma regla con la que se pintaron los totales de la pantalla.
      lineas: LISTAS.flatMap(([grupo]) =>
        filasValidas(grupo).map((fila) => ({
          grupo,
          descripcion: fila.descripcion,
          monto: entradaAMonto(fila.monto || ''),
        })),
      ),
    }

    try {
      const guardado = esNuevo
        ? await cierresApi.crear(elegida, cuerpo)
        : await cierresApi.actualizar(cierre.tienda_id, cierre.id, cuerpo)

      // La hoja ya quedó guardada y nada de lo que le pase a la foto puede
      // echar eso para atrás, así que la foto va en su propio try: si falla,
      // el aviso viaja a la pantalla de Cierres —donde la hoja ya aparece en la
      // lista— y reintentar es abrirla y volver a adjuntar la foto, sin
      // escribir los cuarenta campos otra vez.
      let avisoFoto = ''
      try {
        if (archivo) {
          setFase('foto')
          await cierresApi.subirFoto(guardado.tienda_id, guardado.id, archivo)
        } else if (quitarFoto && cierre?.foto) {
          await cierresApi.quitarFoto(guardado.tienda_id, guardado.id)
        }
      } catch (err) {
        // El motivo útil viene en campos.foto cuando el servidor rechaza el
        // archivo (tipo o tamaño); err.message ahí solo dice "Datos inválidos".
        const motivo = err.campos?.foto ?? err.message
        const hoja = `El cierre del ${formatearFecha(fecha)} quedó guardado`
        avisoFoto = archivo
          ? `${hoja}, pero la foto no se pudo subir (${motivo}). Ábrelo y vuelve a adjuntarla.`
          : `${hoja}, pero la foto anterior sigue ahí (${motivo}). Ábrelo y vuelve a quitarla.`
      }

      await onGuardado(avisoFoto)
    } catch (err) {
      setError(err.message)
      setCampos(err.campos ?? {})
    } finally {
      setFase('')
    }
  }

  return (
    <Modal
      titulo={esNuevo ? 'Nuevo cierre de caja' : `${cierre.tienda_nombre} · ${cierre.fecha}`}
      onCerrar={onCerrar}
      alIntentarCerrar={() => !sucio || confirm('¿Cerrar sin guardar el cierre? Se pierde lo que llevas escrito.')}
      ancho
    >
      <form onSubmit={onSubmit} noValidate className="hoja-cierre">
        {error && <div className="alerta">{error}</div>}

        {/* Al crear se escoge la tienda; al editar ya está decidida y solo
            se muestra, porque la hoja es de ESE local. Se dibuja aunque haya
            un solo local: dice de qué tienda es la hoja, y sobre todo no deja
            un formulario sin tienda que la pantalla no pueda corregir. */}
        {esNuevo && (
          <>
            <label htmlFor="tienda">
              Tienda <span className="req">*</span>
            </label>
            <select id="tienda" value={elegida} onChange={(e) => setElegida(e.target.value)}>
              {tiendas.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.nombre}
                </option>
              ))}
            </select>
            {campos.tienda && <span className="error-campo">{campos.tienda}</span>}
          </>
        )}

        <div className="dos-columnas">
          <div>
            <label htmlFor="fecha">
              Día <span className="req">*</span>
            </label>
            <input
              id="fecha"
              type="date"
              value={fecha}
              max={hoyISO()}
              onChange={(e) => {
                setSucio(true)
                setFecha(e.target.value)
              }}
            />
            {campos.fecha && <span className="error-campo">{campos.fecha}</span>}
          </div>
          <div>
            <label htmlFor="responsable">Responsable</label>
            <input
              id="responsable"
              value={responsable}
              onChange={(e) => {
                setSucio(true)
                setResponsable(e.target.value)
              }}
              placeholder="Quién cerró la caja"
              maxLength={120}
            />
            {campos.responsable && <span className="error-campo">{campos.responsable}</span>}
          </div>
        </div>

        <Bloque titulo="QR · Nequi">
          <FilaMonto
            etiqueta="Banco"
            id="qr-banco"
            valor={montos.qr_banco}
            onCambio={(v) => cambiarMonto('qr_banco', v)}
            error={campos.qr_banco}
          />
          <FilaMonto
            etiqueta={tienda.nombre}
            id="qr-tienda"
            valor={montos.qr_tienda}
            onCambio={(v) => cambiarMonto('qr_tienda', v)}
            error={campos.qr_tienda}
          />
          <FilaCalculada etiqueta="Diferencia" valor={qrDiferencia} alerta />
        </Bloque>

        <Bloque titulo="Datáfono">
          <FilaMonto
            etiqueta="Datáfono"
            id="datafono"
            valor={montos.datafono_reporte}
            onCambio={(v) => cambiarMonto('datafono_reporte', v)}
            error={campos.datafono_reporte}
          />
          <FilaMonto
            etiqueta={tienda.nombre}
            id="datafono-tienda"
            valor={montos.datafono_tienda}
            onCambio={(v) => cambiarMonto('datafono_tienda', v)}
            error={campos.datafono_tienda}
          />
          <FilaCalculada etiqueta="Diferencia" valor={datafonoDiferencia} alerta />
        </Bloque>

        <Bloque titulo="Efectivo">
          <FilaMonto
            etiqueta="Billete"
            id="billete"
            valor={montos.efectivo_billete}
            onCambio={(v) => cambiarMonto('efectivo_billete', v)}
            error={campos.efectivo_billete}
          />
          <FilaMonto
            etiqueta="Moneda"
            id="moneda"
            valor={montos.efectivo_moneda}
            onCambio={(v) => cambiarMonto('efectivo_moneda', v)}
            error={campos.efectivo_moneda}
          />
          <FilaCalculada etiqueta="Total efectivo" valor={efectivoTotal} />
          <FilaMonto
            etiqueta={tienda.nombre}
            id="efectivo-tienda"
            valor={montos.efectivo_tienda}
            onCambio={(v) => cambiarMonto('efectivo_tienda', v)}
            error={campos.efectivo_tienda}
          />
          <FilaCalculada etiqueta="Diferencia" valor={efectivoDiferencia} alerta />
        </Bloque>

        <Bloque titulo="Total venta">
          <FilaCalculada etiqueta="Total métodos de pago" valor={metodosPago} />
          <FilaMonto
            etiqueta={`Total ${tienda.nombre}`}
            id="venta-tienda"
            valor={montos.venta_tienda}
            onCambio={(v) => cambiarMonto('venta_tienda', v)}
            error={campos.venta_tienda}
          />
          <FilaCalculada etiqueta="Diferencia" valor={ventaDiferencia} alerta />
        </Bloque>

        {LISTAS.map(([grupo, titulo]) => (
          <Bloque key={grupo} titulo={titulo}>
            {listas[grupo].map((fila, i) => (
              <div className="fila-lista-cierre" key={i}>
                <input
                  value={fila.descripcion}
                  onChange={(e) => cambiarLinea(grupo, i, 'descripcion', e.target.value)}
                  placeholder="Descripción"
                  aria-label={`${titulo}: descripción`}
                  maxLength={120}
                  className={aMedias(fila) || errorDeFila(grupo, i) ? 'campo-a-medias' : undefined}
                />
                <InputMonto
                  valor={fila.monto}
                  onCambio={(v) => cambiarLinea(grupo, i, 'monto', v)}
                />
                <button
                  type="button"
                  className="boton-tema boton-eliminar"
                  onClick={() => quitarFila(grupo, i)}
                  title="Quitar este renglón"
                  aria-label="Quitar este renglón"
                >
                  <Icono nombre="eliminar" tamano={16} />
                </button>
                {aMedias(fila) && (
                  <span className="error-campo">Ponle un nombre o quita el monto</span>
                )}
                {errorDeFila(grupo, i) && (
                  <span className="error-campo">{errorDeFila(grupo, i)}</span>
                )}
              </div>
            ))}

            <div className="pie-lista-cierre">
              <button type="button" className="secundario" onClick={() => agregarFila(grupo)}>
                Agregar
              </button>
              <span className="fig">Total {formatearMonto(totalDe(grupo))}</span>
            </div>
          </Bloque>
        ))}

        <Bloque titulo="Cuánto quedó">
          <FilaCalculada etiqueta={`Ventas (total ${tienda.nombre})`} valor={num(montos.venta_tienda)} />
          <FilaCalculada etiqueta="− Compras, gastos, descuentos y vales" valor={-salidas} />
          <FilaCalculada etiqueta="Debería quedar" valor={deberiaQuedar} />
          <FilaCalculada etiqueta="Hay en caja y bancos" valor={metodosPago} />
          <FilaCalculada etiqueta="Diferencia" valor={quedaDiferencia} alerta />
        </Bloque>

        <label htmlFor="foto">
          Foto de la hoja <span className="tenue">(opcional)</span>
        </label>
        <input
          id="foto"
          type="file"
          accept="image/jpeg,image/png,image/webp,image/heic,application/pdf"
          onChange={(e) => {
            setSucio(true)
            setArchivo(e.target.files?.[0] ?? null)
            setQuitarFoto(false)
          }}
        />
        {campos.foto && <span className="error-campo">{campos.foto}</span>}

        {cierre?.foto && !archivo && (
          <>
            <p className="tenue ayuda-campo">
              Ya tiene una:{' '}
              <button type="button" className="boton-enlace" onClick={verFoto}>
                {cierre.foto.nombre}
              </button>
            </p>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={quitarFoto}
                onChange={(e) => setQuitarFoto(e.target.checked)}
              />
              Quitar la foto actual
            </label>
          </>
        )}

        <label htmlFor="novedades">Novedades</label>
        <textarea
          id="novedades"
          rows={3}
          value={novedades}
          onChange={(e) => {
            setSucio(true)
            setNovedades(e.target.value)
          }}
          maxLength={2000}
          placeholder="Lo que haya que dejar anotado del día: el saldo en Nequi, un faltante, un turno raro..."
        />
        {campos.novedades && <span className="error-campo">{campos.novedades}</span>}

        <div className="acciones-modal">
          <button type="button" className="secundario" onClick={onCerrar}>
            Cancelar
          </button>
          <button type="submit" disabled={guardando}>
            {fase === 'foto' ? 'Subiendo la foto...' : guardando ? 'Guardando...' : 'Guardar el cierre'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function Bloque({ titulo, children }) {
  return (
    <fieldset className="bloque-cierre">
      <legend>{titulo}</legend>
      {children}
    </fieldset>
  )
}

function FilaMonto({ etiqueta, id, valor, onCambio, error }) {
  return (
    <div className="fila-cierre">
      <label htmlFor={id}>{etiqueta}</label>
      <InputMonto id={id} valor={valor} onCambio={onCambio} />
      {error && <span className="error-campo">{error}</span>}
    </div>
  )
}

// Una casilla que no se escribe: sale de las de arriba. Con `alerta`, se pinta
// en rojo cuando no da cero — que es justo lo que hay que revisar antes de
// firmar la hoja.
function FilaCalculada({ etiqueta, valor, alerta = false }) {
  const descuadra = alerta && Math.round(valor) !== 0
  return (
    <div className="fila-cierre calculada">
      <span>{etiqueta}</span>
      <span className={`fig ${descuadra ? 'negativo' : 'tenue'}`}>{formatearMonto(valor)}</span>
    </div>
  )
}
