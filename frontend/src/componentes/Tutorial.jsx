import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Icono from './Icono'
import Modal from './Modal'
import { useEsMovil } from '../lib/useEsMovil'

// La guía para quien entra por primera vez.
//
// Se abre sola la primera vez que alguien entra (ver Layout) y después vuelve
// con el botón de ayuda de la barra. Son cinco pantallas cortas en el MISMO
// orden en que hay que armar la app: sin una categoría y un medio no se puede
// registrar nada, y quien no lo sabe se queda mirando un formulario que no
// deja guardar.
//
// Cada paso trae el botón que lleva a la sección de la que habla: leer y
// después tener que buscar dónde se hace es justo como la gente se pierde.

const CLAVE = 'finanzas:tutorial-visto:'

// Guardado por usuario y en este navegador. Si el almacenamiento falla (modo
// privado, datos borrados) lo peor que pasa es que la guía vuelva a salir.
export function tutorialVisto(usuarioID) {
  try {
    return localStorage.getItem(CLAVE + usuarioID) === '1'
  } catch {
    return true
  }
}

export function marcarTutorialVisto(usuarioID) {
  try {
    localStorage.setItem(CLAVE + usuarioID, '1')
  } catch {
    // Sin almacenamiento no hay cómo recordarlo; no es grave.
  }
}

// Las cinco formas en que se mueve la plata, con un ejemplo de cada una.
// Los nombres son los mismos del formulario (MovimientoForm).
const TIPOS = [
  {
    icono: 'entra',
    clase: 'positivo',
    nombre: 'Recibí',
    que: 'Te entró plata.',
    ejemplo: 'Te pagaron el sueldo → Sueldo · Nequi',
  },
  {
    icono: 'sale',
    clase: 'negativo',
    nombre: 'Pagué',
    que: 'Salió plata.',
    ejemplo: 'Pagaste el arriendo → Casa · Efectivo',
  },
  {
    icono: 'sale',
    clase: 'advertencia',
    nombre: 'Presté',
    que: 'Le prestaste a alguien. Queda en "Te deben" hasta que te pague.',
    ejemplo: 'Le prestaste a Carlos → Préstamos · Nequi',
  },
  {
    icono: 'entra',
    clase: 'advertencia',
    nombre: 'Me prestaron',
    que: 'Alguien te prestó. Queda en "Debes" hasta que lo devuelvas.',
    ejemplo: 'Tu mamá te prestó → Préstamos · Efectivo',
  },
  {
    icono: 'traslado',
    clase: 'neutro',
    nombre: 'Traslado',
    que: 'Mueves plata de un lado a otro tuyo. No suma ni resta.',
    ejemplo: 'Sacaste del cajero → Bancolombia a Efectivo',
  },
]

