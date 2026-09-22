# El asistente

Un chat dentro de la app: el usuario escribe en lenguaje normal y le responde
un modelo de lenguaje. Vive en `backend/internal/agente/` y, en la app, en la
**burbuja verde flotante** de la esquina (`BurbujaAsistente.jsx`), que está en
todas las pantallas de dinero y abre el chat con forma de WhatsApp
(`ChatAsistente.jsx`). No tiene pestaña propia en el menú: una sola entrada.

El agente consulta los datos del usuario y además *prepara* movimientos a
partir de lo que le dicta — pero no los escribe: deja una tarjeta que el
usuario revisa, corrige y confirma.

También redacta los avisos automáticos (el resumen de cada semana, los
préstamos sin cobrar), aunque esos funcionan igual sin modelo:
ver [avisos.md](avisos.md).

## Las tres reglas que no se negocian

**1. El agente solo ve los datos del usuario de la sesión.**
El `usuario_id` sale del context —donde lo dejó el middleware de auth a partir
del JWT— y nunca del cuerpo de la petición. Tampoco es un argumento de las
herramientas: llega como parámetro de `Ejecutar`, y en el esquema que ve el
modelo ese campo **no existe**. Si alucina un `{"usuario_id": 7}`, se descarta
al decodificar los argumentos y la consulta sigue siendo sobre lo suyo. Es el
mismo cierre del IDOR que usa el resto de la app, extendido al chat.

Ninguna llamada del frontend lleva un id de conversación, tampoco. El hilo es
siempre el de quien tiene la sesión, así que no hay un número que alguien pueda
cambiar a mano para leer otro chat.

**2. El modelo no calcula plata.**
Nunca suma, nunca resta, nunca convierte. Las cifras salen de Postgres
—`NUMERIC(14,2)`, como todo el dinero de esta app—, ya sumadas, y el modelo
solo las redacta. Un modelo de lenguaje sumando montos es la versión cara del
`float`.

Puede darles formato para que se lean bien (`150000.00` → `$150.000`), pero no
cambiar sus dígitos. El resultado de cada herramienta lleva esa instrucción
adentro, además de estar en el prompt: es la regla que más caro sale romper.

Por eso las instrucciones del sistema (`prompt.go`) le prohíben inventar un
monto "de ejemplo": quien lee un número en una app de plata asume que es real.
Y si una herramienta no devuelve nada, eso *es* una respuesta —"no tienes
movimientos de eso"—, no una invitación a rellenar.

**3. El modelo no escribe: propone.**
`proponer_movimiento` y `proponer_marcar_pagado` no tocan la base del dinero:
guardan una propuesta y la app la pinta en una tarjeta **editable**. La
escritura ocurre en otra petición, disparada por un clic del usuario sobre unos
datos que ya vio.

No es desconfianza abstracta: el modelo oye "cuarenta y cinco mil" y casi
siempre escribe 45.000, pero el día que escriba 450.000 el usuario tiene que
poder verlo *antes*, no descubrirlo cuadrando el mes.

## Las herramientas

| Herramienta | Para qué |
|---|---|
| `resumen` | Totales, saldo por medio de pago, desglose por categoría y cuentas con cada quien |
| `listar_movimientos` | Movimientos concretos, con filtros |
| `listar_categorias` | Las categorías del usuario |
| `listar_medios_pago` | Sus medios de pago |
| `listar_contrapartes` | Con quién tiene cuentas pendientes, en los dos sentidos |
| `proponer_movimiento` | **Prepara** un movimiento para confirmar |
| `proponer_abono` | **Prepara** un pago parcial de una deuda |
| `proponer_marcar_pagado` | **Prepara** el saldo completo de una deuda |

Viven en [`herramientas.go`](../backend/internal/agente/herramientas.go) y cada
una envuelve un `Store` que ya existía: **no hay una sola consulta SQL nueva**.
Los endpoints de la API y el agente leen por el mismo camino, así que cualquier
filtro que proteja a uno protege al otro.

