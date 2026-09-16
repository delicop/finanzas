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

Aunque hoy haya un solo usuario. Si el id viniera solo de la URL, cualquiera
con un token válido podría leer o borrar datos ajenos — eso es un **IDOR**.
Filtrar por el id que salió del JWT lo cierra de raíz.

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

Decir "ese correo no existe" le confirma a un atacante cuál es el único correo
válido del sistema.

## Límite de intentos de login

10 fallos por IP cada 15 minutos. La app queda expuesta en internet con **un
solo email que adivinar**; sin freno, un script prueba miles de claves por
minuto. Son ~50 líneas en memoria, sin Redis.

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

La app es de un solo usuario. Un `/register` público sería una puerta abierta.
El usuario se crea una vez con `createuser`, y la clave se pide por teclado
para que no quede en el historial del shell ni en los logs de Docker.

## Migraciones embebidas en el binario

Con `//go:embed`. La imagen final no lleva el código fuente ni el CLI de
goose: el binario trae sus migraciones adentro y las aplica al arrancar.
Desplegar es `docker compose up -d`.

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
