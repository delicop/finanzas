# Finanzas personales

App de finanzas personales vendida por suscripción. Cada cliente ve **solo sus
propios datos**; el administrador lleva el negocio: los planes, a quién le cobra
y quién ya pagó el mes. Todo corre en Docker.

> **Documentación completa en [`docs/`](docs/)** — arquitectura, decisiones
> técnicas, API, [cómo revisar los errores del servidor](docs/errores.md),
> pruebas y despliegue en la Raspberry Pi.

```
/backend    API en Go (monolito)
/frontend   SPA en React + Vite
docker-compose.yml   levanta postgres + backend + frontend
```

## Arrancar en desarrollo

```bash
cp .env.example .env     # y cambia POSTGRES_PASSWORD y JWT_SECRET
docker compose up --build
```

- Frontend: http://localhost:5173
- API: http://localhost:8080
- Postgres desde el host: `localhost:5436` (configurable con `DB_PORT_HOST` en `.env`)

Las migraciones se aplican solas al arrancar el backend.

### Crear el primer usuario

No hay endpoint público de registro: sería una puerta abierta a que cualquiera
se cree una cuenta en el servidor. El primero se crea a mano y queda como
**administrador**; desde ahí se crean los demás en la propia app.

```bash
docker compose exec backend /app/createuser -email tu@correo.com -nombre "Tu Nombre"
```

Pide la contraseña por teclado (no queda en el historial ni en los logs).
La bandera `-admin` sirve para darle el rol a alguien más desde la terminal.

### Usuarios y roles

Cada usuario es su propio compartimento: sus categorías, sus medios de pago y
sus movimientos. Todas las consultas filtran por el id que sale del JWT, nunca
por uno que venga en la URL — eso es lo que cierra el IDOR de raíz.

Hay dos roles, y son **dos aplicaciones distintas**:

| | `usuario` (cliente) | `admin` (dueño del servidor) |
|---|---|---|
| Sus propias finanzas: resumen, movimientos, categorías, medios | ✅ | — |
| Planes, cobros y el tablero del negocio | — | ✅ |
| Crear clientes, resetear claves, activar/desactivar, eliminar | — | ✅ |
| Ver los datos de un cliente | — | ✅ **solo lectura** |
| Bitácora de errores del servidor | — | ✅ |

El administrador **no lleva gastos personales**: su app es el panel. Las
secciones de dinero solo le aparecen mientras revisa la cuenta de un cliente, y
entonces son de esa persona. No están escondidas: para él esas rutas no existen.

El rol se lee de la base **en cada petición**, no del JWT. Si viajara en el
token, quitárselo a alguien no surtiría efecto hasta que expirara: hasta 24
horas de administrador regalado.

**Desactivar** corta el acceso de inmediato (lo revisa el middleware en cada
petición) y deja el historial intacto: es lo que se quiere casi siempre.
**Eliminar** existe para cuando de verdad hay que borrar, y se lleva por CASCADE
los movimientos, las categorías y los medios, más las facturas del disco — que
el servidor borra aparte, porque hasta ahí no llega ninguna llave foránea. Es
definitivo y exige confirmar el correo exacto de la cuenta.

Para revisar la cuenta de alguien, el panel usa la cabecera `X-Ver-Como`: la app
entera pasa a mostrar los datos de esa persona, con un aviso permanente arriba y
sin un solo botón que escriba. El servidor rechaza cualquier método que no sea
GET mientras la cabecera esté puesta, así que ningún movimiento ajeno puede
aparecer modificado sin que su dueño lo haya hecho.

## Endpoints

Todas las rutas bajo `/api` (menos el login) exigen el header
`Authorization: Bearer <token>`.

