# Decisiones técnicas

Por qué las cosas están hechas como están. Si algo parece raro, probablemente
la respuesta esté aquí.

## El dinero nunca es un float

La columna es `NUMERIC(14,2)`, en la API viaja como **string** y **todas las
sumas las hace Postgres**. Go no suma un solo peso.

En binario 0.1 no se puede representar exacto: `0.1 + 0.2` da
`0.30000000000000004`. Después de unos cientos de movimientos los totales
dejan de cuadrar, y en una app de plata eso es lo peor que puede pasar.

El frontend convierte a número solo para pintar.

## Cómo cuenta un préstamo

Prestar y que te devuelvan son dos movimientos que **se anulan**: salieron
$200.000 y volvieron $200.000. Neto: cero.

| Estado | ¿Cuenta en "por cobrar"? | Efecto en el balance |
|---|---|---|
| `pendiente` | Sí | Resta el monto |
| `pagado` | No | Cero (salió y volvió) |

```
balance = recibido − pagado − por_cobrar
```

Al marcar un préstamo como pagado **el balance sube ese monto**: es plata que
volvió a tu bolsillo. Lo que *no* se hace es sumarlo además a "recibido",
porque entonces los mismos $200.000 se contarían dos veces.

Un préstamo guarda **dos** medios de pago: `medio_pago_id` (por dónde salió) y
`medio_cobro_id` (por dónde volvió). Prestas en efectivo y te pueden pagar por
transferencia; sin los dos datos, el saldo de cada medio quedaría mal.

## Las reglas viven también en la base de datos

```sql
CHECK ((tipo = 'preste'  AND a_quien IS NOT NULL AND estado IS NOT NULL)
    OR (tipo <> 'preste' AND a_quien IS NULL     AND estado IS NULL))
```

Validar en Go no basta: un bug futuro o un `UPDATE` manual por psql podría
dejar un préstamo sin dueño. Con el CHECK, Postgres lo rechaza siempre.

Lo mismo con `monto > 0` y con que `medio_cobro_id` solo exista en préstamos
ya cobrados.

## Borrar está restringido a propósito

Categorías y medios de pago usan `ON DELETE RESTRICT`. Borrar uno que tenga
movimientos **falla** y devuelve un 409 con un mensaje claro.

Un `CASCADE` que se lleva por delante registros de dinero es un desastre
silencioso.

## Todas las consultas filtran por `usuario_id`

Si el id viniera de la URL, cualquiera con un token válido podría leer o borrar
datos ajenos — eso es un **IDOR**. Filtrar por el id que salió del JWT lo cierra
de raíz. Se hizo así desde el primer día, cuando todavía había un solo usuario,
y por eso abrir la app a varios no costó reescribir el dominio.

## Crear valida las referencias en la misma consulta

```sql
WITH cat AS (SELECT id FROM categorias WHERE id = $2 AND usuario_id = $1),
     ins AS (INSERT INTO movimientos (...) SELECT ... FROM cat RETURNING *)
SELECT ... FROM ins
```

Si la categoría no es del usuario, el SELECT no devuelve filas y no se inserta
nada. Con dos consultas separadas habría una ventana entre "verifico" e
"inserto" en la que la categoría podría borrarse.

Cuando el medio de pago es inválido, la consulta **falla** en vez de guardar
NULL en silencio: así el usuario no cree que quedó registrado algo que no.

## JWT: el detalle que importa

El payload de un JWT **no está cifrado** — cualquiera lo lee con base64. Por
eso ahí solo va el id del usuario y la expiración. Lo que garantiza es
*integridad*.

```go
jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()})
```

Sin eso existe el ataque de *alg confusion*: alguien manda un token con
`alg: none` (sin firma) y una librería mal configurada lo acepta. Hay un test
que lo comprueba (`TestRechazaAlgoritmoNone`).

No hay refresh tokens: con un solo usuario, un token de 24h y volver a entrar
es suficiente.

## bcrypt costo 12

Cada +1 duplica el tiempo de cómputo. En la Raspberry Pi 4 tarda ~300-500 ms
por login, aceptable porque hay un login por sesión. Si se siente lento, bajar
a 11 sigue siendo seguro.

## Mismo mensaje para correo inexistente y clave mala

Decir "ese correo no existe" le confirma a un atacante cuáles de los correos
que probó son cuentas reales del servidor.

La excepción es la cuenta desactivada: ahí sí se dice el motivo, pero solo
**después** de verificar la contraseña. Quien llega a ese punto ya demostró ser
el dueño del correo, así que no se le revela nada que no sepa, y se ahorra creer
que olvidó la clave.

## Límite de intentos de login

10 fallos por IP cada 15 minutos. La app queda expuesta en internet; sin freno,
un script prueba miles de claves por minuto. Son ~50 líneas en memoria, sin
Redis.

El freno es **por IP**, no por cuenta. Cuando había un solo usuario daba igual;
ahora que hay varios, un atacante con muchas IPs podría repartir los intentos
entre ellas. Sumar un contador por email cerraría ese hueco y está pendiente
(ver el estado en el README).

