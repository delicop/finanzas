// El backend manda los montos como STRING (vienen de una columna NUMERIC).
// Aqui los convertimos a numero SOLO para mostrarlos en pantalla.
// Nunca se hacen cuentas con ellos en el frontend: los totales los calcula
// Postgres y llegan ya sumados.

const formateador = new Intl.NumberFormat('es-CO', {
  style: 'currency',
  currency: 'COP',
  minimumFractionDigits: 0,
  maximumFractionDigits: 2,
})

export function formatearMonto(monto) {
  const n = Number(monto)
  if (Number.isNaN(n)) return String(monto ?? '')
  return formateador.format(n)
}

// Las fechas llegan como "2026-09-15". new Date("2026-09-15") las interpreta
// en UTC y en Colombia (UTC-5) puede mostrarse el dia anterior. Por eso la
// partimos a mano en vez de dejarselo a Date.
export function formatearFecha(iso) {
  if (!iso) return ''
  const [anio, mes, dia] = iso.split('-')
  return `${dia}/${mes}/${anio}`
}

// Las marcas de tiempo del chat si vienen completas (timestamptz), asi que
// aqui Date es seguro: la cadena trae su zona horaria.
const horaCorta = new Intl.DateTimeFormat('es-CO', { hour: '2-digit', minute: '2-digit' })

export function formatearHora(iso) {
  if (!iso) return ''
  const fecha = new Date(iso)
  return Number.isNaN(fecha.getTime()) ? '' : horaCorta.format(fecha)
}

const fechaCorta = new Intl.DateTimeFormat('es-CO', { day: '2-digit', month: 'short' })

// "16 sept · 14:03". Para los avisos, donde importa cuándo llegó pero no el año.
export function formatearFechaHora(iso) {
  if (!iso) return ''
  const fecha = new Date(iso)
  if (Number.isNaN(fecha.getTime())) return ''
  return `${fechaCorta.format(fecha)} · ${horaCorta.format(fecha)}`
}

export function hoyISO() {
  const ahora = new Date()
  const mes = String(ahora.getMonth() + 1).padStart(2, '0')
  const dia = String(ahora.getDate()).padStart(2, '0')
  return `${ahora.getFullYear()}-${mes}-${dia}`
}

// sumarDias("2026-09-28", 5) -> "2026-10-03". Se arma con la fecha LOCAL
// (new Date(año, mes, día)), así un cambio de mes o de año sale bien y no
// hay corrimiento por zona horaria.
export function sumarDias(iso, dias) {
  const [anio, mes, dia] = iso.split('-').map(Number)
  const f = new Date(anio, mes - 1, dia + dias)
  const m = String(f.getMonth() + 1).padStart(2, '0')
  const d = String(f.getDate()).padStart(2, '0')
  return `${f.getFullYear()}-${m}-${d}`
}

// finDeMes("2026-09-10") -> "2026-09-30".
export function finDeMes(iso) {
  const [anio, mes] = iso.split('-').map(Number)
  const ultimo = new Date(anio, mes, 0).getDate() // el día 0 del mes siguiente
  return `${anio}-${String(mes).padStart(2, '0')}-${String(ultimo).padStart(2, '0')}`
}

// Días entre hoy y una fecha. Negativo si ya pasó.
//
// Date.UTC para contar días exactos: con fechas locales, un cambio de horario
// puede hacer que dos días den 0,96 y se redondee mal.
export function diasHasta(iso, hoy = hoyISO()) {
  const [a1, m1, d1] = hoy.split('-').map(Number)
  const [a2, m2, d2] = iso.split('-').map(Number)
  return Math.round((Date.UTC(a2, m2 - 1, d2) - Date.UTC(a1, m1 - 1, d1)) / 86400000)
}

// Cuánto falta para una fecha acordada, dicho como lo diría una persona.
//
// `propio` invierte la frase: cuando la deuda es tuya no es "te paga el
// viernes" sino "le pagas el viernes". Es la misma fecha y el mismo color,
// pero la acción es la contraria y decirla al revés confunde de verdad.
//
// Devuelve { texto, tono } con tono 'vencido', 'hoy' o 'pronto' (o null si
// falta más de una semana, que no merece resaltarse).
export function faltaParaCobrar(iso, hoy = hoyISO(), propio = false) {
  const dias = diasHasta(iso, hoy)
  const verbo = propio ? 'le pagas' : 'te paga'

  if (dias < 0) {
    const n = -dias
    return { texto: `venció hace ${n} día${n === 1 ? '' : 's'}`, tono: 'vencido' }
  }
  if (dias === 0) return { texto: `${verbo} hoy`, tono: 'hoy' }
  if (dias === 1) return { texto: `${verbo} mañana`, tono: 'pronto' }
  if (dias <= 7) return { texto: `${verbo} en ${dias} días`, tono: 'pronto' }
  return { texto: `${verbo} el ${formatearFecha(iso)}`, tono: null }
}

export const ETIQUETAS_TIPO = {
  recibi: 'Recibí',
  pague: 'Pagué',
  preste: 'Presté',
  me_prestaron: 'Me prestaron',
  traslado: 'Traslado',
}

export const ETIQUETAS_ESTADO = {
  pendiente: 'Pendiente',
  parcial: 'Abonado a medias',
  pagado: 'Saldado',
}

// Los dos tipos que son una deuda: llevan a quién, estado, abonos y cuotas.
// Lo único que cambia entre ellos es de qué lado está la plata.
export function esDeuda(movimiento) {
  return movimiento.tipo === 'preste' || movimiento.tipo === 'me_prestaron'
}

