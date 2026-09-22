# 14 · La cuota diaria del agente y su tiempo total

Contexto: en `backend/internal/agente/handler.go` (~166-190) se consulta
cuántos mensajes lleva el usuario y luego se inserta el consumo: N
peticiones simultáneas pasan todas la verificación y se saltan
`LLM_LIMITE_DIARIO`. El consumo se registra antes de responder, así que un
fallo del modelo también cobra. Aparte, `MaxRondas = 8` (`agente.go` ~446)
por hasta 25 s cada llamada (`LLM_TIMEOUT_SEGUNDOS`) contra el
`middleware.Timeout(30s)` del router: el ciclo puede pasarse del timeout y
el usuario ve un error genérico.

Qué hacer:

1. Reserva atómica: un solo `INSERT` en la tabla de consumo condicionado a
   que el conteo de las últimas 24 horas sea menor que el límite
   (`INSERT ... SELECT ... WHERE (SELECT count(*) ...) < $2 RETURNING id`).
   Si no devuelve fila, responde 429. Sin consulta previa separada.
2. Si el proveedor falla (5xx, timeout) antes de la primera respuesta,
   borra esa reserva: no se cobra lo que no se respondió.
3. Presupuesto de tiempo total en `responder`: un context con deadline de
   (timeout del router menos 2 s) que envuelva todas las rondas. Si se agota,
   el agente responde "me tardé demasiado, ¿lo intentamos de nuevo?" con lo
   que tenga, no un 500.
4. Que `config.Load` falle al arrancar si el doble de
   `LLM_TIMEOUT_SEGUNDOS` supera los 30 s del router, con un mensaje que
   explique la relación.
5. Test de concurrencia en `integracion_test.go` del agente: 20 goroutines
   con límite 5 y exactamente 5 pasan.
6. Actualiza `docs/agente.md` (secciones "El límite de mensajes" y "Qué pasa
   cuando el modelo falla").

Commit: "El tope de mensajes del asistente no se salta mandando varios a la vez".
