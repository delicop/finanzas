import { useLayoutEffect, useRef, useState } from 'react'
import { formatearEntradaMonto } from '../lib/formato'

// Campo de monto que va poniendo los puntos de miles mientras escribes:
// tecleas 1500000 y ves 1.500.000, sin tener que contar ceros.
//
// El detalle que no es obvio: al reformatear, el texto cambia de largo
// (aparecen o desaparecen puntos), y el navegador manda el cursor al final.
// Si estás corrigiendo un dígito en la mitad del número, eso es insoportable.
//
// La solución es no guardar "la posición del cursor" sino CUÁNTOS DÍGITOS
// había antes de él: los puntos van y vienen, los dígitos no. Después de
// reformatear, se vuelve a poner el cursor tras esa misma cantidad de dígitos.
export default function InputMonto({ id, valor, onCambio, placeholder = '0' }) {
  const ref = useRef(null)
  const cursorPendiente = useRef(null)

  // Guarda cuántos dígitos quedaban antes del cursor, para restaurarlo.
  const [, forzarRender] = useState(0)

  // useLayoutEffect y no useEffect: corre ANTES de que el navegador pinte,
  // así el cursor nunca se ve saltar al final.
  useLayoutEffect(() => {
    if (cursorPendiente.current === null || !ref.current) return

    const digitosObjetivo = cursorPendiente.current
    cursorPendiente.current = null

    const texto = ref.current.value
    let digitos = 0
    let posicion = texto.length

    if (digitosObjetivo === 0) {
      posicion = 0
    } else {
      for (let i = 0; i < texto.length; i++) {
        if (/[\d,]/.test(texto[i])) digitos++
        if (digitos === digitosObjetivo) {
          posicion = i + 1
          break
        }
      }
    }

    ref.current.setSelectionRange(posicion, posicion)
  })

  function alEscribir(e) {
    const entrada = e.target
    const hastaCursor = entrada.value.slice(0, entrada.selectionStart ?? 0)

    // Contamos dígitos y coma: son los caracteres que el usuario "escribió"
    // de verdad. Los puntos los pone el formateador.
    cursorPendiente.current = (hastaCursor.match(/[\d,]/g) || []).length

    onCambio(formatearEntradaMonto(entrada.value))
    forzarRender((n) => n + 1)
  }

  return (
    <input
      id={id}
      ref={ref}
      // inputMode decimal saca el teclado numérico en el celular.
      // type sigue siendo "text" porque un type="number" no deja pintar
      // los puntos de miles.
      type="text"
      inputMode="decimal"
      autoComplete="off"
      value={valor}
      onChange={alEscribir}
      placeholder={placeholder}
    />
  )
}