| Método | Ruta                             | Descripción                                   |
|--------|----------------------------------|-----------------------------------------------|
| GET    | `/health`                        | Chequeo de vida (sin auth)                    |
| POST   | `/api/auth/login`                | Devuelve el JWT                               |
| GET    | `/api/auth/me`                   | Datos del usuario del token                   |
| POST   | `/api/auth/password`             | Cambiar la contraseña (pide la actual)        |
| GET    | `/api/categorias`                | Lista con el conteo de movimientos de cada una |
| POST   | `/api/categorias`                | Crear                                         |
| PUT    | `/api/categorias/{id}`           | Renombrar                                     |
| DELETE | `/api/categorias/{id}`           | Eliminar (409 si tiene movimientos)           |
| GET    | `/api/medios-pago`               | Lista de medios de pago/recaudo               |
| POST   | `/api/medios-pago`               | Crear                                         |
| PUT    | `/api/medios-pago/{id}`          | Renombrar                                     |
| DELETE | `/api/medios-pago/{id}`          | Eliminar (409 si está en uso)                 |
| GET    | `/api/movimientos`               | Lista paginada con filtros                    |
| POST   | `/api/movimientos`               | Crear                                         |
| GET    | `/api/movimientos/{id}`          | Detalle                                       |
| PUT    | `/api/movimientos/{id}`          | Editar                                        |
| DELETE | `/api/movimientos/{id}`          | Eliminar (borra también su factura del disco) |
| PATCH  | `/api/movimientos/{id}/estado`   | Saldar una deuda o volverla a pendiente (y por dónde se movió) |
| GET    | `/api/movimientos/{id}/abonos`   | Los pagos parciales de una deuda              |
| POST   | `/api/movimientos/{id}/abonos`   | Registrar un abono                            |
| DELETE | `/api/movimientos/{id}/abonos/{abonoID}` | Deshacer un abono                     |
| GET    | `/api/movimientos/{id}/cuotas`   | El acuerdo de pago, con qué cuota está cubierta |
| PUT    | `/api/movimientos/{id}/cuotas`   | Reemplazar el acuerdo entero                  |
| DELETE | `/api/movimientos/{id}/cuotas`   | Borrar el acuerdo                             |
| POST   | `/api/movimientos/{id}/factura`  | Adjuntar factura (multipart, campo `factura`) |
| GET    | `/api/movimientos/{id}/factura`  | Descargar/ver la factura                      |
| DELETE | `/api/movimientos/{id}/factura`  | Quitar la factura                             |
| GET    | `/api/dashboard`                 | Resumen: totales, saldo por medio de pago, por categoría y cuentas con cada quien |
| GET    | `/api/recurrentes`               | Los gastos e ingresos que se repiten          |
| POST   | `/api/recurrentes`               | Crear una plantilla                           |
| PUT    | `/api/recurrentes/{id}`          | Editar o pausar                               |
| DELETE | `/api/recurrentes/{id}`          | Eliminar la plantilla (no los movimientos)    |
| GET    | `/api/recurrentes/pendientes`    | Lo que toca confirmar y lo que viene          |
| POST   | `/api/recurrentes/pendientes/{id}/confirmar` | Crear el movimiento               |
| DELETE | `/api/recurrentes/pendientes/{id}` | Descartar ("este mes no")                   |
| GET    | `/api/notificaciones`            | Tus avisos (resumen semanal, deudas sin mover) |
| POST   | `/api/notificaciones/leidas`     | Marcarlos como leídos                         |
| GET    | `/api/push`                      | Llave pública y dispositivos suscritos        |
| POST   | `/api/push`                      | Recibir avisos en este navegador              |
| DELETE | `/api/push`                      | Dejar de recibirlos (`?endpoint=...`)         |
| GET    | `/api/agente`                    | Tu conversación con el asistente              |
| POST   | `/api/agente/mensajes`           | Escribirle al asistente                       |
| DELETE | `/api/agente`                    | Borrar la conversación y empezar de cero      |
| POST   | `/api/agente/propuestas/{id}/confirmar` | Guardar lo que preparó el asistente    |
| DELETE | `/api/agente/propuestas/{id}`    | Descartar lo que preparó                      |
| GET    | `/api/admin/usuarios`            | *(admin)* Lista de cuentas con rol, estado y conteos |
| POST   | `/api/admin/usuarios`            | *(admin)* Crear una cuenta                    |
| PATCH  | `/api/admin/usuarios/{id}`       | *(admin)* Cambiar nombre, rol o activo        |
| POST   | `/api/admin/usuarios/{id}/password` | *(admin)* Resetear la contraseña           |
| DELETE | `/api/admin/usuarios/{id}`       | *(admin)* Eliminar la cuenta y todos sus datos |
| GET    | `/api/admin/errores`             | *(admin)* Bitácora de errores del servidor    |
| PUT    | `/api/admin/usuarios/{id}/plan`  | *(admin)* Asignar o quitar el plan de un cliente |
| GET    | `/api/admin/planes`              | *(admin)* Planes con su conteo de clientes    |
| POST   | `/api/admin/planes`              | *(admin)* Crear                               |
| PUT    | `/api/admin/planes/{id}`         | *(admin)* Editar nombre, precio y activo      |
| DELETE | `/api/admin/planes/{id}`         | *(admin)* Eliminar (409 si tiene clientes)    |
| GET    | `/api/admin/negocio`             | *(admin)* Tablero del mes: esperado, cobrado, por cobrar |
| GET    | `/api/admin/pagos`               | *(admin)* Cobros de un mes (`?periodo=AAAA-MM`) |
| POST   | `/api/admin/pagos`               | *(admin)* Registrar un cobro                  |
| DELETE | `/api/admin/pagos/{id}`          | *(admin)* Deshacer un cobro                   |