export default function Tutorial({ conIA, onCerrar }) {
  const [paso, setPaso] = useState(0)
  const navigate = useNavigate()
  const esMovil = useEsMovil()

  function ir(ruta) {
    navigate(ruta)
    onCerrar()
  }

  const pasos = [
    {
      titulo: 'Así funciona',
      cuerpo: (
        <>
          <p className="tutorial-texto">
            Cada peso que anotas responde tres preguntas: <strong>¿de qué es?</strong>,{' '}
            <strong>¿por dónde entró o salió?</strong> y <strong>¿cuánto?</strong> Primero armas
            tus dos listas y después todo es anotar.
          </p>
          <Diagrama />
        </>
      ),
    },
    {
      titulo: 'Paso 1 · Tus categorías',
      cuerpo: (
        <>
          <p className="tutorial-texto">
            Responden <strong>¿de qué es esta plata?</strong> Son los grupos en que se reparte: lo
            de la casa, el mercado, el transporte. Sirven igual para lo que gastas y para lo que te
            entra.
          </p>
          <Ejemplos
            icono="categorias"
            nombres={['Casa', 'Mercado', 'Transporte', 'Servicios', 'Sueldo', 'Ventas']}
          />
          <p className="tutorial-consejo">
            Empieza con cuatro o cinco. Cuando algo no encaje en ninguna, creas otra.
          </p>
        </>
      ),
      accion: { texto: 'Crear mis categorías', ruta: '/categorias' },
    },
    {
      titulo: 'Paso 2 · Tus medios de pago',
      cuerpo: (
        <>
          <p className="tutorial-texto">
            Responden <strong>¿dónde está la plata?</strong> Es cada lugar donde guardas o recibes
            dinero. La app lleva el saldo de cada uno, así sabes cuánto hay en el bolsillo y cuánto
            en el banco.
          </p>
          <Ejemplos icono="medios" nombres={['Efectivo', 'Nequi', 'Bancolombia', 'Daviplata']} />
          <p className="tutorial-consejo">
            Tu cuenta ya trae Efectivo, Transferencia y Otro. Agrega uno por cada cuenta o
            billetera que de verdad uses, y borra o renombra los que no.
          </p>
        </>
      ),
      accion: { texto: 'Crear mis medios', ruta: '/medios-pago' },
    },
    {
      titulo: 'Paso 3 · Anota tus movimientos',
      cuerpo: (
        <>
          <p className="tutorial-texto">
            Un movimiento es cada vez que la plata se mueve. Eliges el tipo, la categoría, el medio y
            el monto:
          </p>
          <ul className="tutorial-tipos">
            {TIPOS.map((t) => (
              <li key={t.nombre}>
                <span className={`tutorial-tipo-icono ${t.clase}`}>
                  <Icono nombre={t.icono} tamano={18} />
                </span>
                <div>
                  <strong>{t.nombre}</strong> <span>{t.que}</span>
                  <span className="tutorial-ejemplo">{t.ejemplo}</span>
                </div>
              </li>
            ))}
          </ul>
        </>
      ),
      accion: { texto: 'Ir a Movimientos', ruta: '/movimientos' },
    },
    {
      titulo: 'Paso 4 · Mira cómo vas',
      cuerpo: (
        <>
          <ul className="tutorial-lista">
            <li>
              <Icono nombre="resumen" tamano={20} />
              <div>
                <strong>Resumen.</strong> "Tienes" es la suma de todos tus medios. Abajo ves cuánto
                hay en cada uno, quién te debe y en qué se fue la plata por categoría.
              </div>
            </li>
            <li>
              <Icono nombre="recurrentes" tamano={20} />
              <div>
                <strong>Se repiten.</strong> Lo de todos los meses —arriendo, internet, sueldo— lo
                creas una vez. La app te lo propone cuando toca y tú solo confirmas.
              </div>
            </li>
            {conIA && (
              <li>
                <Icono nombre="movimientos" tamano={20} />
                <div>
                  <strong>El asistente.</strong> La burbuja de abajo. Escríbele como le hablarías a
                  alguien: <em>"pagué 50 mil de mercado con Nequi"</em>, y él lo anota.
                </div>
              </li>
            )}
            <li>
              <Icono nombre="ayuda" tamano={20} />
              <div>
                <strong>¿Te perdiste?</strong> Esta guía vuelve con el botón{' '}
                <Icono nombre="ayuda" tamano={14} />{' '}
                {esMovil ? 'que está junto al título del Resumen.' : 'de la barra de arriba.'}
              </div>
            </li>
          </ul>
        </>
      ),
    },
  ]

  const actual = pasos[paso]
  const esUltimo = paso === pasos.length - 1

  return (
    <Modal titulo={actual.titulo} onCerrar={onCerrar}>
      <div className="tutorial-puntos" aria-label={`Paso ${paso + 1} de ${pasos.length}`}>
        {pasos.map((p, i) => (
          <button
            key={p.titulo}
            className={`tutorial-punto ${i === paso ? 'activo' : ''} ${i < paso ? 'hecho' : ''}`}
            onClick={() => setPaso(i)}
            aria-label={`Ir a: ${p.titulo}`}
            aria-current={i === paso ? 'step' : undefined}
          />
        ))}
      </div>

      <div className="tutorial-cuerpo">{actual.cuerpo}</div>

      {actual.accion && (
        <button className="secundario tutorial-ir" onClick={() => ir(actual.accion.ruta)}>
          {actual.accion.texto} →
        </button>
      )}

      <div className="acciones-modal tutorial-acciones">
        {paso === 0 ? (
          <button className="enlace" onClick={onCerrar}>
            Ya sé cómo funciona
          </button>
        ) : (
          <button className="secundario" onClick={() => setPaso(paso - 1)}>
            Atrás
          </button>
        )}
        {esUltimo ? (
          <button onClick={onCerrar}>Empezar</button>
        ) : (
          <button onClick={() => setPaso(paso + 1)}>Siguiente</button>
        )}
      </div>
    </Modal>
  )
}

// El dibujo de la primera pantalla: las dos listas alimentan cada movimiento,
// y los movimientos alimentan el resumen. Va en HTML y no en SVG para que en
// el teléfono las cajas se acomoden solas y el texto no se encoja.
function Diagrama() {
  return (
    <div className="tutorial-diagrama" role="img" aria-label="Categoría y medio de pago forman un movimiento; los movimientos arman el resumen">
      <div className="diagrama-fila">
        <Caja icono="categorias" titulo="Categoría" pregunta="¿De qué es?" ejemplo="Casa" />
        <span className="diagrama-mas" aria-hidden="true">+</span>
        <Caja icono="medios" titulo="Medio de pago" pregunta="¿Dónde está?" ejemplo="Nequi" />
      </div>
      <span className="diagrama-flecha" aria-hidden="true">↓</span>
      <Caja
        icono="movimientos"
        titulo="Movimiento"
        pregunta="¿Qué pasó y cuánto?"
        ejemplo="Pagué $80.000"
        destacada
      />
      <span className="diagrama-flecha" aria-hidden="true">↓</span>
      <Caja icono="resumen" titulo="Resumen" pregunta="¿Cómo voy?" ejemplo="Tienes, te deben, debes" />
    </div>
  )
}

function Caja({ icono, titulo, pregunta, ejemplo, destacada = false }) {
  return (
    <div className={`diagrama-caja ${destacada ? 'destacada' : ''}`}>
      <span className="diagrama-icono">
        <Icono nombre={icono} tamano={18} />
      </span>
      <strong>{titulo}</strong>
      <span className="diagrama-pregunta">{pregunta}</span>
      <span className="diagrama-ejemplo">ej: {ejemplo}</span>
    </div>
  )
}

function Ejemplos({ icono, nombres }) {
  return (
    <div className="tutorial-ejemplos">
      <span className="tutorial-ejemplos-rotulo">Por ejemplo</span>
      <div className="tutorial-chips">
        {nombres.map((n) => (
          <span key={n} className="tutorial-chip">
            <Icono nombre={icono} tamano={14} />
            {n}
          </span>
        ))}
      </div>
    </div>
  )
}
