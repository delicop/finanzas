# Finanzas personales

App de finanzas personales para un solo usuario. Todo corre en Docker.

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

### Crear el usuario

No hay endpoint de registro: la app es de un solo usuario, así que se crea a mano.

```bash
docker compose exec backend /app/createuser -email tu@correo.com -nombre "Tu Nombre"
```

Pide la contraseña por teclado (no queda en el historial ni en los logs).

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

La migración deja creados **Efectivo**, **Transferencia** y **Otro**; el usuario
los renombra, los borra o agrega los suyos. `medio_pago_id` puede ser `null`
("sin registrar"): los movimientos viejos no lo tienen y no siempre se sabe.

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
- [ ] Tests automatizados
- [ ] Ajustes finales de despliegue en la Raspberry
