# Arquitectura

```
finanzas/
├── backend/          API en Go (monolito)
├── frontend/         SPA en React + Vite
├── docs/             esta documentación
└── docker-compose.yml
```

Backend y frontend son **dos proyectos separados**: cada uno con su Dockerfile
y sus dependencias. No comparten código; hablan solo por HTTP/JSON.

## Backend

```
backend/
├── cmd/
│   ├── api/          el servidor HTTP
│   ├── createuser/   comando para crear el primer administrador
│   └── vapid/        genera las llaves de los avisos al celular
└── internal/
    ├── auth/         login, JWT, contraseñas, roles, límite de intentos
    ├── admin/        panel del dueño: usuarios, claves, activar/desactivar
    ├── agente/       el chat con el asistente (ver agente.md)
    ├── avisos/       resúmenes y recordatorios automáticos (ver avisos.md)
    ├── suscripciones/ el negocio: planes, cobros y el tablero del dueño
    ├── categorias/   CRUD de categorías
    ├── medios/       CRUD de medios de pago
    ├── movimientos/  movimientos, abonos, cuotas, facturas y el resumen
    ├── recurrentes/  lo que se repite: plantillas y lo que falta confirmar
    ├── tiendas/      los locales del cliente y sus cierres de caja (se vende aparte)
    ├── push/         avisos al celular con la app cerrada (ver avisos.md)
    ├── dinero/       validación de montos y reparto en cuotas
    ├── registro/     bitácora de errores
    ├── httpx/        helpers de request/response
    ├── config/       configuración por variables de entorno
    └── db/           conexión y migraciones
```

El frontend va vestido con **Classical**, un sistema editorial de papel y oro.
Sus reglas están en [diseno.md](diseno.md), y vale la pena leerlas antes de
agregar una pantalla: son fáciles de romper sin darse cuenta.

`internal/` es una carpeta especial de Go: **ningún proyecto externo puede
importar lo que esté ahí dentro**. Para un monolito eso significa que el
compilador ayuda a mantener el orden.

`agente` agrega tres archivos a ese patrón: `proveedor.go`, que es lo único que
habla con la API del modelo; `prompt.go`, con las instrucciones del sistema; y
`herramientas.go`, el catálogo de lo que el agente puede consultar. La
separación es lo que permite probar el chat entero sin red — y cambiar de
DeepSeek a OpenRouter sin tocar un handler.

Las herramientas **no abren una puerta propia a la base**: envuelven los mismos
`Store` que usan los endpoints, que ya filtran por `usuario_id`.

Cada paquete de dominio sigue el mismo patrón de tres archivos:

- `<modelo>.go` — los tipos y los errores del dominio
- `store.go` — **lo único que habla SQL**
- `handler.go` — valida la entrada y responde HTTP

Los handlers nunca escriben SQL. Cuando haya que cambiar una consulta, se toca
un solo archivo.

`tiendas` estira ese patrón a ocho archivos, porque adentro hay **dos**
modelos: la tienda, que es una lista como las categorías (`tienda.go`,
`store.go`, `handler.go`), y el cierre de caja, que es la hoja entera con sus
catorce cifras calculadas (`cierre.go`, `cierre_store.go`,
`cierre_handler.go`). Van en el mismo paquete y no en dos porque un cierre no
existe sin su tienda: sus rutas cuelgan del router de tiendas y heredan el mismo
permiso del plan, y partirlos obligaría a exportar ese permiso y la tienda solo
para que el otro paquete los use. Van en archivos aparte porque juntos serían un
store de 550 líneas donde el CRUD de una lista queda enterrado entre las
fórmulas de la hoja.