**El modelo nunca maneja ids.** Los filtros por categoría y por medio de pago
van por *nombre*, y el servidor los resuelve contra las listas del usuario. Si
el nombre no existe, la herramienta responde con la lista de los que sí —y el
modelo se corrige solo en la siguiente ronda— en vez de devolver una lista
vacía que parezca un "no tienes nada".

### Abonar o saldar

Entre las dos hay una regla simple, y está escrita en las descripciones que lee
el modelo: si lo que le dieron alcanza para todo lo que faltaba, es
`proponer_marcar_pagado`; si es menos, es `proponer_abono`. Cuando no es claro,
el modelo mira el saldo con `listar_movimientos` antes de decidir.

`listar_movimientos` devuelve `saldo` y `abonado` en cada deuda justamente por
eso. Sin ellos, el modelo diría "te deben $500.000" de un préstamo del que ya
devolvieron $400.000 — cierto en el monto original y falso en lo que importa.

Al saldar, lo que se registra es el **saldo**, no el monto: de un préstamo de
$500.000 con $400.000 abonados, saldarlo es poner los $100.000 que faltan. La
tarjeta lo dice explícitamente cuando la deuda ya tenía abonos.

### Con quién es la deuda

`listar_contrapartes` existe para un problema concreto: el modelo oye "Carlos
me abonó 50" y no sabe si "Carlos" es alguien que ya existe o un nombre nuevo.
Si escribe "Carlos M" donde ya decía "Carlos", quedan como **dos personas
distintas** y ninguno de los dos saldos es cierto.

La herramienta marca además `es_categoria` cuando ese nombre también es una
categoría del usuario, y el prompt le pide decirlo en voz alta: prestarle a
Negocio 2 no es lo mismo que registrar el movimiento *en* la categoría Negocio
2. Son dos cosas y se llaman igual.

Los resultados van **recortados**: sin ids, sin marcas de tiempo, sin rutas de
facturas. Nada de eso ayuda a responder y todo eso son tokens que se pagan en
esta llamada y en las siguientes. `listar_movimientos` devuelve 20 por omisión
y 50 como máximo, junto con el total que cumple los filtros: si la pregunta
necesita más, la respuesta correcta es un total, no trescientas líneas.

## Proponer y confirmar

```
"pagué 45 mil de almuerzo con Nequi"
        │
        ├─ proponer_movimiento → valida y guarda una PROPUESTA (nada más)
        │
        └─ la app pinta la tarjeta, editable
                │
                ├─ Guardar   → POST /api/agente/propuestas/{id}/confirmar
                │              → valida otra vez → movimientos.Store.Crear
                └─ Descartar → DELETE, y no pasó nada
```

Detalles que sostienen esto:

**El modelo no maneja ids ni monta su propia validación.** Escribe nombres
("Negocio 1", "Efectivo") y el servidor los resuelve; después la entrada pasa
por `movimientos.Validar`, la *misma* función que usa el formulario. Esa
función se extrajo del handler justamente para esto: si el agente tuviera su
copia de las reglas, el día que cambie una tendría también su propio bug. Si la
categoría no existe, no hay propuesta: vuelve un aviso con las que sí hay para
que el modelo pregunte o corrija.

**La tarjeta manda sobre lo que propuso el modelo.** El cuerpo de la
confirmación son los datos que el usuario tiene en pantalla, no los guardados,
y se validan igual. Lo que el modelo propuso queda en la propuesta como
registro de lo que se ofreció.

**Un doble clic no puede duplicar un gasto.** Confirmar primero *reserva* la
propuesta con un `UPDATE ... WHERE estado = 'pendiente'`: de dos peticiones
simultáneas, solo una encuentra la fila pendiente; la otra recibe 409. Con un
SELECT y luego un UPDATE habría una ventana en la que ambas pasarían. Si la
escritura falla después de reservar, la propuesta vuelve a `pendiente` para que
el usuario corrija y reintente.

**Las propuestas caducan a las 24 horas.** Confirmar hoy un "pagué 45 mil" de
anteayer entraría con la fecha de la tarjeta y descuadraría el mes sin que
nadie se entere.

**Sobreviven a recargar la página.** `GET /api/agente` devuelve las pendientes;
si desaparecieran al recargar, el usuario creería que se guardó algo.

