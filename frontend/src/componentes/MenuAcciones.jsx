import { useEffect, useRef, useState } from 'react'

// Un botón "⋯" que despliega un menú de acciones.
//
// Existe porque hay sitios donde las acciones son cuatro y no caben: en la
// tabla de clientes se apilaban en cuatro renglones y cada fila terminaba
// ocupando media pantalla; en el teléfono, lo mismo dentro de cada ficha.
// Escondidas detrás de un botón, la fila vuelve a medir una línea y la lista
// se puede recorrer de un vistazo, que es para lo que existe una lista.
//
// Se cierra solo: al tocar cualquier cosa del menú, al tocar por fuera y con
// Escape. Por eso quien lo usa NO tiene que llamar a nada para cerrarlo — es
// un olvido fácil y dejaría el menú abierto encima de la fila siguiente.
//
// OJO CON EL DESBORDE: el desplegable va posicionado en absoluto, así que
// cualquier contenedor con `overflow` por encima lo recorta. En una tabla eso
// significa no envolverla en `.tabla-scroll`.
export default function MenuAcciones({ etiqueta = 'Más opciones', children }) {
  const [abierto, setAbierto] = useState(false)
  const caja = useRef(null)

  useEffect(() => {
    if (!abierto) return

    const alClic = (e) => {
      if (!caja.current?.contains(e.target)) setAbierto(false)
    }
    // pointerdown y no click: cierra apenas se toca fuera, sin esperar a que
    // se suelte el dedo.
    document.addEventListener('pointerdown', alClic)
    return () => document.removeEventListener('pointerdown', alClic)
  }, [abierto])

  function alTeclear(e) {
    if (e.key === 'Escape' && abierto) {
      // stopPropagation: si no, el mismo Escape cierra también el modal o la
      // pantalla que haya detrás.
      e.stopPropagation()
      setAbierto(false)
    }
  }

  return (
    <div className="menu-acciones" ref={caja} onKeyDown={alTeclear}>
      <button
        type="button"
        className="menu-acciones-boton"
        onClick={(e) => {
          // La fila entera suele ser clicable (abre la cuenta del cliente):
          // sin esto, abrir el menú te sacaría de la lista.
          e.stopPropagation()
          setAbierto((a) => !a)
        }}
        aria-label={etiqueta}
        aria-haspopup="menu"
        aria-expanded={abierto}
      >
        ⋯
      </button>

      {abierto && (
        <div
          className="menu-acciones-lista"
          role="menu"
          // El clic de cada acción corre primero (React burbujea de adentro
          // hacia afuera) y después esto cierra el menú.
          onClick={(e) => {
            e.stopPropagation()
            setAbierto(false)
          }}
        >
          {children}
        </div>
      )}
    </div>
  )
}
