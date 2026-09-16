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
| PATCH  | `/api/movimientos/{id}/estado`   | Marcar un préstamo como pagado/pendiente (y por dónde te pagaron) |
| POST   | `/api/movimientos/{id}/factura`  | Adjuntar factura (multipart, campo `factura`) |
| GET    | `/api/movimientos/{id}/factura`  | Descargar/ver la factura                      |
| DELETE | `/api/movimientos/{id}/factura`  | Quitar la factura                             |
| GET    | `/api/dashboard`                 | Resumen: totales, saldo por medio de pago, por categoría y préstamos pendientes |
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
(`recibi`/`pague`/`preste`), `estado` (`pendiente`/`pagado`), `desde`, `hasta`
(AAAA-MM-DD), `q` (texto en descripción o persona), `limite` (máx. 200), `offset`.

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

### Cómo cuenta un préstamo

Prestar y que te devuelvan son dos movimientos de plata que **se anulan**:
salieron $200.000 y volvieron $200.000. Neto: cero.

| Estado      | ¿Cuenta en "por cobrar"? | Efecto en el balance |
|-------------|--------------------------|----------------------|
| `pendiente` | Sí                       | Resta el monto       |
| `pagado`    | No                       | Cero (salió y volvió)|

```
balance = recibido − pagado − por_cobrar
```

Por eso al marcar un préstamo como pagado **el balance sube ese monto**: es plata
que volvió a tu bolsillo. Lo que *no* se hace es sumarlo además a "recibido",
porque entonces los mismos $200.000 se contarían dos veces.

"Recuperado" (préstamos ya devueltos) se muestra aparte, solo informativo: no
entra en la fórmula del balance.

### Prestar y cobrar por medios distintos

Un préstamo guarda **dos** medios:

- `medio_pago_id` → por dónde salió la plata al prestarla
- `medio_cobro_id` → por dónde volvió cuando te pagaron

Prestas en efectivo y te pueden devolver por transferencia. Al marcar el
préstamo como pagado la app pregunta por dónde te pagaron, y el saldo baja en
un medio y sube en el otro.

### ¿Dónde está la plata?

El resumen muestra cuánto **tienes** en cada medio de pago:

```
Recibí = ingresos por ese medio + préstamos devueltos por ese medio
Pagué  = gastos por ese medio   + préstamos entregados por ese medio
Tengo  = Recibí − Pagué
```

Ahí no aparece "por cobrar" a propósito: un préstamo pendiente no está en
ningún medio, está con la persona que se lo llevó.

Incluye una fila **"Sin registrar"** con los movimientos que no tienen medio.
No es decorativa: sin ella los saldos no sumarían el balance general y no habría
forma de cuadrar los números.

En la lista, un préstamo pagado se muestra en **+ y verde** (la plata volvió) y
uno pendiente en **− y ámbar** (la plata está afuera).

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
- [x] Movimientos (recibí / pagué / presté) + adjuntar factura
- [x] Dashboard (resumen por categoría, total prestado pendiente)
- [x] Marcar préstamo pagado desde la lista (sin abrir el formulario)
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
- [ ] Tests automatizados
- [ ] Ajustes finales de despliegue en la Raspberry