Filtros de `/api/movimientos`: `categoria_id`, `medio_pago_id`, `tipo`
(`recibi`/`pague`/`preste`/`me_prestaron`/`traslado`), `estado`
(`pendiente`/`parcial`/`pagado`), `a_quien` (la contraparte exacta), `desde`,
`hasta` (AAAA-MM-DD), `q` (texto en descripción o persona), `limite` (máx. 200),
`offset`.

Las rutas de `/api/push` **solo existen** si el servidor tiene llaves VAPID
configuradas; si no, responden 404 y la app no ofrece activar los avisos.

### Categorías y medios de pago son dos listas distintas

Responden preguntas diferentes, por eso son dos tablas y dos pantallas:

- **Categoría** (obligatoria) → *¿de qué es esta plata?* — Negocio 1, Personal...
- **Medio de pago** (opcional) → *¿por dónde entró o salió?* — Efectivo,
  Transferencia, Nequi...

Si fueran una sola lista habría que crear "Negocio 1 - efectivo",
"Negocio 1 - transferencia", y se vuelve inmanejable.

Toda cuenta nueva arranca con **Efectivo**, **Transferencia** y **Otro**; el
usuario los renombra, los borra o agrega los suyos. Sin ese sembrado la app
abriría con la lista vacía y no habría por dónde registrar el primer
movimiento. `medio_pago_id` puede ser `null`
("sin registrar"): los movimientos viejos no lo tienen y no siempre se sabe.

### El asistente

Un chat dentro de la app, en la sección **Asistente**. Le preguntas en español
por tus movimientos, tus saldos o quién te debe, y consulta tus datos para
responder: *"¿cuánto llevo gastado este mes?"*, *"¿dónde tengo la plata?"*.
Debajo de cada respuesta dice qué consultó para darla.

También registra: le dices *"pagué 45 mil de almuerzo con Nequi"* y **prepara**
el movimiento en una tarjeta que puedes corregir antes de guardar. Hasta que no
le das a Guardar no se escribe nada — el modelo propone, tú confirmas. Lo mismo
para cobrar un préstamo: *"ya me pagó Juan"*.

Las cifras las suma Postgres, nunca el modelo: sus herramientas solo leen lo
que la app ya calcula, y está hecho para decir "no lo tengo" en vez de
inventarse un número.

Solo existe si el servidor tiene configurada la llave del modelo
(`LLM_API_KEY` en el `.env`): sin ella la app funciona igual y la pantalla lo
avisa. Sirve cualquier API compatible con la de OpenAI — DeepSeek u OpenRouter,
por ejemplo.

Cada usuario tiene su propio hilo y solo ve el suyo. El del administrador es
suyo también: mientras revisa la cuenta de un cliente, la sección desaparece,
porque una conversación no es un registro de dinero sino algo que esa persona
escribió creyendo que era privado.

Hay un tope de mensajes por usuario cada 24 horas (`LLM_LIMITE_DIARIO`, 50 por
omisión): cada mensaje le cuesta plata a quien paga el servidor.

Todo el detalle está en [`docs/agente.md`](docs/agente.md).

### Avisos

En la campana de arriba llegan solos: tu **resumen de cada semana**, los
**préstamos que llevas más de un mes sin cobrar** y, si eres el dueño del
servidor, **a quién te falta cobrarle** este mes.

Las cifras las calcula Postgres. El asistente, cuando está configurado, solo
redacta el párrafo — y si se inventa un número, su versión se descarta y sale
el texto de la app. Sin modelo configurado los avisos funcionan igual.

