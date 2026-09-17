# Documentación

Guía para quien mantiene la aplicación de finanzas.

| Documento | De qué trata |
|---|---|
| [arquitectura.md](arquitectura.md) | Cómo está organizado el proyecto y por qué |
| [decisiones.md](decisiones.md) | Las decisiones técnicas importantes, explicadas |
| [api.md](api.md) | Todos los endpoints y el formato de los datos |
| [errores.md](errores.md) | **Cómo revisar y corregir las fallas del servidor** |
| [agente.md](agente.md) | El asistente: qué puede hacer, cómo se configura y sus límites |
| [avisos.md](avisos.md) | Los resúmenes y recordatorios automáticos |
| [pruebas.md](pruebas.md) | Cómo correr los tests automáticos |
| [despliegue.md](despliegue.md) | Puesta en marcha en la Raspberry Pi |
| [hoja-de-ruta.md](hoja-de-ruta.md) | Lo que se le podría agregar, en orden de valor |

## Arranque rápido

```bash
cp .env.example .env     # y cambia POSTGRES_PASSWORD y JWT_SECRET
docker compose up --build
docker compose exec backend /app/createuser -email tu@correo.com -nombre "Tu Nombre"
```

- App: http://localhost:5173
- API: http://localhost:8080
- Postgres desde el host: `localhost:5436`

## En una frase

Un backend en Go que guarda movimientos de dinero en Postgres, un frontend en
React para registrarlos y consultarlos, y todo en Docker para poder llevarlo a
una Raspberry Pi con un solo comando.