Los otros dos son suyos por la misma razón que el `exportar` de `movimientos`:
`foto.go` es la foto de la hoja firmada, y habla con el almacén de las facturas
**por una interfaz** que declara aquí mismo, sin importar `movimientos` (ver
[decisiones.md](decisiones.md#la-foto-usa-el-almacén-de-las-facturas-por-una-interfaz));
`exportar.go` es el Excel de los cierres, y es el **único** archivo del paquete
que convierte plata a float — Excel no guarda otra cosa —, así que tenerlo
aparte deja esa excepción encerrada en un solo sitio.

`admin` es un paquete aparte de `auth` porque responden preguntas distintas:
`auth` contesta *"¿quién eres y puedes entrar?"*; `admin`, *"¿quiénes existen y
qué puede hacer cada uno?"*. Juntarlos dejaría las rutas de administración
colgando del mismo sub-router que el login, que es público.

### Cómo se conecta todo

En [router.go](../backend/cmd/api/router.go) se construyen los `Store`, el
`TokenManager` y los `Handler` **una sola vez** y se inyectan hacia abajo. No
hay variables globales: así cada pieza se puede probar por separado.

Los middlewares van en grupos anidados, y el orden importa:

```
/api
├── /auth/login                        público
├── /mantenimiento/errores             token propio del .env
└── Group: RequireAuth → VerComo       todo lo privado
    ├── /categorias, /medios-pago, /movimientos, /dashboard
    ├── /tiendas, /cierres              solo si el plan las incluye (cada uno lo revisa)
    ├── /agente                         solo si hay llave del modelo
    ├── /notificaciones                 los avisos automáticos
    └── Group: RequireAdmin
        └── /admin/
            ├── /usuarios              clientes del servidor
            ├── /planes, /pagos, /negocio   el lado negocio
            └── /errores               la bitácora
```

`RequireAuth` valida el token y deja en el context el id y el rol. `VerComo` va
justo después: si llegó la cabecera `X-Ver-Como` de un admin en una petición
`GET`, cambia el id del context y todo lo de abajo responde con los datos del
usuario observado sin enterarse de nada.

Cada grupo pone su middleware **una sola vez**. Agregar una ruta dentro de un
grupo la deja protegida sin tener que acordarse: no hay forma de olvidarlo.

`/tiendas` y `/cierres` son la excepción visible: el permiso del plan no es un
grupo del router sino un middleware **dentro** de cada uno de los dos
sub-routers, porque lo que decide es un dato del cliente (su plan) y no del
token. `/cierres` es el mismo handler montado por segunda vez para leer los
cierres de todas las tiendas juntos, y por eso repite el middleware: si solo lo
tuviera `/tiendas`, quitarle la sección a un plan dejaría al cliente leyendo y
exportando sus cierres por la otra puerta.

Además de servir HTTP, `main` arranca tres tareas de fondo con su propia
goroutine: limpiar la bitácora de errores, limpiar el consumo del agente y
**generar los avisos** cada 6 horas. Son goroutines y no entradas de cron para
no depender de nada instalado en la máquina; lo que evita que se repitan es la
clave de periodo de cada aviso, no que la tarea lleve la cuenta.

Las piezas que `main` construye y el router solo conecta viajan en una struct
`dependencias`: varias las necesitan las dos partes, y con una lista posicional
larga es cuestión de tiempo que dos punteros del mismo tipo se crucen sin que
el compilador diga nada.

```
main.go
  └─ carga config → abre Postgres → corre migraciones → prepara /uploads
     └─ router.go
        ├─ middlewares globales (request id, logs, pánicos, CORS, timeout)
        ├─ /health                  público
        ├─ /api/auth/login          público
        ├─ /api/mantenimiento/*     token propio (ver errores.md)
        └─ resto de /api/*          exige JWT
```

## Frontend

```
frontend/src/
├── paginas/       una por pantalla (Login, Dashboard, Movimientos, ...)
├── componentes/   piezas reutilizables (Modal, formularios, visores)
└── lib/           cliente de la API, formato de datos, contextos
```

`lib/api.js` es el **único** archivo que hace `fetch`. Si cambia la forma de
autenticar o la URL base, se cambia ahí y nada más.

Los contextos (`AuthContext`, `TemaContext`) envuelven la app y evitan pasar
sesión y tema por props a cada componente.

## Base de datos

```
usuarios ──┬── categorias ──┐
           ├── medios_pago ─┼── movimientos
           ├── errores      ┘
           ├── tiendas ─────┐
           └────────────────┴── cierres ── cierre_lineas
```

`cierres` cuelga de los dos: de `tiendas`, porque una hoja siempre es de un
local, y de `usuarios` directamente, porque todas las consultas filtran por el
`usuario_id` del token igual que en el resto de la app. Las cinco listas de la
hoja (pagos por Nequi, compras, gastos, descuentos y vales) son **una** tabla,
`cierre_lineas`, con una columna `grupo`: son la misma cosa, una descripción y
un valor, y cinco tablas iguales se cambian mal en alguna.

Las migraciones están en `backend/internal/db/migrations/` y van **embebidas
en el binario** con `//go:embed`. La imagen final no lleva ni el código fuente
ni el CLI de goose: el binario trae sus migraciones adentro y las aplica al
arrancar.
