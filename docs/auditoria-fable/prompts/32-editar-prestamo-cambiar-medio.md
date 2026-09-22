# 32 · Editar un préstamo y cambiarle el medio

Contexto: al editar un `preste` (o cualquier salida) y cambiar el medio de
pago, la guardia de fondos (`fondos.go`, `comparar`) no tiene en cuenta que
la plata **vuelve** al medio anterior: solo ve que el nuevo medio queda
corto y pregunta de dónde salió, aunque el total no cambie. Además en
`MovimientoForm.jsx` el aviso "No te alcanza" no se actualiza cuando el
usuario corrige el medio o el monto, y el formulario queda trabado (el
botón no vuelve a habilitarse o el `cubrir` viejo se queda pegado).

Qué hacer:

1. Backend: la foto de saldos "antes" y "después" ya existe. Verifica que
   al editar, el "antes" se tome con el movimiento **viejo** aplicado y el
   "después" con el nuevo, para que el medio anterior recupere su plata en
   la comparación. Si `comparar` solo mira los medios que empeoran, está
   bien; el fallo probablemente está en que `verificarFondos` se llama con
   el medio nuevo sin descontar el viejo. Escribe primero el test que lo
   reproduce: Efectivo 100, Nequi 0, gasto de 100 en Efectivo, editar a
   Nequi con un traslado previo de 100 a Nequi → debe pasar sin preguntar.
2. Frontend, `MovimientoForm.jsx`: el estado `faltaPlata` se limpia cuando
   cambia `medio_pago_id`, `monto` o `fecha`; el botón vuelve a "Guardar";
   el `cubrir` se descarta. Solo se vuelve a preguntar si el backend vuelve
   a responder 409.
3. Test de integración para el caso 1 y uno más: editar solo la
   descripción de un gasto que ya dejó el medio "en rojo por datos viejos"
   no exige cuadrar (esa regla está en el comentario de `fondos.go` ~385 y
   debe seguir).

Commit: "Cambiarle el medio a un gasto ya cuenta la plata que vuelve al medio anterior".
