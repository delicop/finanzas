# API

Base: `http://localhost:8080` (o la IP de la Raspberry).

Todas las rutas bajo `/api` exigen `Authorization: Bearer <token>`, menos el
login. Las de `/api/mantenimiento` usan un token distinto — ver
[errores.md](errores.md).

## Autenticación

| Método | Ruta | Descripción |
|---|---|---|
| POST | `/api/auth/login` | Devuelve el JWT |
| GET | `/api/auth/me` | Datos del usuario del token |
| POST | `/api/auth/password` | Cambiar contraseña (pide la actual) |

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"tu@correo.com","password":"tu-clave"}'
```

```json
{ "token": "eyJ...", "expira_en": "2026-09-17T03:32:07Z",
  "usuario": { "id": 1, "email": "tu@correo.com", "nombre": "Tu Nombre", "rol": "admin" } }
```

Una cuenta desactivada responde **403** al login, con el motivo explícito: ya
demostró ser dueña del correo, así que no se le está revelando nada nuevo y se
ahorra creer que olvidó la contraseña.

## Administración

Solo para `rol: "admin"`. Cualquier otro recibe **403** en todas estas rutas.
El rol se lee de la base en cada petición, no del JWT: si no, quitárselo a
alguien no surtiría efecto hasta que su token expirara (hasta 24 horas).

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/api/admin/usuarios` | Lista con rol, estado y cuántos datos tiene cada uno |
| POST | `/api/admin/usuarios` | Crear (`email`, `nombre`, `password`, `rol`) |
| PATCH | `/api/admin/usuarios/{id}` | Cambiar `nombre`, `rol` o `activo` (solo lo que mandes) |
| POST | `/api/admin/usuarios/{id}/password` | Resetear la contraseña (`nueva`) |
| DELETE | `/api/admin/usuarios/{id}?email=<correo>` | Eliminar la cuenta y todos sus datos |
| GET | `/api/admin/errores` | La bitácora del servidor, sin el token de mantenimiento |

### Eliminar una cuenta

Se lleva por delante los movimientos, las categorías, los medios de pago y las
**facturas del disco**. No hay papelera.

Las filas las borra el `ON DELETE CASCADE`, pero a los archivos no llega
ninguna llave foránea: el servidor lee las rutas de las facturas **antes** de
borrar las filas y las elimina después. Sin eso, cada cliente eliminado dejaría
sus archivos ocupando la tarjeta de la Raspberry para siempre, sin una sola fila
que dijera de quién eran.

El `email` no es opcional: tiene que coincidir exacto con el de la cuenta. Un id
en una URL se equivoca fácil — se borra el 3 creyendo que era el 2 y se va el
historial de otro cliente — y escribir el correo completo obliga a mirar a quién
se está borrando. Si no coincide, **400**.

Para cortarle el acceso a alguien sin destruir su historial está
`PATCH {"activo": false}`, que es casi siempre lo que de verdad se quiere:
surte efecto en la siguiente petición, porque el middleware lo revisa en todas.

Cuatro reglas que el servidor no deja romper:

- No puedes desactivar tu propia cuenta (409).
- No puedes eliminar tu propia cuenta (409).
- No puedes quitarte a ti mismo el rol de admin (409).
- Nunca puede quedar el servidor sin ningún administrador activo (409). Esto
  último se verifica dentro de una transacción con candado sobre las filas de
  los admins: sin él, dos peticiones simultáneas que degradan a dos admins
  distintos verían cada una que "todavía queda el otro" y pasarían las dos.

### Planes y cobros