Y el prompt insiste en lo que más importa después de usar estas herramientas:
*nunca* decir "listo, lo registré". Decir que ya quedó cuando no ha quedado es
la peor mentira posible en una app de plata.

## El ciclo

```
pregunta → modelo → ¿pidió herramientas?
                      │
                      ├── sí → se ejecutan → resultados de vuelta al modelo ─┐
                      │                                                     │
                      │         (hasta MaxRondas = 4 vueltas)  ←────────────┘
                      │
                      └── no → ese texto es la respuesta: se guarda y se responde
```

Cada vuelta es otra llamada al proveedor —otros segundos y otra fracción de
centavo—, así que hay un techo de cuatro. Con estas herramientas dos sobran:
una para consultar y otra para redactar. Si se agotan, el usuario recibe un 503
con un mensaje claro en vez de una cuenta creciendo.

**Del ir y venir no queda nada en la base.** Al hilo llega el texto final; los
mensajes intermedios (la petición de datos y el JSON de cada herramienta) viven
solo mientras dura la respuesta. En el turno siguiente el modelo no recuerda lo
que consultó, pero puede volver a consultarlo, y a cambio el historial no se
llena de JSON que se paga en cada llamada.

Los tokens que se guardan son los de **todas** las rondas sumadas: si solo se
contara la última, el costo real del agente quedaría subestimado.

## Se ve qué consultó

Cada respuesta guarda qué herramientas usó (columna `herramientas`), y la app
lo muestra debajo: *"Consultó tu resumen"*. Quien lee una cifra en una app de
plata tiene derecho a saber de dónde salió, y meses después deja auditable qué
fue lo que el agente miró.

## Cuando el modelo se equivoca

Una herramienta inventada, un `tipo` que no existe, una fecha en otro formato:
todo eso vuelve **como resultado de la herramienta**, en JSON, para que el
modelo lo lea y se corrija. No es un fallo del servidor y no tumba la
respuesta.

Solo los errores de verdad —que falle la base de datos— suben como error de Go,
quedan en la bitácora y le dan un 500 al usuario. La distinción está en
`Catalogo.Ejecutar`: devuelve `(resultado, error)` y el `error` está reservado
para lo segundo.

## El chat es privado, incluso para el administrador

Un admin en modo "ver como" puede revisar los movimientos de un cliente: es
parte de llevar el negocio. Su conversación con el asistente, no. No es un
registro de dinero: es lo que esa persona escribió creyendo que era privado.

El handler rechaza con 403 cualquier petición que venga marcada como
observación (`httpx.Observador`), y el menú ni siquiera muestra la sección
mientras se observa. Sin ese corte, el middleware `VerComo` cambiaría el id del
context y el `GET` devolvería el hilo ajeno sin que ningún handler se enterara.

## Cómo se configura

**Quién puede usarlo: solo los clientes cuyo plan incluye IA.** Sin plan, o
con un plan sin `incluye_ia`, `/api/agente` responde **403** y la burbuja no
aparece. La revisión va en cada petición (`auth.Store.TieneIA`),
así que quitarle la IA a un plan corta el chat de inmediato. El handler recibe
ese permiso como función: si alguien lo arma sin él, **nadie** entra.

Sin `LLM_API_KEY` **el chat no existe**: las rutas no se montan y la app
funciona exactamente igual. Es el mismo criterio de `TOKEN_MANTENIMIENTO`: una
funcionalidad opcional se apaga sola cuando no está configurada, en vez de
arrancar a medias y fallar en la primera petición.

```bash
LLM_API_KEY=...
LLM_BASE_URL=https://api.deepseek.com/v1    # o https://openrouter.ai/api/v1
LLM_MODELO=deepseek-chat                    # o deepseek/deepseek-chat en OpenRouter
LLM_TIMEOUT_SEGUNDOS=25
LLM_LIMITE_DIARIO=50
```

El cliente habla con una API **compatible con la de OpenAI**
(`POST {base}/chat/completions`), que es lo que ofrecen tanto DeepSeek como
OpenRouter. Cambiar de proveedor es cambiar dos variables, no código.