Detalle en [`docs/avisos.md`](docs/avisos.md).

### Planes y cobros

El negocio se lleva con dos tablas y una regla que vale la pena entender.

**Planes** — nombre y precio mensual. Se asignan a cada cliente desde el panel.
Un plan con clientes no se puede borrar (409): o los mueves a otro, o lo
**desactivas**, que lo saca del catálogo sin tocar a los que ya lo tienen.

**Pagos** — un renglón por cliente y mes. Un índice único `(usuario_id, periodo)`
impide cobrar dos veces el mismo mes: un doble clic no puede inflar los ingresos.

El monto y el nombre del plan se **copian** en el pago en vez de referenciarlos.
Si mañana le subes el precio al plan Pro, los cobros viejos tienen que seguir
diciendo lo que de verdad se cobró ese mes; con un JOIN al plan actual el
histórico se reescribiría solo cada vez que cambias una tarifa. Por lo mismo,
borrar a un cliente **no borra sus pagos** (`ON DELETE SET NULL`): un cliente se
va, pero los ingresos de marzo siguen siendo los ingresos de marzo.

El tablero muestra tres cifras por mes:

```
Esperado  = suma de los planes de todos los clientes activos que tienen uno
Cobrado   = suma de los pagos registrados en ese mes
Por cobrar= suma de los planes de los clientes activos SIN pago ese mes
```

"Por cobrar" **no** es `esperado − cobrado`. Si alguien te paga de más, o pagas
un mes atrasado dentro de este periodo, esa resta daría un pendiente negativo o
escondería a quien sí debe. Calculado aparte, la cifra responde a la pregunta
real: *¿a quién me falta cobrarle?*

### Formato de los datos

- **Montos**: siempre string (`"150000.50"`). La columna es `NUMERIC(14,2)` y
  todas las sumas las hace Postgres — nunca se usa punto flotante para dinero.
  El formulario los muestra con separadores de miles (`1.500.000,50`) y los
  convierte al formato crudo antes de enviarlos.
- **Fechas**: `AAAA-MM-DD`.
- **`a_quien` y `estado`**: solo existen cuando `tipo` es `preste`; en los demás
  llegan como `null`. Lo garantiza un CHECK en la base de datos.
- **Facturas**: JPG, PNG, WEBP, HEIC o PDF, máximo 10 MB. El tipo se detecta
  leyendo el archivo, no por su extensión.

Formato de error uniforme:

```json
{ "error": "Datos invalidos", "campos": { "monto": "el monto debe ser mayor que cero" } }
```

### Cómo cuenta una deuda

Prestar y que te devuelvan son dos movimientos de plata que **se anulan**:
salieron $200.000 y volvieron $200.000. Neto: cero. Con pagos parciales la idea
es la misma, solo que a pedazos, y por eso todo se calcula sobre el **saldo**:

```
saldo = monto − suma de sus abonos
```

| Tipo | Qué pasa con el saldo | Efecto en el balance |
|---|---|---|
| `preste` | Es plata tuya que está afuera | **Resta** |
| `me_prestaron` | Es plata ajena que tienes | **Suma** |

```
balance = recibido − pagado − por_cobrar + por_pagar
```

Que lo que te prestaron *sume* sorprende al principio, pero es lo correcto: esa
plata la tienes en el bolsillo, aunque la debas. Cada abono mueve el balance en
sentido contrario, porque la plata regresa o se va. Lo que *no* se hace es
sumarla además a "recibido", porque entonces los mismos pesos se contarían dos
veces.

Una deuda saldada tiene saldo cero y por lo tanto no mueve nada, sin necesidad
de mirar ningún flag. "Recuperado" y "abonado" se muestran aparte, solo
informativos.

### El estado no es la verdad

`estado` (`pendiente` / `parcial` / `pagado`) es un **resumen para filtrar**,
no el dato. La verdad son los abonos, y el estado se recalcula a partir de
ellos en la misma transacción cada vez que uno entra o sale. Con una sola
puerta, no puede contradecir a la tabla.

Por eso "marcar pagado" no escribe un flag: **registra el abono que faltaba**.
Y volver a pendiente borra los abonos — es la única acción destructiva de la
lista, y por eso pregunta.

### Abonar por medios distintos

Prestas en efectivo y te pueden devolver la mitad por transferencia y el resto
en efectivo. Cada **abono** guarda su propio medio, que es la única forma de
que eso quede bien en los saldos. Una sola columna en el movimiento no podría
contarlo.