El lado negocio. Estas tablas no llevan `usuario_id`: no son de nadie, son del
servidor. Lo que las protege no es un filtro sino `RequireAdmin`.

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/api/admin/planes` | Planes con su conteo de clientes |
| POST | `/api/admin/planes` | Crear (`nombre`, `precio_mensual`, `precio_anual`, `incluye_ia`) |
| PUT | `/api/admin/planes/{id}` | Editar todo lo anterior y `activo` |
| DELETE | `/api/admin/planes/{id}` | Eliminar — **409** si tiene clientes |
| PUT | `/api/admin/usuarios/{id}/plan` | Asignar el plan y cómo lo paga (`{"plan_id": 3, "ciclo": "anual"}`) o quitarlo (`null`) |
| GET | `/api/admin/negocio?periodo=AAAA-MM` | Tablero del mes |
| GET | `/api/admin/pagos?periodo=AAAA-MM` | Cobros de ese mes |
| POST | `/api/admin/pagos` | Registrar un cobro |
| DELETE | `/api/admin/pagos/{id}` | Deshacerlo |

Los precios y montos son **string**, igual que en el resto de la app: columna
`NUMERIC(14,2)` y ninguna suma en Go.

#### Qué trae un plan

| Campo | Qué es |
|---|---|
| `precio_mensual` | Obligatorio. |
| `precio_anual` | Opcional. Vacío = el plan **no se vende por año**. |
| `incluye_ia` | Si sus clientes pueden usar el asistente. Por omisión, `false`. |

**Sin IA en el plan no hay asistente**, y **sin plan tampoco**: cada mensaje le
cuesta plata al dueño del servidor. Se revisa en el backend en cada petición a
`/api/agente`, así que quitarle la IA a un plan corta el chat desde el mensaje
siguiente, sin esperar a que venza la sesión (responde **403**). `/api/auth/me`
trae `"ia": true|false` para que la app esconda el botón, pero quien lo impide
es el servidor.

Los avisos automáticos le llegan a todos igual; el modelo solo los *redacta*
para quien tiene IA en el plan. Los demás reciben el texto que arma la app, con
las mismas cifras.

#### Mensual o anual

El ciclo es **del cliente**, no del plan: el mismo plan lo puede pagar uno por
mes y otro por año (`usuarios.ciclo_pago`). Solo se acepta `"anual"` si el plan
tiene precio anual (**422** en `campos.ciclo` si no). Quitarle el precio anual a
un plan que alguien paga por año también se rechaza (**422** en
`campos.precio_anual`): primero hay que pasar a ese cliente a mensual.

Un pago **cubre un rango** de meses: `[periodo, cubre_hasta)`. Uno mensual cubre
ese mes; uno anual, ese mes y los once siguientes. El monto por omisión sale del
ciclo del cliente: el precio mensual o el anual.

`periodo` siempre es `AAAA-MM` y se normaliza al día 1. Que dos pagos del mismo
cliente no cubran el mismo mes lo garantiza la base con una **restricción de
exclusión** sobre rangos (`pagos_sin_solapar`, con la extensión `btree_gist`):
ni un doble clic ni un pago mensual metido en medio de un año pagado pueden
inflar los ingresos.

```bash
# El cobro normal: el monto sale del plan, no del cuerpo de la petición.
curl -X POST http://localhost:8080/api/admin/pagos \
  -H "Authorization: Bearer $TOKEN_DEL_ADMIN" \
  -H "Content-Type: application/json" \
  -d '{"usuario_id": 2, "periodo": "2026-09"}'
```

`monto` es opcional y solo se manda para cobrar algo distinto del precio de
lista. Si viniera siempre del formulario, un dedazo registraría que alguien pagó
$1 y los ingresos quedarían mal para siempre.

Respuestas que conviene esperar:

- **409** `"Ese cliente ya tiene cubierto ese periodo"` — un pago de ese mes, o uno anual que lo incluye.
- **409** `"Ese cliente no tiene plan asignado"` — no hay nada que cobrarle.
- **422** con `campos.periodo` si el periodo no es `AAAA-MM`.

El tablero de `/api/admin/negocio` devuelve:

| Campo | Qué es |
|---|---|
| `esperado` | **Ingreso mensual recurrente.** Un cliente anual aporta su precio anual ÷ 12. |
| `cobrado` | Lo que entró en los pagos registrados ese mes. Un pago anual cuenta entero en el mes en que se hizo. |
| `pendiente` | Lo que les toca pagar a los clientes que **no tienen el mes cubierto**, según su ciclo (el año entero si pagan por año). |
| `clientes_pagaron` | Cuántos tienen el mes cubierto ("al día"). |
| `clientes_anuales` | Cuántos pagan por año. |

Ojo: **`pendiente` no es `esperado − cobrado`**, y con clientes anuales esa
resta no tiene ningún sentido: un mes puede cobrar un año entero y el siguiente
nada. Cada cifra se calcula por su lado.

### Ver los datos de otro usuario

Cabecera `X-Ver-Como: <id>` en cualquier ruta privada. El backend responde con
los datos de ese usuario en vez de los tuyos, sin que los handlers se enteren.

Es, literalmente, saltarse el filtro por `usuario_id` que impide el IDOR, así
que está cerrado con tres llaves: solo un **admin**, solo en peticiones **GET**,
y el observado tiene que existir. Un `POST`/`PUT`/`PATCH`/`DELETE` con la
cabecera puesta responde 403 — el admin mira las cuentas ajenas, no las edita.

```bash
curl http://localhost:8080/api/dashboard \
  -H "Authorization: Bearer $TOKEN_DEL_ADMIN" \
  -H "X-Ver-Como: 2"