El timeout tiene que quedar por debajo de los 30 s a los que el router corta
toda petición (`middleware.Timeout`); si fuera mayor, nunca llegaría a
cumplirse y el usuario vería un error genérico en vez del mensaje nuestro. La
configuración lo valida al arrancar.

## El límite de mensajes

Cada usuario puede mandar `LLM_LIMITE_DIARIO` mensajes en una ventana móvil de
24 horas. Cada mensaje le cuesta plata al dueño del servidor: sin techo, un
cliente con un script le deja la factura del mes en la mano.

La ventana es móvil y no "desde la medianoche" porque el servidor corre en UTC
y el usuario no: el corte a medianoche le llegaría a una hora arbitraria de la
tarde.

El contador vive en su propia tabla (`agente_consumo`) y **no** se deduce del
historial. Si se contara sobre `agente_mensajes`, el botón de "empezar de cero"
—que borra la conversación— devolvería los mensajes del día y el techo de costo
se saltaría con un clic. La tabla guarda solo el *cuándo*, nunca el texto: así
borrar el hilo borra de verdad todo lo que se escribió.

## Qué pasa cuando el modelo falla

| Situación | Qué ve el usuario | Qué queda en la bitácora |
|---|---|---|
| El proveedor se cayó o está saturado (429, 5xx, timeout) | 503, "intenta de nuevo en un minuto" | Sí |
| Respondió sin texto | 503, "pregúntalo de otra forma" | Sí |
| La llave está vencida o sin saldo (401, 402) | 500 genérico | Sí, con el detalle |

La distinción importa: que el proveedor se caiga no es un error de la app, pero
una llave vencida sí es algo que hay que ir a arreglar, y disfrazarla de "no
disponible" haría que nadie la arreglara. La llave nunca viaja en el mensaje de
error —hay un test que lo comprueba—, porque la bitácora se lee desde la app.

La pregunta del usuario se guarda **antes** de llamar al modelo: si el
proveedor falla, sigue en pantalla en vez de perderse. Y el consumo se anota al
aceptar el mensaje, no al responderlo: si solo se cobrara el éxito, un
proveedor fallando en bucle sería un reintento infinito gratis contra la cuota.

## Foto o PDF de la factura

El clip 📎 del chat adjunta la foto (o el PDF) de la factura de un gasto:

- Si ya hay una tarjeta de gasto esperando, la foto va directo a esa tarjeta.
- Si no, queda "lista" encima del cajón y se pega a la próxima tarjeta de
  gasto que prepare el asistente. El mensaje sale con "📎 (con la foto de la
  factura)" para que el asistente sepa que ya la tiene.
- Cada tarjeta de gasto tiene además su propio botón **📎 Adjuntar factura**,
  y cuando el asistente prepara un "pagué" **pregunta si tienes la foto o el
  PDF** (se lo indica la herramienta `proponer_movimiento`; a los préstamos e
  ingresos no se les pide).
- Al **Guardar**, primero se crea el movimiento y luego se sube la factura con
  la misma ruta del formulario (`POST /api/movimientos/{id}/factura`), con sus
  mismas validaciones (tipo real del archivo, 10 MB máximo). Si la subida
  falla, el movimiento ya quedó y el chat avisa que la factura se puede
  adjuntar desde Movimientos.

**El asistente no lee la foto**: el modelo solo entiende texto, y el monto y
el comercio los dice la persona. La foto es el soporte del gasto. Antes de
subirla, el navegador la achica a 2000 px (`lib/imagen.js`): una foto de
teléfono pasa de ~5 MB a unos cientos de KB.

## Notas de voz

Con el cajón vacío, el botón de enviar se vuelve un micrófono 🎤 (como en
WhatsApp). Se toca, se habla ("pagué 20 mil de gasolina") y el texto aparece
en el cajón; se toca ■ para parar antes. **No se envía solo**: lo dictado se
revisa primero, porque un "18 mil" mal oído como "80 mil" es plata.

Usa el reconocimiento de voz del propio navegador (Web Speech API,
`frontend/src/lib/dictado.js`), en español de Colombia. No cuesta nada ni
pasa por nuestro servidor: al backend llega el texto, igual que si se hubiera
escrito. Ojo con la privacidad: Chrome manda el audio a Google para
convertirlo.