## Facturas

- **El tipo se detecta leyendo los primeros bytes**, no por la extensión ni
  por el `Content-Type`: ambos los controla el cliente y se falsifican en un
  segundo. Un `.txt` renombrado a `.png` se rechaza.
- **El nombre en disco es aleatorio.** Con el nombre original, un archivo
  llamado `../../algo` escribiría fuera de `/uploads`, y dos facturas con el
  mismo nombre se pisarían.
- **Se guardan por año/mes**, para no terminar con miles de archivos sueltos
  en una carpeta.
- **La descarga pasa por el JWT.** Servir `/uploads` con un `FileServer`
  dejaría las facturas accesibles a quien adivinara la URL.
- Al borrar un movimiento, **primero la fila y después el archivo**. Si falla
  lo segundo queda un archivo huérfano: molesto pero inofensivo. Al revés
  quedaría un movimiento apuntando a una factura que no existe.

Como la descarga exige el header `Authorization`, un `<img src="...">` no
sirve: el frontend la baja con `fetch` y crea una URL `blob:` temporal.

## No hay endpoint de registro

Un `/register` público sería una puerta abierta a que cualquiera se cree una
cuenta en el servidor. Las cuentas las crea el administrador desde el panel.

El comando `createuser` sigue existiendo porque es la única forma de crear al
**primer** administrador: sin él no habría nadie que pudiera abrir el panel. Por
eso el primer usuario del servidor queda como admin sin tener que acordarse de
ninguna bandera. La clave se pide por teclado para que no quede en el historial
del shell ni en los logs de Docker.

## Migraciones embebidas en el binario

Con `//go:embed`. La imagen final no lleva el código fuente ni el CLI de
goose: el binario trae sus migraciones adentro y las aplica al arrancar.
Desplegar es `docker compose up -d`.

## Varios usuarios sin reescribir el dominio

La app nació para una sola persona, pero desde el primer día toda tabla de
dominio tiene `usuario_id` y **toda** consulta filtra por él. Ese id sale del
JWT, nunca de la URL: es lo que cierra el IDOR de raíz. Los índices únicos son
`(usuario_id, lower(nombre))`, así que dos personas pueden tener cada una su
categoría "Personal" sin chocar.

Por eso abrir la app a varios usuarios no tocó una sola consulta. Lo único que
faltaba era saber **quién administra a quién**, y eso son dos columnas: `rol` y
`activo`.

### El rol no viaja en el JWT

Se lee de la base en cada petición. Meterlo en el token ahorraría una consulta
por clave primaria, pero un token dura 24 horas: quitarle el rol a alguien no
surtiría efecto hasta que expirara. Un permiso que sobrevive a su revocación no
es un permiso, es un agujero. Lo mismo vale para `activo`: desactivar una
cuenta corta el acceso en la siguiente petición, no al día siguiente.

### Desactivar y eliminar son dos cosas distintas

`usuarios` tiene `ON DELETE CASCADE` colgando de todas las tablas de dominio,
así que un `DELETE` se lleva el historial de plata completo de esa persona. El
panel ofrece las dos acciones, pero no son intercambiables:

- **Desactivar** corta el acceso en la siguiente petición (lo revisa el
  middleware en todas) y no destruye nada. Es lo que se quiere casi siempre:
  un cliente que se va puede volver, y sus cifras del año pasado siguen ahí.
- **Eliminar** es definitivo y no tiene papelera. Se justifica cuando alguien
  pide que borres sus datos, o cuando la cuenta se creó por error.

El `CASCADE` no alcanza a las facturas, que viven en el disco: ninguna llave
foránea llega hasta ahí. Por eso el borrado lee las rutas **antes** de borrar
las filas y elimina los archivos después. Al revés ya no habría a quién
preguntarle cuáles eran, y los archivos quedarían ocupando la tarjeta de la
Raspberry para siempre sin una sola fila que dijera de quién eran.

Borrar exige mandar el correo de la cuenta como confirmación, y el frontend
obliga a escribirlo a mano. Un `confirm()` de "¿seguro?" no sirve aquí: con
veinte clientes de nombres parecidos, el error probable no es dudar sino
equivocarse de fila, y para eso un id en una URL no avisa de nada.

### "Al menos un admin activo" no puede ser un CHECK

Un `CHECK` mira una fila; esta regla mira todas. Vive en Go, dentro de la misma
transacción que hace el cambio y detrás de un `SELECT ... FOR UPDATE` sobre las
filas de los administradores activos. Sin ese candado, dos peticiones
simultáneas que degradan a dos admins distintos verían cada una que "todavía
queda el otro" y pasarían las dos: el servidor se quedaría sin ninguno y la
única forma de volver a entrar sería un `UPDATE` a mano por psql.

### Ver los datos de otro: una cabecera, no un endpoint nuevo