```

## Categorías y medios de pago

Son **dos listas distintas** porque responden preguntas distintas:

- **Categoría** (obligatoria) → *¿de qué es esta plata?* — Negocio 1, Personal
- **Medio de pago** (obligatorio) → *¿por dónde entró o salió?* — Efectivo, Nequi

Si fueran una sola habría que crear "Negocio 1 - efectivo", "Negocio 1 -
transferencia"… y se vuelve inmanejable.

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/api/categorias` | Lista con el conteo de movimientos de cada una |
| POST | `/api/categorias` | Crear |
| PUT | `/api/categorias/{id}` | Renombrar |
| DELETE | `/api/categorias/{id}` | Eliminar (409 si tiene movimientos) |
| GET | `/api/medios-pago` | Lista |
| POST | `/api/medios-pago` | Crear |
| PUT | `/api/medios-pago/{id}` | Renombrar |
| DELETE | `/api/medios-pago/{id}` | Eliminar (409 si está en uso) |

## Movimientos

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/api/movimientos` | Lista paginada con filtros |
| GET | `/api/movimientos/exportar` | Excel o PDF de un rango de fechas |
| POST | `/api/movimientos` | Crear |
| GET | `/api/movimientos/{id}` | Detalle |
| PUT | `/api/movimientos/{id}` | Editar |
| DELETE | `/api/movimientos/{id}` | Eliminar (borra también su factura) |
| PATCH | `/api/movimientos/{id}/estado` | Marcar préstamo pagado/pendiente |
| POST | `/api/movimientos/{id}/factura` | Adjuntar (multipart, campo `factura`) |
| GET | `/api/movimientos/{id}/factura` | Ver o descargar |
| DELETE | `/api/movimientos/{id}/factura` | Quitar |

**Filtros del listado:** `categoria_id`, `medio_pago_id`, `tipo`
(`recibi`/`pague`/`preste`), `estado` (`pendiente`/`pagado`), `desde`, `hasta`
(AAAA-MM-DD), `q` (busca en descripción y en la persona), `limite` (máx. 200),
`offset`.

**Campos obligatorios al crear:** `categoria_id`, `medio_pago_id`, `tipo`,
`monto`, `fecha` (y `a_quien` + `estado` si el tipo es `preste`).
Opcionales: `descripcion`, la factura y, solo para `preste`, `cobrar_el`
(AAAA-MM-DD, la fecha de pago acordada; no puede ser antes de `fecha`). Ese
día llega un aviso `cobro_del_dia`.

```bash
curl -X POST http://localhost:8080/api/movimientos \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"categoria_id":1,"medio_pago_id":2,"tipo":"preste","monto":"200000",
       "fecha":"2026-09-16","descripcion":"Préstamo",
       "a_quien":"Carlos","estado":"pendiente","cobrar_el":"2026-09-30"}'
