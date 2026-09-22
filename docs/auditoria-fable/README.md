# Auditoría de Fable (21 de septiembre de 2026)

Qué se revisó, qué se encontró y en qué orden arreglarlo. Los arreglos están
en [`prompts/`](prompts/): **un archivo por arreglo**, escrito para pegarlo tal
cual en una sesión de Claude Opus. Van numerados en el orden recomendado.

## Qué hizo Fable

1. Leyó la documentación completa de `docs/`, el `README.md`, `main.go`,
   `router.go`, `db.go`, `config.go`, los Dockerfiles, `docker-compose.yml`,
   `nginx.conf`, `api.js`, `AuthContext.jsx` y `sw.js`.
2. Lanzó dos revisores en paralelo, uno para el backend en Go y otro para el
   frontend en React, con instrucciones de leer los archivos a fondo y
   reportar hallazgos con archivo y línea.
3. Verificó a mano contra el código cada hallazgo de impacto alto antes de
   darlo por bueno. Un hallazgo del revisor del frontend estaba mal (decía que
   el backend no aceptaba el filtro `a_quien`; sí lo acepta) y se corrigió.
4. No modificó ningún archivo del proyecto. Solo creó esta carpeta.

## Veredicto

La base es sólida: el patrón modelo/store/handler se respeta, el dinero nunca
es float, todo filtra por `usuario_id`, las escrituras de plata van en
transacción con lock por usuario, y la documentación explica el porqué de cada
decisión. Eso no se toca.

Lo que falta se agrupa en cuatro cosas:

| Grupo | Qué es | Prompts |
|---|---|---|
| Bugs visibles | Seis errores concretos que el cliente puede ver hoy | 01 a 06 |
| Red de seguridad | CI con Postgres, lint y tests del frontend | 07 |
| Frontend | Capa de datos, carga perezosa, búsqueda, modales, CSS | 08, 15 a 20 |
| Backend | Rate limit, revocación de tokens, goroutines, health, SSRF, cuotas, cabeceras, pool | 09 a 14, 21, 22 |
| Arquitectura | Partir `movimientos`, SQL cruzado, duplicación, `herramientas.go` | 23 a 26 |
| Reglas del dinero | Lo que encontraron las pruebas manuales: "Sin registrar", abonos, fecha futura, cubrir faltante, resumen | 27 a 37 |

## Cruce con el reporte de pruebas manuales

El mismo día, otros agentes probaron la app a mano (cuentas `prueba1` a
`prueba4@test.local`, unas 45 operaciones) y entregaron una lista de bugs.
Así se cruza con esta auditoría:

**Ya estaba cubierto** (se amplió el prompt con lo que agregaron las pruebas):

| Hallazgo de las pruebas | Prompt |
|---|---|
| Exportar: Tipo vacío en traslados y "Me prestaron", columnas corridas, falta "Debes", emojis como "." en el PDF | 01 (ampliado) |
| La lista de Movimientos y "Por confirmar" no se actualizan al guardar | 08 (ampliado) |
| Tres clics rápidos guardan el pago tres veces | 17 (ampliado con guardia síncrona e idempotencia) |
| "Datos inválidos" genérico | 29 y 36 |

**Nuevo**, con prompt propio:

| Hallazgo de las pruebas | Prioridad | Prompt |
|---|---|---|
| "Sin registrar" deja "Tienes" en negativo; "Me prestaron" creado ya pagado descuadra | Alta | 27 |
| Bajar el monto de un préstamo por debajo de lo abonado | Alta | 28 |
| Abonos con fecha anterior al préstamo; monto negativo guardado como positivo | Media | 29 |
| Los movimientos con fecha futura cuentan como plata de hoy; traslados automáticos con la fecha del gasto | Media | 30 |
| "Cubrir faltante": cascada, plan mostrado distinto al guardado, medios en $0, doble conteo, misma persona | Media | 31 |
| Editar un préstamo y cambiar el medio no cuenta la plata que vuelve; el aviso no se actualiza y el formulario se traba | Media | 32 |
| El Resumen dice "Septiembre de 2026" pero suma todo; conteo por medio distinto entre pantallas | Media | 33 |
| Desde/Hasta no se pueden escribir con el teclado | Media | 34 |
| La búsqueda no ignora tildes; "%" y "_" devuelven todo | Baja | 35 |
| "le pagas" / "te paga", "no debes nada", "¿Cómo fue el pago?", "Balance", un solo decimal, dos guiones | Baja | 36 |
| Recurrentes: "Desde" vacío se guarda con hoy, mensaje equivocado al borrar categoría/medio, pausados con pendientes | Baja | 37 |

**Las tres preguntas que dejaron las pruebas**, ya respondidas dentro de los
prompts (cámbialas ahí si no estás de acuerdo):