### El acuerdo de pago

Una deuda puede tener cuotas con su fecha. Las cuotas son el **calendario, no
la plata**: lo que se debe sigue siendo el saldo. Los abonos las van cubriendo
en orden, y el día que una vence sin estar cubierta, llega un aviso.

Se guardan una por una (y no como "6 cuotas cada 30 días") porque un acuerdo
real se corre: la tercera se pasa para el 15 y las demás siguen igual. Con una
regla calculada no habría dónde anotar esa excepción.

El reparto lo hace el servidor en **centavos enteros**, nunca con float: las
partes suman siempre el total exacto, y los centavos que sobran van en las
primeras cuotas.

### Traslados: la misma plata, otro bolsillo

Pasar del efectivo a la cuenta no es ingreso ni gasto. Es **un solo
movimiento** de tipo `traslado`, con origen (`medio_pago_id`) y destino
(`medio_cobro_id`).

Con dos movimientos ("pagué" en uno y "recibí" en el otro) los totales se
inflarían con plata que nunca entró ni salió, y habría que mantener las dos
filas sincronizadas al editar o borrar. Así, el balance general no se mueve y
los dos saldos por medio sí.

### ¿Dónde está la plata?

El resumen muestra cuánto **tienes** en cada medio de pago:

```
Recibí = ingresos + lo que te prestaron + abonos que te hicieron + traslados que entraron
Pagué  = gastos   + lo que prestaste    + abonos que hiciste     + traslados que salieron
Tengo  = Recibí − Pagué
```

Ahí no aparece "por cobrar" a propósito: un préstamo con saldo no está en
ningún medio, está con la persona que se lo llevó.

Incluye una fila **"Sin registrar"** con los movimientos que no tienen medio.
No es decorativa: sin ella los saldos no sumarían el balance general y no habría
forma de cuadrar los números.

En la lista, un préstamo saldado se muestra en **+ y verde** (la plata volvió)
y uno con saldo en **− y ámbar** (la plata está afuera). "Me prestaron" es al
revés: **+ ámbar** mientras la debes (entró, pero no es tuya) y **− rojo** una
vez pagada. Un traslado va en gris: no suma ni resta.

### Cuentas con cada quien

El resumen agrupa las deudas por persona o negocio y muestra cuánto te debe
cada quien, cuánto le debes y el **neto**. Se agrupa por el nombre normalizado
(sin mayúsculas ni espacios de sobra): "Carlos", "carlos" y " Carlos " son la
misma persona, porque si no el saldo quedaría partido y ninguna mitad sería
cierta.

Cuando hay deuda en los dos sentidos se muestran las dos cifras y el neto:
decir solo "te debe 50" cuando además le debes 200 sería cierto y engañoso a la
vez.

## Comandos útiles

```bash
docker compose logs -f backend      # ver logs de la API
docker compose restart backend      # reiniciar solo la API
docker compose down                 # apagar (los datos sobreviven en el volumen)
docker compose down -v              # apagar Y BORRAR la base de datos
docker compose exec db psql -U finanzas -d finanzas   # consola de Postgres
```

Si agregas una dependencia al frontend (`package.json`), hay que renovar el
volumen de `node_modules`; si no, el contenedor sigue usando el viejo:

```bash
docker compose up -d --build --renew-anon-volumes frontend
```

Para empezar de cero (borra TODO: base de datos y facturas):

```bash
docker compose down -v
rm -rf backend/uploads/2*
docker compose up -d --build
docker compose exec backend /app/createuser -email tu@correo.com -nombre "Tu Nombre"
```

(El usuario que se crea así queda como administrador: es el único que puede
abrir el panel y crear a los demás.)

Para trabajar el backend sin Docker (compila más rápido mientras desarrollas):

```bash
docker compose up -d db
cd backend
DATABASE_URL="postgres://finanzas:<clave>@localhost:5436/finanzas?sslmode=disable" go run ./cmd/api
```

## Desplegar en la Raspberry Pi 4B

1. Instalar Docker en la Pi y clonar el repo.
2. Crear el `.env` con valores de producción (`APP_ENV=production`, secreto JWT nuevo,
   `CORS_ORIGINS` y `VITE_API_URL` con la IP o dominio real de la Pi).