```

Marcar que ya pagaron, indicando por dónde:

```bash
curl -X PATCH http://localhost:8080/api/movimientos/7/estado \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"estado":"pagado","medio_cobro_id":3}'
```

### Exportar a Excel o PDF

`GET /api/movimientos/exportar?formato=xlsx&desde=2026-09-01&hasta=2026-09-30`

- `formato`: `xlsx` o `pdf`. `desde` y `hasta` son **obligatorios** (máximo
  cinco años de rango).
- Acepta los mismos filtros del listado (`tipo`, `categoria_id`,
  `medio_pago_id`, `estado`, `q`): se exporta lo que se está viendo.
- Responde el archivo con `Content-Disposition: attachment` y
  `Cache-Control: no-store`. Trae arriba el resumen del rango (recibido,
  pagado, por cobrar, recuperado y balance, sumados por Postgres) y abajo los
  movimientos en orden cronológico.
- Más de 5.000 movimientos en el rango → 422, "escoge un rango más corto".

```bash
curl -o septiembre.pdf -H "Authorization: Bearer $TOKEN"   "http://localhost:8080/api/movimientos/exportar?formato=pdf&desde=2026-09-01&hasta=2026-09-30"
```

En el Excel los montos son números (se pueden sumar y filtrar) y las
descripciones siempre texto: una que empiece por `=` no se ejecuta como
fórmula.

## Resumen

`GET /api/dashboard` devuelve:

- `totales` — recibido, pagado, por cobrar, recuperado y balance
- `medios` — cuánto hay en cada medio de pago (**¿dónde está la plata?**)
- `categorias` — el desglose por categoría
- `deudores` — quién debe cuánto, con `proximo_cobro` (la fecha de pago más
  cercana de sus préstamos pendientes, o `null`)

En `medios` no aparece "por cobrar": un préstamo pendiente no está en ningún
medio, está con la persona. La suma de los saldos da exactamente el balance
general, y hay una fila **"Sin registrar"** para los movimientos sin medio —
sin ella los números no cuadrarían.

## Avisos

| Método | Ruta | Qué hace |
|---|---|---|
| GET | `/api/notificaciones` | Tus avisos (máx. 30) y cuántos sin leer |
| POST | `/api/notificaciones/leidas` | Marca todos como leídos (204) |

```json
{ "avisos": [
    { "id": 4, "tipo": "resumen_semanal",
      "titulo": "Tu semana: pagaste $45.000",
      "cuerpo": "Del 7 al 13 de septiembre registraste 2 movimientos...",
      "leida_en": null, "creada_en": "2026-09-14T08:00:00Z" }
  ],
  "sin_leer": 1 }
```

`tipo` es `resumen_semanal`, `cobro_del_dia`, `prestamos_pendientes` o
`cobros_del_mes` (este último solo le llega al administrador). Los genera una
tarea del servidor cada hora; ver [avisos.md](avisos.md).

Igual que el chat, son privados: en modo "ver como" responden 403.

## Asistente

Estas rutas **solo existen si el servidor tiene configurada la llave del
modelo** (`LLM_API_KEY`); si no, responden 404. Ver
[agente.md](agente.md).

| Método | Ruta | Qué hace |
|---|---|---|
| GET | `/api/agente` | Tu conversación con sus mensajes |
| POST | `/api/agente/mensajes` | Escribirle al asistente |
| DELETE | `/api/agente` | Borrar la conversación abierta y las guardadas (204) |
| POST | `/api/agente/terminar` | Archivar la conversación abierta → `{"guardada": true}` |
| GET | `/api/agente/guardadas` | Conversaciones terminadas, la más reciente primero |
| GET | `/api/agente/guardadas/{id}` | Una guardada con sus mensajes en `hilo` |
| DELETE | `/api/agente/guardadas/{id}` | Borrar una guardada (204; 404 si no es tuya o está abierta) |
| POST | `/api/agente/propuestas/{id}/confirmar` | Ejecuta lo que el agente preparó |
| DELETE | `/api/agente/propuestas/{id}` | Descarta la propuesta (204) |

`GET /api/agente` devuelve el hilo abierto. Nunca 404 por no tener
conversación: la primera visita es un hilo vacío, que es un estado normal y no
un error.

```json
{
  "id": 12,
  "mensajes": [
    { "id": 30, "rol": "usuario", "contenido": "¿cómo registro un préstamo?",
      "creado_en": "2026-09-16T14:02:11Z" },
    { "id": 31, "rol": "agente", "contenido": "En Movimientos...",
      "creado_en": "2026-09-16T14:02:14Z", "herramientas": ["resumen"] }
  ],
  "restantes": 47
}
```

`POST /api/agente/mensajes` recibe `{"texto": "..."}` (máx. 2000 caracteres) y
devuelve la respuesta ya guardada:

```json
{ "conversacion_id": 12,
  "mensaje": { "id": 31, "rol": "agente", "contenido": "En Movimientos...",
               "creado_en": "2026-09-16T14:02:14Z" },
  "restantes": 46 }