// Si la deuda es TUYA (te prestaron a ti). Cambia casi todos los textos.
export function esDeudaPropia(movimiento) {
  return movimiento.tipo === 'me_prestaron'
}

// Decide cómo se ve el monto de un movimiento en la lista.
//
// Las reglas que no son obvias:
//  - Un préstamo YA SALDADO se muestra en "+" y en verde: esa plata volvió.
//    Uno con saldo, en "−" y en ámbar: salió y todavía está afuera.
//  - "Me prestaron" es al revés: entró plata (por eso "+"), pero la debes,
//    y por eso va en ámbar y no en verde. Cuando ya la pagaste, sale en "−"
//    rojo: al final del camino, esa plata se fue.
//  - Un traslado no suma ni resta: la misma plata cambió de bolsillo.
export function estiloMonto(movimiento) {
  const saldada = movimiento.estado === 'pagado'

  switch (movimiento.tipo) {
    case 'recibi':
      return { signo: '+', clase: 'positivo' }
    case 'preste':
      return saldada ? { signo: '+', clase: 'positivo' } : { signo: '−', clase: 'advertencia' }
    case 'me_prestaron':
      return saldada ? { signo: '−', clase: 'negativo' } : { signo: '+', clase: 'advertencia' }
    case 'traslado':
      return { signo: '↔', clase: 'neutro' }
    default:
      return { signo: '−', clase: 'negativo' }
  }
}

// El mensaje de WhatsApp para recordar un cobro, ya listo para enviar.
//
// Se arma aquí y no en el servidor porque no se manda nada: se abre WhatsApp
// con el texto escrito y la persona decide si lo envía, a quién y cuándo. La
// app nunca le escribe a nadie por su cuenta.
export function mensajeDeCobro(movimiento) {
  const quien = (movimiento.a_quien ?? '').trim()
  const saldo = formatearMonto(movimiento.saldo ?? movimiento.monto)
  const concepto = (movimiento.descripcion ?? '').trim()

  let texto = `Hola${quien ? ' ' + quien : ''}, `
  texto += `te escribo para recordarte los ${saldo}`
  if (concepto) texto += ` de ${concepto}`
  texto += ` que quedaron pendientes desde el ${formatearFecha(movimiento.fecha)}`
  if (movimiento.cobrar_el) {
    texto += `. Habíamos quedado para el ${formatearFecha(movimiento.cobrar_el)}`
  }
  texto += '. ¿Me confirmas cuándo puedes? Gracias.'
  return texto
}

// El enlace que abre WhatsApp con el mensaje escrito. Sin número: lo elige la
// persona en su lista de contactos, que es más rápido y evita que la app
// guarde teléfonos que nadie le pidió guardar.
export function enlaceWhatsApp(texto) {
  return `https://wa.me/?text=${encodeURIComponent(texto)}`
}

/* ---------------------------------------------------------------------------
 * Escribir montos sin contar ceros
 *
 * En Colombia el punto separa los miles y la coma los decimales:
 * 1.500.000,50. Pero la API siempre recibe y devuelve el número "crudo"
 * (1500000.50, punto decimal), que es el formato que entiende Postgres.
 *
 * Por eso hay dos conversiones: una para MOSTRAR y otra para ENVIAR.
 * ------------------------------------------------------------------------ */

// Formatea lo que el usuario va escribiendo: "1500000" -> "1.500.000"
export function formatearEntradaMonto(texto) {
  // Nos quedamos solo con dígitos y UNA coma decimal.
  const limpio = String(texto ?? '').replace(/[^\d,]/g, '')
  const [crudoEntero = '', ...resto] = limpio.split(',')
  const tieneComa = resto.length > 0

  // Quitamos ceros a la izquierda ("007" -> "7") pero dejamos el "0" solo.
  let entero = crudoEntero.replace(/^0+(?=\d)/, '')
  // Tope de 12 enteros: es lo que acepta la columna NUMERIC(14,2).
  entero = entero.slice(0, 12)

  // Agrupa de a tres desde la derecha. Se hace con regex y no con Number
  // para que un monto muy grande no pierda precisión al convertirlo.
  const agrupado = entero.replace(/\B(?=(\d{3})+(?!\d))/g, '.')

  if (!tieneComa) return agrupado

  const decimales = resto.join('').slice(0, 2)
  return `${agrupado || '0'},${decimales}`
}

// Convierte lo que se ve en pantalla a lo que espera la API:
// "1.500.000,50" -> "1500000.50"
export function entradaAMonto(texto) {
  return (
    String(texto ?? '')
      .replace(/\./g, '') // los puntos son separadores de miles, se quitan
      .replace(',', '.') // la coma decimal pasa a punto
      .replace(/[^\d.]/g, '')
      // Si quedó la coma colgando ("250.000," -> "250000."), la soltamos:
      // el usuario escribió un monto completo, no uno a medias, y el backend
      // rechazaría ese punto sin decimales.
      .replace(/\.$/, '')
  )
}

// Convierte el monto que devuelve la API al texto del formulario:
// "300000.00" -> "300.000"   |   "1500.50" -> "1.500,50"
export function montoAEntrada(valorApi) {
  if (valorApi === null || valorApi === undefined || valorApi === '') return ''
  const [entero, decimales] = String(valorApi).split('.')
  // Si los centavos son cero no los mostramos: en pesos casi nunca se usan
  // y "300.000" se lee mucho mejor que "300.000,00".
  const conDecimales = decimales && Number(decimales) > 0
  return formatearEntradaMonto(conDecimales ? `${entero},${decimales}` : entero)
}