3. En `docker-compose.yml`, cambiar el servicio `frontend` a `target: prod`
   (nginx sirviendo el build estático, sin Node en la imagen final: ~74 MB en vez de ~290 MB).
4. Quitar el `ports: 5433:5432` de `db` para que Postgres no quede expuesto.
5. `docker compose up -d --build`

Las imágenes se construyen en la propia Pi, así que salen en arm64 sin configuración extra.

### Respaldos

```bash
# base de datos
docker compose exec db pg_dump -U finanzas finanzas > respaldo.sql
# facturas
tar czf facturas.tar.gz backend/uploads/
```

## Estado

- [x] Login con JWT, middleware de rutas protegidas
- [x] Categorías (CRUD)
- [x] Movimientos (recibí / pagué / presté / me prestaron / traslado) + adjuntar factura
- [x] Dashboard (resumen por categoría, total prestado pendiente)
- [x] Marcar deuda saldada desde la lista (sin abrir el formulario)
- [x] Traslados entre medios de pago (no cuentan como ingreso ni gasto)
- [x] Deudas propias: lo que **tú** debes, con las mismas reglas
- [x] Pagos parciales (abonos) y acuerdos de pago por cuotas
- [x] Cuentas por persona o negocio, con el neto en los dos sentidos
- [x] Gastos e ingresos que se repiten: la app los propone y tú confirmas
- [x] Recordatorio de cobro por WhatsApp (arma el mensaje, tú decides si lo mandas)
- [x] Interfaz adaptada a teléfono
- [x] Medios de pago/recaudo (CRUD propio + campo en el movimiento)
- [x] Saldo por medio de pago en el resumen ("¿dónde está la plata?")
- [x] Registro rápido desde el resumen y campos obligatorios
- [x] Modo claro / oscuro (recuerda la preferencia)
- [x] Cambio de contraseña desde la app
- [x] Varios usuarios, cada uno con sus propios datos
- [x] Panel de administración: crear usuarios, resetear claves, activar/desactivar, eliminar
- [x] Ver la cuenta de otro usuario en modo lectura
- [x] Bitácora de errores dentro de la app (sin curl)
- [x] Planes de suscripción con precio mensual
- [x] Cobros por cliente y mes, con tablero de esperado / cobrado / por cobrar
- [x] Asistente: chat dentro de la app
- [x] Asistente: preguntarle por tus movimientos y saldos
- [x] Asistente: registrar movimientos hablando (con confirmación)
- [x] Asistente: resúmenes y avisos automáticos
- [x] Asistente: registrar abonos y reconocer con quién es cada deuda
- [x] Avisos al celular con la app cerrada (Web Push)
- [x] Tests automáticos del dominio (dinero, auth, movimientos, agente, avisos, push, recurrentes)
- [ ] Ajustes finales de despliegue

## Lo que falta

Lo pendiente de verdad, para no tener que reconstruirlo de memoria en un mes:

**Por hacer**

- **Probar el asistente contra el proveedor real.** Todo se desarrolló y se
  probó con un modelo de mentiras (un servidor local que responde el formato de
  DeepSeek). El contrato está cubierto por pruebas, pero nadie ha visto todavía
  cómo se comporta el modelo de verdad: si elige bien las herramientas, si
  pregunta cuando le falta la categoría, cuánto tarda.
- **Arreglar `TestRutaSeguraBloqueaSalidas`** (`internal/movimientos`). Una ruta
  de factura con barras invertidas (`..\..\windows\system.ini`) no se
  bloquea. En Linux no es explotable —esa cadena es un nombre de archivo
  válido, no un salto de directorio—, pero la prueba dice lo que debería pasar
  y hoy no pasa.
- **Ajustes de despliegue**: `target: prod` en el frontend, quitar el puerto
  expuesto de Postgres y un secreto JWT nuevo.

**Decidido dejar afuera (por ahora)**

- El asistente no edita ni borra movimientos: solo consulta y propone crear.
- No lee facturas: mandarle la foto de un recibo para que saque el monto sería
  el siguiente paso natural.
- El chat no va apareciendo palabra por palabra (sin streaming): la respuesta
  llega completa.
- Entre un mensaje y otro el modelo no recuerda lo que consultó; si hace falta,
  vuelve a consultarlo.
- Los avisos solo llegan dentro de la app. Nada de correo ni notificaciones al
  teléfono.
