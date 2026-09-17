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
| `resumen` | Totales, saldo por medio de pago, desglose por categoría y deudores |
| `listar_movimientos` | Movimientos concretos, con filtros |
| `listar_categorias` | Las categorías del usuario |
| `listar_medios_pago` | Sus medios de pago |
| `proponer_movimiento` | **Prepara** un movimiento para confirmar |
| `proponer_marcar_pagado` | **Prepara** el cobro de un préstamo |

Viven en [`herramientas.go`](../backend/internal/agente/herramientas.go) y cada
una envuelve un `Store` que ya existía: **no hay una sola consulta SQL nueva**.
Los endpoints de la API y el agente leen por el mismo camino, así que cualquier
filtro que proteja a uno protege al otro.

**El modelo nunca maneja ids.** Los filtros por categoría y por medio de pago
van por *nombre*, y el servidor los resuelve contra las listas del usuario. Si
el nombre no existe, la herramienta responde con la lista de los que sí —y el
modelo se corrige solo en la siguiente ronda— en vez de devolver una lista
vacía que parezca un "no tienes nada".

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
