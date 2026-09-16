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
  "usuario": { "id": 1, "email": "tu@correo.com", "nombre": "Tu Nombre" } }
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
Opcionales: `descripcion` y la factura.

```bash
curl -X POST http://localhost:8080/api/movimientos \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"categoria_id":1,"medio_pago_id":2,"tipo":"preste","monto":"200000",
       "fecha":"2026-09-16","descripcion":"Préstamo",
       "a_quien":"Carlos","estado":"pendiente"}'
```

Marcar que ya pagaron, indicando por dónde:

```bash
curl -X PATCH http://localhost:8080/api/movimientos/7/estado \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"estado":"pagado","medio_cobro_id":3}'
```

## Resumen

`GET /api/dashboard` devuelve:

- `totales` — recibido, pagado, por cobrar, recuperado y balance
- `medios` — cuánto hay en cada medio de pago (**¿dónde está la plata?**)
- `categorias` — el desglose por categoría
- `deudores` — quién debe cuánto

En `medios` no aparece "por cobrar": un préstamo pendiente no está en ningún
medio, está con la persona. La suma de los saldos da exactamente el balance
general, y hay una fila **"Sin registrar"** para los movimientos sin medio —
sin ella los números no cuadrarían.

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
