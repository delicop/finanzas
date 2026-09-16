# Bitácora de errores

Cuando el servidor falla, la falla queda guardada en la base de datos para que
puedas revisarla y corregirla después.

## Por qué existe

La app va a vivir en una Raspberry Pi. Entrar por SSH a leer
`docker compose logs` desde el celular no es práctico, y esos logs se pierden
cada vez que se reconstruye la imagen. Guardados en Postgres quedan, y se
consultan con una petición desde cualquier parte.

**El cliente no ve nada de esto.** No hay pantalla ni aviso en la aplicación:
enterarse de una falla técnica no le sirve de nada y solo genera ruido.

## Qué se guarda

Solo lo que es una falla **del servidor**:

| Se guarda | No se guarda |
|---|---|
| Errores 500 (falló una consulta, falló el disco…) | Errores 4xx: un monto mal escrito, una sesión vencida |
| Pánicos de Go, con su traza completa | Peticiones normales |

Un 4xx es el usuario equivocándose, y eso no es una falla.

Cada registro tiene:

| Campo | Para qué sirve |
|---|---|
| `ocurrido_en` | Cuándo pasó |
| `contexto` | En qué parte del código: `"dashboard: calculando resumen"` |
| `mensaje` | El error técnico exacto |
| `metodo` + `ruta` | Qué petición lo provocó |
| `request_id` | Para cruzarlo con la línea correspondiente en `docker compose logs` |
| `usuario_id` | Quién la hizo |
| `es_panico` + `traza` | En un pánico, dónde reventó exactamente |
| `resuelto` + `resuelto_en` | Si ya lo corregiste |

## Cómo revisarlo desde afuera

Las rutas viven bajo `/api/mantenimiento` y usan un **token propio**, no el
login del cliente. Así:

1. El cliente no puede llegar a ellas ni escribiendo la URL a mano.
2. Tú puedes revisar el servidor sin pedirle la contraseña.

El token sale de `TOKEN_MANTENIMIENTO` en el `.env`. Si lo dejas vacío, **las
rutas no existen** (responden 404). Para generar uno:

```bash
openssl rand -base64 24
```

Sin el token correcto todo responde **404**, no 401: para quien no lo tenga,
estas rutas simplemente no están ahí.

### Los comandos

```bash
TOKEN="el-token-de-tu-.env"
API="http://localhost:8080"          # o http://ip-de-la-raspberry:8080
```

**Ver cuántas fallas hay sin revisar**

```bash
curl -s $API/api/mantenimiento/errores/conteo -H "Authorization: Bearer $TOKEN"
# {"pendientes":3,"total":17}
```

**Ver las pendientes**

```bash
curl -s "$API/api/mantenimiento/errores?pendientes=1" \
  -H "Authorization: Bearer $TOKEN" | jq
```

Sin `jq`, para leerlo cómodo:

```bash
curl -s "$API/api/mantenimiento/errores?pendientes=1" -H "Authorization: Bearer $TOKEN" \
  | python -c "
import json,sys
for e in json.load(sys.stdin)['errores']:
    print(f\"[{e['id']}] {e['ocurrido_en'][:19]}  {e['metodo']} {e['ruta']}\")
    print(f\"     {e['contexto']}\")
    print(f\"     {e['mensaje']}\n\")
"
```

**Ver todas (incluidas las ya corregidas)**

```bash
curl -s "$API/api/mantenimiento/errores?limite=100" -H "Authorization: Bearer $TOKEN" | jq
```

**Marcar una como corregida**

```bash
curl -s -X PATCH $API/api/mantenimiento/errores/42 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"resuelto":true}'
```

Para devolverla a pendiente, `{"resuelto":false}`.

**Limpiar del historial lo ya corregido**

```bash
curl -s -X DELETE $API/api/mantenimiento/errores/resueltos \
  -H "Authorization: Bearer $TOKEN"
# {"borrados":14}
```

### Parámetros del listado

| Parámetro | Qué hace |
|---|---|
| `pendientes=1` | Solo las que faltan por corregir |
| `limite` | Cuántas traer (máx. 200, por defecto 50) |
| `offset` | Para paginar |

## Directo en la base de datos

Si estás en el servidor, sale más rápido con psql:

```bash
# Las últimas 10 fallas pendientes
docker compose exec db psql -U finanzas -d finanzas -c "
  SELECT id, ocurrido_en, metodo, ruta, contexto, left(mensaje, 80) AS mensaje
  FROM errores WHERE NOT resuelto
  ORDER BY ocurrido_en DESC LIMIT 10;"

# La traza completa de un pánico
docker compose exec db psql -U finanzas -d finanzas -c "SELECT traza FROM errores WHERE id = 42;"

# Marcar como corregida
docker compose exec db psql -U finanzas -d finanzas -c "
  UPDATE errores SET resuelto = true, resuelto_en = now() WHERE id = 42;"

# Las fallas que más se repiten
docker compose exec db psql -U finanzas -d finanzas -c "
  SELECT contexto, count(*), max(ocurrido_en) AS ultima
  FROM errores WHERE NOT resuelto
  GROUP BY contexto ORDER BY count(*) DESC;"
```

## Flujo de trabajo

1. `.../errores/conteo` para saber si hay algo.
2. `.../errores?pendientes=1` para ver qué pasó.
3. El `contexto` te dice el paquete y la operación; el `mensaje` es el error
   real. Con el `request_id` puedes buscar la petición completa en
   `docker compose logs backend`.
4. Corriges, despliegas.
5. `PATCH .../errores/{id}` con `{"resuelto":true}`.
6. De vez en cuando, `DELETE .../errores/resueltos` para limpiar.

## Detalles de implementación

**Guardar un error nunca puede romper una petición.** Esto se ejecuta justo
cuando algo ya falló; si además reventara aquí, el usuario recibiría una
respuesta rota por culpa del sistema de registro. Por eso `Registrar` no
devuelve error: si no puede guardar, lo deja al menos en el log de Docker.

**Usa su propio contexto con timeout.** El contexto de la petición puede estar
ya cancelado (el usuario cerró la pestaña) y entonces no se guardaría nada.

**Los textos se recortan** antes de insertarse, para que un error enorme no
llene la tabla de una sola vez.

**Se borra solo lo viejo.** Al arrancar y cada 12 horas se eliminan los
registros de más de **30 días** (`registro.RetencionDias`). En una Raspberry
el disco es chico y una falla en bucle podría llenarlo en horas.

**Al cliente nunca se le muestra el detalle.** La respuesta siempre dice
`"Error interno del servidor"`: un error de base de datos filtra nombres de
tablas y columnas, que es información útil para un atacante. El detalle queda
solo en la bitácora.
