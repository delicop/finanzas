# Avisos

Las notificaciones que la app deja sin que nadie las pida: el resumen de cada
semana, el día en que alguien quedó de pagarte (o en que te toca pagar a ti),
las cuotas de un acuerdo que se vencieron, las deudas que llevas tiempo sin
mover, los gastos que se repiten y están por confirmar y, para el dueño del
servidor, a quién le falta pagarle el mes.

Se leen en la campana de la barra superior y, si el servidor tiene llaves VAPID
configuradas, **llegan también al celular con la app cerrada** (ver
[Avisos al celular](#avisos-al-celular)).

Viven en `backend/internal/avisos/`. **No dependen del asistente**: las cifras
las calcula Postgres y el texto lo arma la app. Si hay modelo configurado, lo
único que hace es redactar mejor el párrafo — y bajo vigilancia.

## Los avisos

| Aviso | Cuándo | Para quién |
|---|---|---|
| `resumen_semanal` | Lo que pasó la semana pasada (lunes a domingo), si hubo movimientos | Cada usuario |
| `cobro_del_dia` | El día en que quedaron de devolverte un préstamo | Cada usuario |
| `pago_del_dia` | El día en que **tú** quedaste de pagar algo que te prestaron | Cada usuario |
| `cuota_vencida` | Una cuota de un acuerdo llegó a su fecha sin que los abonos la cubran | Cada usuario |
| `prestamos_pendientes` | Te deben algo desde hace más de 30 días | Cada usuario |
| `deudas_propias` | Debes algo desde hace más de 30 días | Cada usuario |
| `recurrente_pendiente` | Al arriendo (o lo que sea) le tocó y falta confirmarlo | Cada usuario |
| `cobros_del_mes` | Del día 5 en adelante, si alguien no ha pagado | Solo el administrador |

El resumen semanal no sale si la semana estuvo vacía: un "no registraste nada"
no le sirve a nadie. De paso, eso deja fuera al administrador, que no lleva
finanzas propias.

El recordatorio de deudas se guarda con la clave del **mes**, no de la semana:
repetir lo mismo cada siete días no hace que te paguen más rápido, solo que
dejes de leer los avisos.

Lo que te deben y lo que debes van en **dos avisos distintos** y no en uno solo
con las dos cifras. Son dos acciones diferentes: una se resuelve escribiéndole
a alguien y la otra sacando plata. Un párrafo que diga "te deben 300 y debes
200" no le dice a nadie qué hacer hoy.

Todas las cifras van sobre el **saldo**, no sobre el monto original. De un
préstamo de $500.000 con $400.000 ya devueltos, lo que llevas sin cobrar son
$100.000. Con el monto, el aviso exageraría más cuanto más te fueran pagando,
que es justo al revés de lo que tiene que pasar.

El de cobros no sale el día 1 porque "te faltan todos" al empezar el mes no es
información.

### El día del cobro

Al registrar un préstamo ("Presté") se puede poner **¿Cuándo te paga?**: la
fecha de pago acordada (`movimientos.cobrar_el`, opcional). Ese día llega
**"Hoy te paga Carlos"** con el monto, cuándo se prestó y para qué.

- Un aviso **por préstamo**, con clave `<id del movimiento>:<fecha>`: si se
  cambia la fecha, el nuevo día también avisa.
- Si ya está pagado, no avisa.
- El "hoy" es el de **Colombia** (UTC-5), no el del servidor: a las 8 de la
  noche en Bogotá el servidor ya está en el día siguiente.
- Si el servidor estuvo apagado ese día, el aviso sale al volver (hasta 3 días
  después) diciendo "Carlos quedó de pagarte el 14 de septiembre". Más tarde ya
  lo cubre el recordatorio mensual de préstamos pendientes.
- La fecha también se ve en la lista de movimientos y en "Te deben" del
  Resumen: "te paga hoy" (ámbar), "te paga en 3 días" (verde), "venció hace 2
  días" (rojo).
- El asistente la entiende: "le presté 50 mil a Juan, me paga el viernes" y la
  pone en la tarjeta. Si no la dicen, pregunta una vez.
- Funciona **en los dos sentidos**: si lo que registraste es un
  `me_prestaron`, el aviso es `pago_del_dia` y dice "Hoy le pagas a Carlos".
  Es la misma fecha y el mismo color, pero la acción es la contraria.

### Las cuotas de un acuerdo

Una deuda puede tener un acuerdo de pago: cuotas con su fecha. Las cuotas son
el **calendario**, no la plata — lo que se debe sigue siendo el saldo.

Una cuota está cubierta cuando el acumulado de cuotas hasta ella cabe en lo que
ya se abonó: los abonos las cubren **en orden**. El día que una llega a su
fecha sin estar cubierta, sale un `cuota_vencida` con lo que falta para
ponerse al día con esa (que puede ser menos que la cuota, si ya se abonó una
parte).

- Un aviso **por cuota**, con el id de la cuota en la clave. Si el acuerdo se
  renegocia, las cuotas nuevas son otras filas y vuelven a avisar — es lo
  correcto, es un acuerdo distinto.
- La ventana de gracia es de **7 días**, más ancha que la de los cobros
  sueltos: una cuota que se pasa no deja de importar al tercer día, sigue
  debiéndose y el acuerdo entero se corre detrás de ella.
- Es exactamente la misma regla que usa la pantalla del acuerdo. Tiene que
  serlo: si el aviso contara de otra forma, diría que falta una cuota que la
  app muestra como pagada.

### Los gastos que se repiten

La misma tarea que genera los avisos pone al día las ocurrencias de los
recurrentes: mira a cuáles les tocó y crea las que falten (el índice único
sobre *recurrente + fecha* impide repetirlas). Después avisa de las que siguen
sin resolver.

Si el usuario ya confirmó el arriendo, no hay aviso. Si lo descartó, tampoco:
la ocurrencia queda resuelta y no vuelve.

La app **no crea el movimiento sola**, ni siquiera cuando el monto es siempre
el mismo. Un gasto inventado no se nota nunca; uno olvidado salta al cuadrar el
mes.

## Por qué no hay un cron

La tarea es una goroutine del propio servidor
([main.go](../backend/cmd/api/main.go)), que corre al arrancar y cada hora
(para que el aviso del día de cobro llegue temprano).
Una pieza menos que instalar, que configurar y que se puede olvidar al mover la
app de máquina.

Que corra "cada hora" y no "los lunes a las 8" no importa, porque **la tarea
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

Antes de redactar un aviso se pregunta si ya existe: así el modelo solo se
llama para los avisos nuevos, y correr cada hora no cuesta más.

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


## Avisos al celular

Un aviso dentro de la app solo sirve si la app está abierta. Web Push resuelve
eso: el navegador mantiene un canal con el servicio de su fabricante (Google
para Chrome, Apple para Safari, Mozilla para Firefox) y el servidor le entrega
el mensaje a *ese* servicio, que hace de cartero.

Vive en `backend/internal/push/`. **Sin llaves VAPID configuradas no existe**:
la app funciona igual y los avisos se quedan en la campana. Mismo criterio que
el chat y el token de mantenimiento — lo opcional se apaga solo, no arranca a
medias.

### Qué es VAPID

El cartero no acepta paquetes de cualquiera: si no, cualquier servidor que
consiga un endpoint podría bombardear al usuario. VAPID (RFC 8292) es cómo el
servidor se identifica: firma un token corto con una llave privada suya y manda
al lado la pública. El servicio verifica la firma y sabe que los mensajes
vienen siempre del mismo remitente.

No es una cuenta ni una contraseña con nadie: es un par de llaves que se genera
**una vez** y se guarda en el `.env`:

```bash
cd backend && go run ./cmd/vapid
```

La pública la ve el navegador (no es secreta). La privada no se comparte ni se
sube al repositorio. Si algún día se cambian, todos los que tenían los avisos
activados tienen que volver a activarlos.

### Por qué además va cifrado

El cartero entrega, pero no tiene por qué leer. El contenido va cifrado
(RFC 8291, `aes128gcm`) con una llave que solo conocen el navegador del usuario
y este servidor, derivada de las credenciales que el propio navegador generó al
suscribirse. Google puede ver que le mandamos algo a alguien; no puede ver que
dice "Hoy te paga Carlos $200.000".

El cifrado está implementado con la librería estándar de Go (`crypto/ecdh`,
`crypto/hkdf`, `crypto/aes`), sin dependencias nuevas, y la prueba lo compara
**byte por byte** con el ejemplo del RFC. No es por gusto: un error aquí no
revienta nada, simplemente los avisos nunca llegan — o llegan y el navegador
los descarta en silencio. "Que no falle" es exactamente lo que también haría
una implementación mal hecha.

### Detalles que importan

- Un usuario puede tener **varios dispositivos**: el celular y el computador
  son dos suscripciones. Si uno falla, los demás igual reciben.
- El endpoint es único en el mundo, así que volver a suscribirse actualiza la
  misma fila en vez de duplicarla. Sin eso, cada rotación de llaves del
  navegador dejaría al usuario recibiendo el mismo aviso dos, tres, cuatro
  veces.
- Si el servicio responde 404 o 410, la suscripción **se borra**: el usuario
  desinstaló la app o limpió el navegador. No es un fallo que reintentar.
- Un fallo de push **nunca** tumba el aviso: el aviso ya quedó guardado y el
  usuario lo va a ver en la campana. Que Google esté caído no puede hacer que
  la tarea marque como fallida la generación de un aviso que sí se creó.
- El clic lleva a la pantalla que corresponde (Movimientos, Recurrentes, el
  panel). Abrir la app en la pantalla equivocada es casi tan malo como no
  avisar.
- El navegador **solo permite push por HTTPS** (o en localhost). En la
  Raspberry hace falta un certificado; sin él, el interruptor no aparece.
- En iPhone solo funciona con la app instalada desde Compartir › *Agregar a
  inicio*.
