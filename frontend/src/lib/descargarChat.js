import { guardarArchivo } from './archivos'

// Descarga una conversación del asistente como texto plano, para guardarla en
// el teléfono o mandarla por correo. Se arma en el navegador: el servidor ya
// entregó los mensajes y no hace falta otra ruta para esto.

const fechaLarga = new Intl.DateTimeFormat('es-CO', { dateStyle: 'long', timeStyle: 'short' })
const soloHora = new Intl.DateTimeFormat('es-CO', { hour: '2-digit', minute: '2-digit' })

export function textoDeConversacion(mensajes, titulo = '') {
  const lineas = ['Conversación con el asistente']
  if (titulo) lineas.push(titulo)
  if (mensajes.length > 0 && mensajes[0].creado_en) {
    lineas.push(fechaLarga.format(new Date(mensajes[0].creado_en)))
  }
  lineas.push('')

  for (const m of mensajes) {
    const quien = m.rol === 'usuario' ? 'Tú' : 'Asistente'
    const hora = m.creado_en ? ` (${soloHora.format(new Date(m.creado_en))})` : ''
    lineas.push(`${quien}${hora}:`, m.contenido, '')
  }
  return lineas.join('\n')
}

const BOM = String.fromCharCode(0xfeff)

export function descargarConversacion(mensajes, titulo = '') {
  const primero = mensajes.find((m) => m.creado_en)?.creado_en
  const dia = (primero ? new Date(primero) : new Date()).toISOString().slice(0, 10)

  // Con BOM (marca de UTF-8) el Bloc de notas de Windows lee bien las tildes.
  const archivo = new Blob([BOM, textoDeConversacion(mensajes, titulo)], {
    type: 'text/plain;charset=utf-8',
  })
  guardarArchivo(archivo, `conversacion-asistente-${dia}.txt`)
}
