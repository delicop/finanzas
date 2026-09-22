# 36 · Textos confusos, decimales y guiones

Contexto, de las pruebas manuales:

- En "Cuentas con cada quien" dice **"le pagas"** cuando es **"te paga"**
  (o al revés): el verbo no mira quién debe a quién.
- "no debes nada" aparece cuando sí debes (probablemente mira solo
  `preste` y no `me_prestaron`).
- "¿Cómo fue el pago?" como etiqueta del medio de pago no se entiende en un
  ingreso ni en un traslado.
- Queda alguna "Balance" donde ya se decidió decir "Tienes" (commit
  d0beee3).
- "Datos inválidos" genérico (ver también prompt 29).
- Los montos muestran **un solo decimal** en algunos sitios ($1.322,5) y
  dos en otros.
- Se usan dos guiones distintos para lo negativo (−, -) según la pantalla.

Qué hacer:

1. `lib/formato.js`: una sola `formatearMonto` que siempre muestre dos
   decimales cuando hay centavos y ninguno cuando no, con el signo menos
   tipográfico (U+2212) siempre. Un test por caso (prompt 07). Recorre
   `Dashboard.jsx` (~282 hace `String(c.neto).replace('-','')`) y quita
   cualquier formateo a mano.
2. "Cuentas con cada quien": el texto depende del signo del neto:
   `neto > 0` → "te debe", `neto < 0` → "le debes", `0` → "a paz y salvo".
   Escribe los tres casos en un helper puro y pruébalo.
3. Etiqueta del medio según tipo: ingreso "¿Por dónde entró?", gasto "¿Con
   qué pagaste?", préstamo "¿Por dónde salió?", traslado "De" y "A".
4. Grep de "Balance" y de "no debes nada" y corrige con la condición
   correcta (incluir `me_prestaron` con saldo > 0).
5. Recorre los mensajes de error del backend que digan "Datos inválidos"
   sin campo y ponles campo y razón (`httpx.Validador`).

Commit: "Los textos dicen quién le debe a quién, y los montos se ven igual en toda la app".