1. *"Sin registrar" al pagar una deuda*: se deja solo para plata que
   **entra**. Para plata que sale, el medio es obligatorio y se pregunta de
   dónde salió, como en los gastos (prompt 27).
2. *Movimientos con fecha futura*: **no cuentan** en "Tienes" ni en la
   guardia de fondos hasta que llegue su fecha; se marcan como
   "Programado" (prompt 30).
3. *El Resumen*: los **saldos** ("Tienes", "Te deben", "Debes") son de hoy y
   no tienen mes; los **flujos** ("Recibido", "Pagado") son del mes, con
   selector para cambiarlo y ver todo el historial (prompt 33).

Lo que las pruebas no cubrieron y sigue sin verificar: el asistente (las
cuentas de prueba no tenían plan con IA), los acuerdos por cuotas y el
botón "Recordar".

## Los hallazgos, resumidos

### Bugs

- **Exportar a Excel/PDF sale incompleto.** `informe.go` solo conoce tres tipos
  y dos estados: deudas propias, traslados y "parcial" salen en blanco.
- **"Cuentas con cada quien" no filtra.** El dashboard manda `?a_quien=` y
  `Movimientos.jsx` no lo lee. El backend sí lo acepta.
- **Sesión zombi.** Un 401 borra el token pero nadie cierra la sesión; la app
  sigue dibujada y cada acción falla hasta recargar.
- **Sin ErrorBoundary.** Cualquier error de render deja la pantalla en blanco.
- **Borrar un abono ignora el movimiento de la URL.**
- **El monto del cobro del negocio va sin convertir** ("25.000" llega como
  25 con tres decimales).

### Frontend

- No hay capa de datos: fetch, loading, error y recarga copiados en cada
  página; categorías y medios se piden en cinco sitios; sin refetch al volver
  a la pestaña; invalidación por un bus de eventos casero.
- Sin carga perezosa: todo cliente descarga el panel de admin y el chat.
- Búsqueda sin debounce ni cancelación, y con un historial por tecla.
- Botones sueltos sin guardia de doble toque.
- 16 `confirm()`/`alert()` nativos; Modal sin focus trap ni bloqueo de scroll.
- `estilos.css` con 3543 líneas, dos vocabularios de tokens, selectores
  redefinidos por "tandas", ~150 líneas de clases muertas.
- Sin ESLint, Prettier, Vitest ni CI. `npm install` en vez de `npm ci`.

### Backend

- El límite de intentos de login confía en `X-Forwarded-For`: se salta con
  una cabecera.
- Cambiar la contraseña no invalida los tokens ya emitidos (hasta 24 h).
- Las tres goroutines de fondo no tienen `recover` ni se esperan al apagar.
- `/health` responde 200 aunque Postgres esté caído.
- El endpoint de push acepta cualquier URL https (SSRF).
- La cuota diaria del agente no es atómica; 8 rondas × 25 s contra un
  timeout de 30 s.
- Uploads sin cuota por usuario.
- Facturas servidas sin `nosniff` ni CSP; nginx sin cabeceras de seguridad.
- Diagnóstico de fondos abre una segunda conexión con la transacción viva.
- Las pruebas de integración se saltan sin `TEST_DATABASE_URL`; sin CI,
  `go test ./...` "pasa" sin probar SQL.

### Arquitectura

- `movimientos` mezcla persistencia, reglas de fondos, reporting y Excel/PDF;
  las validaciones de dominio viven en el handler aunque las usan otros
  paquetes.
- `admin`, `auth`, `avisos` y `exportar` consultan tablas de otros paquetes.
- `escaneable` definido cinco veces, la zona horaria de Colombia tres veces
  en Go y dos en SQL, dos validadores de fecha con reglas distintas.
- `herramientas.go` con 1098 líneas.

## Cómo usar los prompts

1. Abre una sesión de Claude Opus en la raíz del proyecto.
2. Pega el contenido de `prompts/01-...md`. Espera a que termine, revisa el
   diff y corre las pruebas.
3. Haz commit (los prompts ya dicen cómo redactar el mensaje).
4. Sigue con el siguiente número.

Cada prompt es independiente, pero el orden importa en tres casos: el **07**
(CI) conviene hacerlo antes de los cambios grandes para que los proteja; el
**08** (capa de datos) simplifica los del **15** al **17**, así que va antes;
y los **27** y **28** (los dos de prioridad alta del reporte de pruebas) van
justo después de los bugs 01 a 06, antes que todo lo demás.

Orden recomendado completo: 01-06, 27, 28, 07, 29-33, 08, 09-14, 34-37,
15-22, 23-26.

Los prompts piden siempre lo mismo al final: pruebas, documentación al día y
un commit en español que diga qué cambió para el usuario, como los que ya
hay en el historial.