```

`herramientas` son las que el agente consultó para armar esa respuesta
(`resumen`, `listar_movimientos`, `listar_categorias`, `listar_medios_pago`).
Solo aparece en los mensajes del agente que consultaron algo; la app lo muestra
debajo del mensaje.

`restantes` son los mensajes que le quedan al usuario en las próximas 24 horas.
Viene en las dos respuestas para que la app pueda avisar **antes** de que el
usuario se choque con el 429.

No hay ningún id de conversación en la entrada, a propósito: el hilo es siempre
el de quien tiene la sesión. Así no existe un número que se pueda cambiar a
mano para leer el chat de otro.

### Propuestas

Cuando el agente prepara un movimiento, la respuesta trae una propuesta
**pendiente**: en la base del dinero todavía no ha pasado nada.

```json
{ "conversacion_id": 12,
  "mensaje": { "...": "..." },
  "propuestas": [
    { "id": 8, "tipo": "movimiento", "creada_en": "2026-09-16T14:02:14Z",
      "datos": { "tipo": "pague", "monto": "45000", "fecha": "2026-09-16",
                 "descripcion": "almuerzo",
                 "categoria_id": 3, "categoria": "Negocio 1",
                 "medio_pago_id": 2, "medio_pago": "Efectivo" } }
  ],
  "restantes": 46 }
```

`GET /api/agente` devuelve en `propuestas` las pendientes y no caducadas, para
que una tarjeta sobreviva a recargar la página.

`POST /api/agente/propuestas/{id}/confirmar` recibe los datos **finales** —los
que el usuario tenga en la tarjeta, que puede haber editado— y responde 201 con
el movimiento creado:

- tipo `movimiento`: mismos campos que `POST /api/movimientos`, mismas
  validaciones, mismos errores por campo.
- tipo `marcar_pagado`: `{"medio_cobro_id": 2}` (0 = sin registrar). Responde
  200 con el préstamo ya cobrado. Cuál préstamo es lo dice la propuesta, no el
  cuerpo.

Una propuesta caduca a las 24 horas, y solo se puede resolver una vez: la
segunda confirmación responde 409 sin crear nada.

Códigos propios de estas rutas:

| Código | Significa |
|---|---|
| 403 | Se intentó leer el chat en modo "ver como": es privado |
| 409 | La propuesta ya se resolvió o caducó |
| 429 | Se acabaron los mensajes de las últimas 24 horas |
| 503 | El proveedor del modelo no respondió — se puede reintentar |

## Formato de los datos

- **Montos**: siempre string (`"150000.50"`). Columna `NUMERIC(14,2)`; todas
  las sumas las hace Postgres. Nunca punto flotante para dinero.
- **Fechas**: `AAAA-MM-DD`.
- **`a_quien` y `estado`**: solo existen cuando `tipo` es `preste`; en los
  demás llegan como `null`. Lo garantiza un CHECK en la base de datos.
- **Facturas**: JPG, PNG, WEBP, HEIC o PDF, máximo 10 MB. El tipo se detecta
  leyendo el archivo, no por su extensión.

## Errores

Formato único:

```json
{ "error": "Datos inválidos",
  "campos": { "monto": "El monto debe ser mayor que cero" } }
```

| Código | Significa |
|---|---|
| 400 | El cuerpo no se pudo leer |
| 401 | Falta el token, está vencido o las credenciales no sirven |
| 404 | No existe (o no es tuyo) |
| 409 | Válido, pero choca con el estado actual (borrar algo en uso) |
| 422 | Datos inválidos — revisa `campos` |
| 429 | Demasiados intentos de login |
| 500 | Falla del servidor — queda en la [bitácora](errores.md) |