| Navegador | ¿Funciona? |
|---|---|
| Chrome / Edge (Android, computador) | Sí |
| Safari (iPhone, Mac) | Sí |
| Firefox | No: el micrófono no aparece |

Necesita HTTPS (o `localhost`) y permiso de micrófono. Si el permiso está
negado, el chat lo dice.

## Que te responda en voz alta

Cada respuesta del asistente lleva debajo un **Escuchar** 🔈, como el altavoz
de ChatGPT o Claude. Se toca y la lee; mientras suena, ese mismo botón dice
**Parar**. Se pide respuesta por respuesta, que es como se oye de verdad: la
cifra que se quería, no la conversación entera. Solo una a la vez — empezar
otra corta la anterior. Las conversaciones guardadas también se pueden
escuchar, con el mismo botón.

El altavoz de la **cabecera** es otra cosa: que se lean TODAS solas, sin
tocar nada, para oír el saldo con el teléfono en el bolsillo. **Viene
apagado** y la decisión se recuerda en el navegador: una app de plata que se
pone a hablar sola en una reunión es un problema, pero volver a apagarla
todos los días también. Apagarlo calla lo que esté sonando, igual que
terminar la conversación o borrar el historial.

Es la otra mitad de la Web Speech API (`frontend/src/lib/voz.js`), y tampoco
cuesta nada. A diferencia del dictado, aquí **no sale nada del dispositivo**:
la voz la sintetiza el propio aparato. Se elige la voz en español más cercana
que tenga instalada (Colombia, luego cualquiera de América, luego la que
haya); si no tiene ninguna, se deja la de por defecto con `lang = 'es-CO'`.

Firefox sí trae esta parte, así que funciona en un navegador más que el
micrófono.

Tres detalles que no son obvios y están resueltos en el código:

- **El texto se prepara antes de decirlo** (`paraLeer`). Lo que importa son
  los montos: `$45.000` leído tal cual suena "cuarenta y cinco punto cero
  cero cero", así que se le quitan los puntos de los miles y se le agrega
  "pesos". También se sueltan las `**negritas**`, los emojis y las viñetas, y
  cada salto de línea pasa a ser una pausa.
- **iPhone**: Safari solo deja hablar si la primera vez salió de un toque del
  usuario. Con el botón de cada respuesta eso sobra —ese toque ya es el
  permiso—, pero el modo automático lee algo que llega segundos después de
  enviar. Por eso, al prender el altavoz de la cabecera se dice un silencio
  (volumen 0) dentro de ese mismo toque: eso abre el permiso para las frases
  de después.
- **Chrome en computador** corta el audio cerca de los 15 segundos (un bug
  viejo suyo). Mientras haya algo que decir se le hace `pause()` + `resume()`
  cada 10 s, que es el remedio conocido; una respuesta larga del asistente
  pasa de sobra ese tope.

En automático solo se lee la respuesta que **acaba de llegar**: al abrir el
chat no se relee el hilo entero.

### Por qué no un TTS de servidor

Una voz de servidor (OpenAI, ElevenLabs) suena mucho más natural, pero el
proveedor del chat es una API compatible con la de OpenAI —hoy DeepSeek— que
**no hace audio**: tocaría contratar otro servicio, pagar por respuesta,
aguantar la latencia extra y hacer pasar el audio por nuestro servidor. Para
lo que se necesita aquí —oír el saldo sin mirar la pantalla— no compensa. Si
algún día compensa, el cambio es solo dentro de `voz.js`: quien lo llama solo
dice `voz.hablar(texto)`.

## Terminar y guardar la conversación

Un chat que nunca se limpia tiene dos problemas: el usuario tiene que leer todo
lo anterior cada vez que lo abre, y el modelo relee los últimos mensajes en
cada pregunta (más lento y más caro). Por eso la conversación se **termina**:

- Después de guardar un movimiento desde una tarjeta, el chat pregunta
  **"¿Necesitas algo más?"** con dos respuestas: *Sí, otra cosa* (sigue
  escribiendo) y *No, terminar*.
