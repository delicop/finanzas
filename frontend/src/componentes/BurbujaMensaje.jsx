import { formatearHora } from '../lib/formato'
import Icono from './Icono'

// Cómo se le nombran al usuario las herramientas que consultó el agente. El
// backend manda el nombre técnico; aquí se traduce, y lo que no esté en la
// lista se muestra tal cual en vez de desaparecer.
// Las de "proponer_" no se listan: no son una consulta, y lo que prepararon ya
// está a la vista en su tarjeta.
const NOMBRES_HERRAMIENTAS = {
  resumen: 'tu resumen',
  listar_movimientos: 'tus movimientos',
  listar_categorias: 'tus categorías',
  listar_medios_pago: 'tus medios de pago',
}

// Un mensaje del chat, a la derecha si es tuyo y a la izquierda si es del
// asistente. Lo usan el chat abierto y las conversaciones guardadas.
//
//   onLeer   si llega, la respuesta muestra debajo el botón de escucharla.
//            Sin él (o en un navegador sin voz) la burbuja es la de siempre.
//   leyendo  si es ESTA la que está sonando, para pintar el botón de parar.
export default function BurbujaMensaje({ mensaje, onLeer, leyendo = false }) {
  const esMio = mensaje.rol === 'usuario'
  const consultas = (mensaje.herramientas ?? []).filter((h) => !h.startsWith('proponer_'))

  return (
    <div className={`wa-burbuja ${esMio ? 'wa-mio' : 'wa-agente'}`}>
      {/* El texto NO se interpreta como HTML: React escapa el contenido y eso
          es exactamente lo que se quiere con algo que escribe un modelo. Lo
          único que se reconoce son las **negritas**, que el modelo usa mucho.
          Los saltos de línea los respeta el CSS con white-space: pre-wrap. */}
      <p>{esMio ? mensaje.contenido : conNegritas(mensaje.contenido)}</p>

      {/* De dónde salieron las cifras. No es un adorno: quien lee un número en
          una app de plata tiene derecho a saber qué miró el agente. */}
      {consultas.length > 0 && (
        <p className="wa-fuente">
          Consultó {consultas.map((h) => NOMBRES_HERRAMIENTAS[h] ?? h).join(' y ')}
        </p>
      )}

      {/* Escuchar la respuesta, debajo de ella y no en la cabecera: se pide
          una por una, que es como se oye de verdad — la cifra que se quería,
          no la conversación entera. */}
      {!esMio && onLeer && (
        <button
          type="button"
          className={`wa-leer ${leyendo ? 'leyendo' : ''}`}
          onClick={onLeer}
          aria-label={leyendo ? 'Dejar de leer esta respuesta' : 'Escuchar esta respuesta'}
        >
          <Icono nombre={leyendo ? 'parar' : 'altavoz'} tamano={15} />
          {leyendo ? 'Parar' : 'Escuchar'}
        </button>
      )}

      {mensaje.creado_en && (
        <time className="wa-hora">
          {formatearHora(mensaje.creado_en)}
          {esMio && <span className="wa-check"> ✓✓</span>}
        </time>
      )}
    </div>
  )
}

// "Tienes **Comida**" -> ["Tienes ", <strong>Comida</strong>]. Con split y un
// grupo de captura, las partes impares son las que iban entre asteriscos.
// Cada parte sigue siendo texto: React la escapa igual que antes.
function conNegritas(texto) {
  const partes = texto.split(/\*\*(.+?)\*\*/g)
  if (partes.length === 1) return texto
  return partes.map((parte, i) => (i % 2 === 1 ? <strong key={i}>{parte}</strong> : parte))
}
