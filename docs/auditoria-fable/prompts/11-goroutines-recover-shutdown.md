# 11 · Las tareas de fondo no deben tumbar el servidor ni morir a medias

Contexto: `cmd/api/main.go` (~120-123) lanza tres goroutines: limpiar la
bitácora, generar avisos y limpiar el consumo del agente. Ninguna tiene
`recover`: un panic dentro del generador de avisos (que llama al modelo y a
push, el código con más superficie) tumba el proceso entero. En el apagado
(~169-176) solo se espera al servidor HTTP; `defer pool.Close()` corre
mientras el generador puede estar a media transacción.

Qué hacer:

1. Una función `tareaDeFondo(ctx, nombre, cada time.Duration, fn func(ctx))`
   que envuelva las tres: `defer` con `recover()` que registre el panic con
   `slog.Error` (y en la bitácora de errores, vía `registro`, si es fácil) y
   siga viva; el ticker adentro.
2. Un `sync.WaitGroup` en `run()`: cada tarea hace `Add(1)`/`Done()`. Tras
   `srv.Shutdown`, esperar el WaitGroup con un tope de 15 s y **después**
   cerrar el pool.
3. Que `generador.Correr` reciba un context con timeout propio (por ejemplo
   10 min) para que una corrida colgada no bloquee el apagado.
4. Test unitario de `tareaDeFondo`: una `fn` que hace panic no mata el test
   y la tarea vuelve a correr en el siguiente tick.
5. Actualiza el párrafo de `docs/arquitectura.md` sobre las goroutines.

Commit: "Un fallo en los avisos automáticos ya no apaga el servidor".