- En el menú ⋮ de la cabecera también está *Terminar y guardar*.

Terminar **no borra**: la conversación queda archivada
(`agente_conversaciones.archivada_en`) con la primera pregunta como título, y
el siguiente mensaje abre un hilo nuevo. Desde el menú ⋮:

| Opción | Qué hace |
|---|---|
| Terminar y guardar | Archiva la abierta y deja el chat limpio |
| Descargar esta conversación | Un `.txt` con la conversación abierta |
| Conversaciones guardadas | Lista para leer, descargar o borrar cada una |
| Borrar todo el historial | Borra la abierta **y** las guardadas (no se puede deshacer) |

Reglas que cuida el backend:

- **Una sola conversación abierta por usuario**, con un índice único parcial.
  Si dos pestañas escriben a la vez, la segunda usa la que abrió la primera.
- Al terminar, las **tarjetas sin confirmar** de esa conversación se descartan:
  en el chat nuevo aparecerían sin el contexto que las explica.
- Un chat **sin mensajes** no se guarda: se borra.
- Las guardadas **solo se leen**. Seguir escribiendo en una vieja obligaría al
  modelo a releerla entera, que es justo lo que terminar evita.
- La de otro usuario responde 404, como si no existiera.

Terminar no toca el límite diario: el contador vive en `agente_consumo`.

## Privacidad

Los mensajes salen del servidor hacia el proveedor del modelo. Van las preguntas del
usuario, su nombre de pila y **los datos que devuelven las herramientas**:
montos, descripciones, fechas y los nombres de las personas a las que les
prestó.

Vale la pena tenerlo escrito antes de abrirlo a clientes: si el asistente va a
estar disponible para ellos y no solo para el dueño, lo razonable es dejarlo
apagado por defecto y que cada quien lo active.

## Las tablas

```
agente_propuestas      lo preparado y sin confirmar, con su estado
agente_conversaciones  una abierta por usuario (archivada_en NULL) y las
                       guardadas, con su título
agente_mensajes        las líneas del hilo, con los tokens y las herramientas
                       que gastó cada una
agente_consumo         una marca por mensaje gastado: el contador del límite
```

Las tres cuelgan de `usuarios` con `ON DELETE CASCADE`. Aquí sí es cascada y no
`RESTRICT` como en categorías y medios: una conversación no es un registro de
dinero, y si la cuenta se va, su chat se va con ella.

## Cómo se prueba

```bash
cd backend
go test ./internal/agente/                      # el contrato con el proveedor
TEST_DATABASE_URL="postgres://finanzas:<clave>@localhost:5436/finanzas_test?sslmode=disable" \
  go test ./internal/agente/                    # + el aislamiento entre usuarios
```

Las pruebas del proveedor levantan un servidor de mentiras que responde como
respondería DeepSeek —incluido el formato de las llamadas a herramientas, que
es donde más fácil se rompe la integración—: no necesitan internet ni gastan un
peso. Las de integración corren contra un Postgres de verdad, porque lo que
prueban es el filtro por `usuario_id`.

`Proveedor` es una interfaz de un solo método justamente para eso: el ciclo
completo se prueba con un guion ("primero pide `resumen`, después contesta
esto") y sin salir a la red.

Las que más valen:

- **`TestProponerNoEscribeNada`** — el agente propone y en `movimientos` no hay
  ni una fila. Si esto se rompe, se rompió lo único que justifica tener una
  tabla de propuestas.
- **`TestConfirmarDosVecesNoDuplica`** — dos clics en Guardar dejan un solo
  movimiento.

- **`TestUnUsuarioIdInventadoNoTieneEfecto`** — el modelo pide los movimientos
  "del usuario 42" (el id de Ana) mientras quien pregunta es Beto: la
  herramienta devuelve los de Beto, que no tiene ninguno.
- **`TestNingunaHerramientaPideElUsuario`** — recorre el catálogo que se le
  ofrece al modelo y falla si alguna herramienta llegara a aceptar un argumento
  con el usuario. La regla deja de depender de que alguien se acuerde.
