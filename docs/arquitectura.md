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
│   └── createuser/   comando para crear el usuario
└── internal/
    ├── auth/         login, JWT, contraseñas, límite de intentos
    ├── categorias/   CRUD de categorías
    ├── medios/       CRUD de medios de pago
    ├── movimientos/  movimientos, facturas y el resumen
    ├── dinero/       validación de montos
    ├── registro/     bitácora de errores
    ├── httpx/        helpers de request/response
    ├── config/       configuración por variables de entorno
    └── db/           conexión y migraciones
```

`internal/` es una carpeta especial de Go: **ningún proyecto externo puede
importar lo que esté ahí dentro**. Para un monolito eso significa que el
compilador ayuda a mantener el orden.

Cada paquete de dominio sigue el mismo patrón de tres archivos:

- `<modelo>.go` — los tipos y los errores del dominio
- `store.go` — **lo único que habla SQL**
- `handler.go` — valida la entrada y responde HTTP

Los handlers nunca escriben SQL. Cuando haya que cambiar una consulta, se toca
un solo archivo.

### Cómo se conecta todo

En [router.go](../backend/cmd/api/router.go) se construyen los `Store`, el
`TokenManager` y los `Handler` **una sola vez** y se inyectan hacia abajo. No
hay variables globales: así cada pieza se puede probar por separado.

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
           └── errores      ┘
```

Las migraciones están en `backend/internal/db/migrations/` y van **embebidas
en el binario** con `//go:embed`. La imagen final no lleva ni el código fuente
ni el CLI de goose: el binario trae sus migraciones adentro y las aplica al
arrancar.
