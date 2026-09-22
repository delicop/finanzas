# 07 · CI con Postgres, lint y tests del frontend

Contexto: no hay `.github/`, `Makefile` ni pipeline. Las pruebas de
integración de Go se saltan sin `TEST_DATABASE_URL`
(`backend/internal/movimientos/integracion_test.go` ~línea 36), así que
`go test ./...` "pasa" sin probar una sola consulta. El frontend no tiene
ESLint, Prettier, Vitest ni TypeScript; hay comentarios
`eslint-disable-next-line` decorativos. El `Dockerfile` del frontend usa
`package-lock.json*` opcional y `npm install`, así que el build no es
reproducible.

Qué hacer:

1. **GitHub Actions** en `.github/workflows/ci.yml` con dos jobs:
   - `backend`: servicio `postgres:16-alpine`, `go vet ./...`,
     `staticcheck ./...` (instálalo en el job), y `go test ./...` con
     `TEST_DATABASE_URL` apuntando al servicio. Las integraciones deben
     **correr**, no saltarse.
   - `frontend`: `npm ci`, `npm run lint`, `npm test`, `npm run build`.
2. **ESLint** plano (flat config) con `eslint-plugin-react-hooks` y
   `eslint-plugin-jsx-a11y`. Corrige lo que salga o justifica cada
   desactivación con un comentario real.
3. **Prettier** con la configuración que mejor calce con el código actual
   (sin punto y coma, comillas simples). Un solo commit de formato aparte.
4. **Vitest** + Testing Library. Primeros tests, todos de lógica pura de
   `lib/formato.js`: `formatearEntradaMonto`, `entradaAMonto`, `diasHasta`,
   `faltaParaCobrar`, `sumarDias`, y el parseo de fechas locales. Después,
   uno de `apiFetch` con un `fetch` falso que devuelva 401 y compruebe que se
   emite el evento de sesión cerrada (prompt 03).
5. `Dockerfile` del frontend: `COPY package.json package-lock.json ./` y
   `npm ci`.
6. Un `Makefile` (o scripts en `package.json` y `backend/`) con `make test`,
   `make lint` para correr lo mismo en local.
7. Documenta en `docs/pruebas.md` cómo corre CI y cómo correr los tests del
   frontend. Agrega el badge al README si quieres.

No cambies lógica de la app en este prompt: solo tooling y tests.

Commit: "Pruebas automáticas en cada push: Go con Postgres y el frontend con lint y tests".
