// El distintivo de una categoría o de un medio de pago: un dibujo si el nombre
// se reconoce, y si no, su inicial. Siempre con un color que sale del nombre.
//
// No se guarda nada: se calcula cada vez a partir del nombre. Así funciona con
// lo que sea que la persona haya escrito, sin migración ni pantalla nueva, y
// "Mercado" se ve igual en el resumen, en la lista y en cada movimiento.

// Palabras clave → dibujo. Se revisan en orden y gana la primera que aparezca,
// así que lo más específico va arriba ("tarjeta" antes que "banco": una
// "Tarjeta Bancolombia" es una tarjeta).
//
// Una palabra corta (hasta 4 letras) tiene que coincidir entera: "ara" no
// puede encender el dibujo de mercado en "Para la casa". Las largas también
// valen como comienzo de palabra, para que "restaurantes" o "mercados" caigan.
const CATEGORIAS = [
  ['🛒', ['mercado', 'super', 'supermercado', 'd1', 'exito', 'ara', 'olimpica', 'carulla', 'jumbo', 'tienda', 'viveres', 'abarrotes', 'fruver']],
  ['🍔', ['comida', 'restaurante', 'almuerzo', 'domicilio', 'rappi', 'cafe', 'desayuno', 'cena', 'antojo', 'mecato', 'onces']],
  ['🏠', ['arriendo', 'alquiler', 'casa', 'hogar', 'vivienda', 'hipoteca', 'administracion', 'apartamento']],
  ['💡', ['servicios', 'luz', 'agua', 'gas', 'energia', 'internet', 'epm', 'acueducto']],
  ['📱', ['celular', 'telefono', 'claro', 'movistar', 'tigo']],
  ['🚗', ['transporte', 'bus', 'taxi', 'uber', 'didi', 'gasolina', 'moto', 'carro', 'parqueadero', 'peaje', 'metro', 'transmilenio', 'pasaje']],
  ['💊', ['salud', 'medico', 'drogueria', 'farmacia', 'eps', 'medicina', 'medicamentos', 'odontologo', 'dentista', 'cita']],
  ['🎓', ['educacion', 'colegio', 'universidad', 'curso', 'estudio', 'matricula', 'libros', 'pension']],
  ['🎬', ['ocio', 'entretenimiento', 'cine', 'fiesta', 'salida', 'rumba', 'netflix', 'spotify', 'suscripcion', 'juegos']],
  ['👕', ['ropa', 'zapatos', 'vestuario', 'calzado']],
  ['💰', ['sueldo', 'salario', 'nomina', 'quincena', 'ingreso', 'ingresos', 'prima']],
  ['💼', ['trabajo', 'negocio', 'ventas', 'venta', 'clientes', 'freelance', 'oficina', 'empresa']],
  ['🐶', ['mascota', 'mascotas', 'perro', 'gato', 'veterinario']],
  ['🧸', ['hijos', 'hijo', 'hija', 'bebe', 'ninos', 'nino', 'panales']],
  ['🎁', ['regalo', 'regalos', 'cumpleanos', 'navidad', 'detalle']],
  ['✈️', ['viaje', 'viajes', 'vacaciones', 'hotel', 'vuelo', 'paseo']],
  ['🐷', ['ahorro', 'ahorros', 'inversion', 'cdt', 'fondo']],
  ['💳', ['tarjeta', 'credito', 'deuda', 'deudas', 'prestamo', 'cuota', 'cuotas']],
  ['🏋️', ['gym', 'gimnasio', 'deporte', 'futbol']],
  ['💇', ['belleza', 'peluqueria', 'barberia', 'manicure', 'cuidado personal']],
  ['🔧', ['reparacion', 'mantenimiento', 'arreglo', 'arreglos', 'herramientas']],
  ['🙏', ['diezmo', 'iglesia', 'donacion', 'ofrenda']],
  ['🧾', ['impuestos', 'impuesto', 'predial', 'dian', 'multa']],
  ['👪', ['familia', 'mama', 'papa']],
]

const MEDIOS = [
  ['💳', ['tarjeta', 'credito', 'visa', 'mastercard', 'debito', 'amex']],
  ['📱', ['nequi', 'daviplata', 'movii', 'dale', 'rappipay', 'billetera digital', 'lulo', 'nu']],
  ['💵', ['efectivo', 'cash', 'billetera', 'plata', 'contado']],
  ['🏦', ['banco', 'bancolombia', 'davivienda', 'bogota', 'bbva', 'cuenta', 'colpatria', 'villas', 'occidente', 'popular', 'caja social', 'itau', 'falabella', 'pichincha']],
  ['🐷', ['alcancia', 'ahorro', 'ahorros', 'bolsillo', 'colchon']],
]

// Ocho tonos fríos, ninguno verde ni rojo: esos dos son del dinero, y un
// círculo verde junto a un gasto se leería como "entró plata". Los valores
// viven en estilos.css (--dist-1…8) para que el modo oscuro los ajuste.
const TONOS = 8

function normalizar(texto) {
  return (texto || '')
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
}

function buscarDibujo(nombre, tabla) {
  const limpio = normalizar(nombre)
  const palabras = limpio.split(/[^a-z0-9ñ]+/).filter(Boolean)
  for (const [dibujo, claves] of tabla) {
    for (const clave of claves) {
      const k = normalizar(clave)
      if (k.includes(' ')) {
        if (limpio.includes(k)) return dibujo
      } else if (palabras.some((p) => p === k || (k.length >= 5 && p.startsWith(k)))) {
        return dibujo
      }
    }
  }
  return null
}

// El mismo nombre da siempre el mismo tono, sin importar mayúsculas ni
// tildes: "Café" y "cafe" son la misma categoría a los ojos de la persona.
function tonoDe(nombre) {
  let h = 0
  for (const c of normalizar(nombre).trim()) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return (h % TONOS) + 1
}

function inicialDe(nombre) {
  const primera = (nombre || '').trim().match(/[\p{L}\p{N}]/u)
  return primera ? primera[0].toUpperCase() : '·'
}

export function distintivo(nombre, clase = 'categoria') {
  return {
    // Una persona nunca lleva dibujo: "Andrés" no es una categoría, y a una
    // persona se le reconoce por la inicial, como en la agenda del celular.
    dibujo: clase === 'persona' ? null : buscarDibujo(nombre, clase === 'medio' ? MEDIOS : CATEGORIAS),
    inicial: inicialDe(nombre),
    tono: tonoDe(nombre),
  }
}