Para que el admin revise una cuenta ajena había dos caminos: duplicar cada
endpoint bajo `/api/admin/usuarios/{id}/...`, o cambiar el `usuario_id` del
context antes de que corran los handlers de siempre. Lo segundo es un
middleware de cuarenta líneas y cero código duplicado; lo primero habría sido
copiar movimientos, dashboard, categorías y medios, y mantener las dos copias
en sincronía para siempre.

El precio de esa elegancia es que el middleware **es** el salto del filtro por
`usuario_id`. Por eso está cerrado con tres llaves a la vez: solo un admin,
solo peticiones `GET`, y el observado tiene que existir. La restricción a `GET`
es la que de verdad importa: significa que ningún movimiento ajeno puede
aparecer modificado sin que su dueño lo haya hecho.

## El negocio y las finanzas del cliente son dos cosas distintas

Es la confusión fácil de este proyecto, así que vale decirlo explícito:

- `movimientos`, `categorias`, `medios_pago` → las finanzas **del cliente**.
  Cada uno ve las suyas, filtradas por `usuario_id`.
- `planes`, `pagos` → las finanzas **del dueño del servidor**. No llevan
  `usuario_id` porque no son de nadie: son del servidor. Lo que las protege no
  es un filtro, es `RequireAdmin` en el router.

Por eso el administrador no tiene resumen ni movimientos propios: no es un
cliente de su propia app. Sus rutas de dinero directamente no existen en el
frontend, y solo aparecen mientras observa la cuenta de alguien.

### Los pagos guardan su propia copia del monto y del plan

No un `plan_id`. Si mañana subes el precio del plan Pro, los cobros de marzo
tienen que seguir diciendo lo que de verdad se cobró en marzo; con un JOIN al
plan actual, el histórico se reescribiría solo cada vez que tocas una tarifa.

Es la misma razón por la que una factura impresa no cambia cuando cambia la
lista de precios. Y de paso, el registro sigue siendo legible cuando el cliente
ya no existe: `ON DELETE SET NULL` en `usuario_id`, con el correo copiado al
lado. Un cliente se va, pero los ingresos de marzo siguen siendo los de marzo.

### "Por cobrar" no es esperado menos cobrado

Se calcula aparte: la suma de los planes de los clientes activos que **no**
tienen pago registrado ese mes.

La resta parece equivalente y no lo es. Si un cliente te paga de más, o le
registras en este mes un cobro atrasado, la resta da un pendiente negativo o
esconde a quien sí debe. La cifra tiene que responder *¿a quién me falta
cobrarle?*, y eso solo lo contesta mirando cliente por cliente.

### El monto del cobro no viene del formulario

`RegistrarPago` lee el precio del plan en la misma consulta que inserta, con un
`INSERT ... SELECT` sobre `usuarios JOIN planes`. Si el monto llegara del
cliente HTTP, un dedazo registraría que alguien pagó $1 y los ingresos
quedarían descuadrados para siempre, sin que nada avisara.

Cobrar algo distinto sigue siendo posible — hay un campo opcional para el
descuento puntual — pero es una decisión explícita, no lo que pasa por defecto.

### Un índice único impide cobrar dos veces el mismo mes

`(usuario_id, periodo)`, con `periodo` normalizado siempre al día 1. Sin esa
normalización, "2026-03-01" y "2026-03-15" serían dos periodos distintos para
la base y el índice no serviría de nada. Un doble clic en "registrar pago" no
puede inflar los ingresos, y no porque el frontend deshabilite el botón.

## Los medios por defecto se siembran al crear la cuenta

La migración que introdujo `medios_pago` sembró Efectivo, Transferencia y Otro
a los usuarios que existían **en ese momento**. Con un solo usuario nunca se
notó, pero cualquier cuenta creada después abría con la lista vacía y sin nada
que elegir en el formulario de movimientos.

El sembrado vive en `medios.SembrarPorDefecto` y no dentro de
`auth.Store.Crear` para que el paquete de usuarios no tenga que saber que la
tabla `medios_pago` existe. Lo llaman los dos caminos que crean cuentas: el
comando `createuser` y el panel.

## Mensajes de error en dos niveles

Al cliente, siempre `"Error interno del servidor"`. El detalle real (que
filtra nombres de tablas) va al log y a la [bitácora](errores.md).

## Frontend: un hook y no solo CSS para el móvil

`useEsMovil` decide en JavaScript. En el teléfono no queremos *esconder* la
tabla, queremos renderizar algo **distinto**: tarjetas en vez de filas,
filtros plegados, menú abajo. Con CSS habría que pintar las dos versiones y
ocultar una.

## El monto se formatea mientras se escribe

Tecleas `1500000` y ves `1.500.000`. El detalle que cuesta: al meter los
puntos el texto cambia de largo y el navegador manda el cursor al final —
insoportable si estás corrigiendo un dígito en la mitad.

La solución es no guardar *la posición* del cursor sino **cuántos dígitos**
había antes de él: los puntos van y vienen, los dígitos no.
