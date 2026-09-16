# Despliegue en la Raspberry Pi 4B

## Antes de empezar

- Raspberry Pi OS de 64 bits (la de 32 no sirve: Postgres 16 no tiene imagen
  para armv7)
- Docker y el plugin de compose
- Una tarjeta rápida o, mejor, un SSD por USB — la base de datos escribe
  bastante y una microSD lenta se nota

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER   # y volver a entrar por SSH
```

## Puesta en marcha

```bash
git clone https://github.com/delicop/finanzas.git
cd finanzas
cp .env.example .env
nano .env
```

Lo que **hay que** cambiar:

```bash
APP_ENV=production

POSTGRES_PASSWORD=<una clave larga y nueva>
JWT_SECRET=<openssl rand -base64 48>
TOKEN_MANTENIMIENTO=<openssl rand -base64 24>

# La IP o el dominio real de la Raspberry, no localhost
CORS_ORIGINS=http://192.168.1.50
VITE_API_URL=http://192.168.1.50:8080
```

Dos cambios más en `docker-compose.yml`:

1. En `frontend`, poner `target: prod` — nginx con el build estático, sin Node
   en la imagen final (~74 MB en vez de ~290 MB).
2. En `db`, **borrar la sección `ports`** para que Postgres no quede expuesto a
   la red. Los contenedores se hablan entre ellos por la red interna.

```bash
docker compose up -d --build
docker compose exec backend /app/createuser -email cliente@correo.com -nombre "Nombre"
```

Las imágenes se construyen en la propia Pi, así que salen en arm64 sin
configuración extra. El primer build tarda un buen rato.

## Verificar

```bash
docker compose ps                  # los tres en "Up", db "healthy"
curl localhost:8080/health         # {"status":"ok"}
docker compose logs -f backend
```

## Que arranque solo

Los servicios ya tienen `restart: unless-stopped`, así que vuelven solos
después de un corte de luz. Solo hace falta que Docker arranque con el
sistema:

```bash
sudo systemctl enable docker
```

## Respaldos

Lo importante son dos cosas: la base y las facturas.

```bash
#!/bin/bash
# respaldo.sh — dejar en cron
DESTINO=/mnt/respaldo/finanzas
FECHA=$(date +%F)
mkdir -p "$DESTINO"

cd /home/pi/finanzas
docker compose exec -T db pg_dump -U finanzas finanzas | gzip > "$DESTINO/db-$FECHA.sql.gz"
tar czf "$DESTINO/facturas-$FECHA.tar.gz" backend/uploads/

# Conservar 30 días
find "$DESTINO" -name "*.gz" -mtime +30 -delete
```

```bash
chmod +x respaldo.sh
crontab -e
# 0 3 * * *  /home/pi/finanzas/respaldo.sh
```

**Un respaldo que nunca se restauró no es un respaldo.** Pruébalo:

```bash
gunzip -c db-2026-09-16.sql.gz | docker compose exec -T db psql -U finanzas -d finanzas_prueba
```

## Actualizar

```bash
cd /home/pi/finanzas
git pull
docker compose up -d --build
```

Las migraciones se aplican solas al arrancar el backend. Si agregaste alguna
dependencia al frontend, hace falta renovar el volumen de `node_modules`:

```bash
docker compose up -d --build --renew-anon-volumes frontend
```

## Acceso desde fuera de la casa

Tres opciones, de menos a más trabajo:

1. **Tailscale o WireGuard** — la Pi queda en una VPN privada. Es lo más
   seguro y no hay que abrir nada en el router.
2. **Cloudflare Tunnel** — dominio con HTTPS sin abrir puertos.
3. **Abrir el puerto en el router** — la peor. Si vas por ahí, pon al menos
   nginx o Caddy delante con HTTPS: **sin TLS, la contraseña viaja en texto
   plano**.

Con proxy delante, asegúrate de que envíe `X-Forwarded-For`; el límite de
intentos de login lo usa para identificar la IP real.

## Si algo falla

```bash
docker compose ps                       # ¿están arriba?
docker compose logs backend --tail 50
docker compose logs db --tail 50
df -h                                   # ¿disco lleno?
free -h                                 # ¿memoria?
```

Las fallas del servidor quedan guardadas — ver [errores.md](errores.md).

**El login va lento.** Normal: bcrypt costo 12 tarda ~300-500 ms en la Pi. Si
molesta, bajar a 11 en `internal/auth/password.go` sigue siendo seguro.

**El build se queda sin memoria.** Pasa en las Pi de 2 GB. Agrega swap:

```bash
sudo dphys-swapfile swapoff
sudo sed -i 's/CONF_SWAPSIZE=.*/CONF_SWAPSIZE=2048/' /etc/dphys-swapfile
sudo dphys-swapfile setup && sudo dphys-swapfile swapon
```

**No se ve desde el celular.** Revisa que `CORS_ORIGINS` y `VITE_API_URL`
tengan la IP real de la Pi, no `localhost`. Y que el celular esté en la misma
red.
