# Avisos

Las notificaciones que la app deja sin que nadie las pida: el resumen de cada
semana, los préstamos que llevas tiempo sin cobrar y, para el dueño del
servidor, a quién le falta pagarle el mes. Se leen en la campana de la barra
superior.

Viven en `backend/internal/avisos/`. **No dependen del asistente**: las cifras
las calcula Postgres y el texto lo arma la app. Si hay modelo configurado, lo
único que hace es redactar mejor el párrafo — y bajo vigilancia.

## Los tres avisos

| Aviso | Cuándo | Para quién |
|---|---|---|
| `resumen_semanal` | Lo que pasó la semana pasada (lunes a domingo), si hubo movimientos | Cada usuario |
| `prestamos_pendientes` | Hay préstamos pendientes con más de 30 días | Cada usuario |
| `cobros_del_mes` | Del día 5 en adelante, si alguien no ha pagado | Solo el administrador |

El resumen semanal no sale si la semana estuvo vacía: un "no registraste nada"
no le sirve a nadie. De paso, eso deja fuera al administrador, que no lleva
finanzas propias.

El recordatorio de préstamos se guarda con la clave del **mes**, no de la
semana: repetir lo mismo cada siete días no hace que te paguen más rápido, solo
que dejes de leer los avisos.

El de cobros no sale el día 1 porque "te faltan todos" al empezar el mes no es
información.

## Por qué no hay un cron

La tarea es una goroutine del propio servidor
([main.go](../backend/cmd/api/main.go)), que corre al arrancar y cada 6 horas.
Una pieza menos que instalar, que configurar y que se puede olvidar al mover la
app de máquina.

Que corra "cada 6 horas" y no "los lunes a las 8" no importa, porque **la tarea
no lleva ninguna cuenta**: cada aviso se guarda con una clave de periodo
(`2026-W38` para el semanal, `2026-09` para los mensuales) y un índice único
sobre `(usuario_id, tipo, clave)` impide el duplicado.

```sql
CREATE UNIQUE INDEX notificaciones_clave_key
    ON notificaciones (usuario_id, tipo, clave);
```

Eso es idempotencia por construcción, y es lo que hace que el sistema aguante
lo que de verdad pasa: que el servidor se reinicie tres veces seguidas, que
esté apagado un fin de semana (al volver genera lo que falte), o que alguien
dispare la tarea a mano. Guardar "la última vez que corrí" se desincroniza en
cuanto una de esas cosas ocurre a destiempo.

La clave de la semana sale de `ISOWeek` y no de una cuenta propia: en los
cambios de año la semana 1 puede empezar en diciembre, y calcularla a mano ahí
significa un resumen repetido o uno que nunca llega. Hay un test justo de ese
caso.

## El modelo redacta, pero no inventa cifras

El texto base lo arma Go con los números que ya sumó Postgres:

> Del 7 al 13 de septiembre registraste 2 movimientos: recibiste $900.000 y
> pagaste $45.000. En lo que más se te fue fue en Negocio 1: $45.000.

Si hay modelo configurado, se le pide que lo reescriba más natural. Y su
versión se acepta **solo si no aparece ninguna cifra que no estuviera en el
original** ([redactor.go](../backend/internal/avisos/redactor.go)):

```go
if nuevas := cifrasNuevas(base, redactado); len(nuevas) > 0 {
    return base   // se descarta su versión
}
```

Un modelo al que le das cifras y le pides prosa tiende a "ayudar": *"un 20% más
que la semana pasada"*. Ese 20% no lo calculó nadie. La comparación se hace sin
separadores, así que darle formato (`45000` → `$45.000`) sí se acepta: eso se
lee mejor y no cambia nada.

La guardia es deliberadamente estricta: *"pagaste como 45 mil"* también se
rechaza, porque 45 no es ninguna de las cifras que le dimos. Preferimos un
aviso más seco a uno bonito con un número que no es.

Y si el modelo se cae, tarda o devuelve vacío, sale el texto base. El aviso
nunca depende de que el proveedor esté vivo.

## Privacidad

Los avisos de una cuenta son de esa cuenta. El endpoint rechaza con 403 las
peticiones en modo "ver como" y la campana desaparece mientras el admin observa
a un cliente — el mismo criterio que con el chat del asistente
([agente.md](agente.md)).

## La tabla

```
notificaciones  tipo, titulo, cuerpo, clave (el periodo), leida_en
```

Cuelga de `usuarios` con `ON DELETE CASCADE`, y se limpian a los 90 días en la
misma tarea: un aviso viejo no le sirve a nadie y la tabla crece sola.

## Cómo se prueba

```bash
cd backend
go test ./internal/avisos/                      # fechas, formato y la guardia
TEST_DATABASE_URL="..." go test ./internal/avisos/   # + las cifras y el índice único
```

Las que más valen:

- **`TestElModeloPuedeRedactarPeroNoInventarCifras`** — acepta la reescritura y
  el cambio de formato; rechaza el 20% inventado y el total que nadie calculó.
- **`TestCorrerDosVecesNoRepiteElAviso`** — tres corridas, un solo aviso.
- **`TestSemanaAnterior/cambio_de_año`** — la clave ISO en el salto de año.
- **`TestCobrosDelMesSoloParaElAdminYNoElDiaUno`** — a un cliente no le llega el
  aviso del negocio del dueño.
