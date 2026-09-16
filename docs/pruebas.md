# Pruebas automáticas

```bash
cd backend
go test ./...
```

Sin más, corren las pruebas que no necesitan base de datos. Las de integración
se **saltan solas**.

## Con base de datos

Las pruebas de integración corren contra un Postgres de verdad, porque lo que
prueban **es el SQL**: los CHECK que protegen las reglas de "presté" y las
sumas del resumen. Con una base falsa no se probaría nada real.

```bash
# Una sola vez: crear la base de prueba
docker compose exec db psql -U finanzas -d postgres -c "CREATE DATABASE finanzas_test OWNER finanzas;"

# Correr todo
cd backend
TEST_DATABASE_URL="postgres://finanzas:TU_CLAVE@localhost:5436/finanzas_test?sslmode=disable" go test ./...
```

La clave es la de `POSTGRES_PASSWORD` en el `.env`. Cada prueba limpia las
tablas antes de empezar, así que **no uses la base de producción**.

## Qué se cubre

| Paquete | Qué prueba |
|---|---|
| `dinero` | Validación de montos: decimales, ceros, negativos, topes |
| `httpx` | Validador de campos y lectura de JSON (cuerpos rotos, gigantes, campos desconocidos) |
| `auth` | JWT, contraseñas y límite de intentos |
| `movimientos` | Facturas (tipos, rutas, nombres) y **toda la matemática del dinero** |
| `registro` | Que un pánico no tumbe el servidor y que no se filtren detalles al cliente |

Las que más valen:

- **`TestRechazaAlgoritmoNone`** — que no se acepte un JWT sin firma
  (ataque de *alg confusion*).
- **`TestGuardarRechazaArchivoDisfrazado`** — que un `.txt` renombrado a
  `.png` no pase.
- **`TestRutaSeguraBloqueaSalidas`** — que un `../../etc/passwd` no escriba
  fuera de `/uploads`.
- **`TestPrestamoDevueltoQuedaEnCero`** — que cobrar un préstamo suba el
  balance **exactamente** lo que salió, y no el doble.
- **`TestLosSaldosPorMedioSumanElBalance`** — que las partes cuadren con el
  total.
- **`TestLosCentavosNoSePierden`** — que `0.10 + 0.20` dé `0.30` exacto.
- **`TestBusquedaNoEsVulnerableAInyeccion`** — que un `'; DROP TABLE` en el
  buscador no haga nada.
- **`TestNoSeVenDatosDeOtroUsuario`** — que adivinar un id ajeno no sirva.

## Comandos útiles

```bash
go test ./... -v                          # con detalle
go test ./internal/dinero/ -v             # un solo paquete
go test ./... -run Prestamo               # las que coincidan con el nombre
go test ./... -cover                      # cobertura
go test ./... -race                       # detector de condiciones de carrera
```

## Al agregar código

Lo que vale la pena probar:

1. **Cualquier cosa que toque plata.** Es lo único que no se puede equivocar.
2. **Las reglas de negocio en SQL.** Un CHECK sin prueba es un CHECK que nadie
   sabe si funciona.
3. **Las validaciones de seguridad.** Tipos de archivo, rutas, tokens.

Lo que no: los CRUD simples sin lógica. El tiempo rinde más en lo de arriba.
