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

// Cuánto falta para una fecha de cobro, dicho como lo diría una persona.
// Devuelve { texto, tono } con tono 'vencido', 'hoy' o 'pronto' (o null si
// falta más de una semana, que no merece resaltarse).
export function faltaParaCobrar(iso, hoy = hoyISO()) {
  const [a1, m1, d1] = hoy.split('-').map(Number)
  const [a2, m2, d2] = iso.split('-').map(Number)
  // Date.UTC para contar días exactos, sin horas de verano de por medio.
  const dias = Math.round((Date.UTC(a2, m2 - 1, d2) - Date.UTC(a1, m1 - 1, d1)) / 86400000)

  if (dias < 0) {
    const n = -dias
    return { texto: `venció hace ${n} día${n === 1 ? '' : 's'}`, tono: 'vencido' }
  }
  if (dias === 0) return { texto: 'te paga hoy', tono: 'hoy' }
  if (dias === 1) return { texto: 'te paga mañana', tono: 'pronto' }
  if (dias <= 7) return { texto: `te paga en ${dias} días`, tono: 'pronto' }
  return { texto: `te paga el ${formatearFecha(iso)}`, tono: null }
}

export const ETIQUETAS_TIPO = {
  recibi: 'Recibí',
  pague: 'Pagué',
  preste: 'Presté',
}

export const ETIQUETAS_ESTADO = {
  pendiente: 'Pendiente',
  pagado: 'Pagado',
}

// Decide como se ve el monto de un movimiento en la lista.
//
// La regla que no es obvia: un préstamo PAGADO se muestra en "+" y en verde,
// porque esa plata ya volvió a tu bolsillo. Uno pendiente se muestra en "−"
// y en ámbar: salió y todavía está afuera.
export function estiloMonto(movimiento) {
  const prestamoDevuelto = movimiento.tipo === 'preste' && movimiento.estado === 'pagado'

  if (movimiento.tipo === 'recibi' || prestamoDevuelto) {
    return { signo: '+', clase: 'positivo' }
  }
  if (movimiento.tipo === 'preste') {
    return { signo: '−', clase: 'advertencia' }
  }
  return { signo: '−', clase: 'negativo' }
}

export function esPrestamo(movimiento) {
  return movimiento.tipo === 'preste'
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
