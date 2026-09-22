# 06 · El monto del cobro del negocio se manda sin convertir

Contexto: en `frontend/src/paginas/Negocio.jsx` (~líneas 486-492) el
formulario de registrar un pago usa un `<input>` plano y envía `monto` tal
cual, sin pasar por `entradaAMonto` de `lib/formato.js`. Un usuario
colombiano escribe "25.000" y el backend recibe `25.000` (25 con tres
decimales) o lo rechaza. Todo el resto de la app usa el componente
`InputMonto`.

Qué hacer:

1. Reemplaza ese input por `InputMonto` y convierte con `entradaAMonto`
   antes de llamar al API, igual que hace `MovimientoForm.jsx`.
2. Revisa el mismo formulario en `Clientes.jsx` (asignar plan / cobrar) y en
   `Planes.jsx` (precio del plan): si alguno manda el monto crudo, corrígelo
   igual.
3. Comprueba en el backend (`suscripciones/handler.go`) que el monto se
   valida con `dinero.Validar` como los demás. Si no, agrégalo.

Commit: "El monto del cobro se escribe como en el resto de la app".
