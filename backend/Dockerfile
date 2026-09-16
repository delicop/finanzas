# ---------- Etapa 1: compilar ----------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Copiamos primero go.mod/go.sum solos: si no cambian las dependencias,
# Docker reutiliza la capa cacheada y no vuelve a descargar todo.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 -> binario estatico, no depende de libc del sistema.
# Por eso podemos correrlo sobre una imagen minima en la etapa 2.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/createuser ./cmd/createuser

# ---------- Etapa 2: imagen final ----------
FROM alpine:3.20

# ca-certificates: por si luego hacemos llamadas HTTPS salientes.
# tzdata: para que time.Now() en America/Bogota no se vuelva loco.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /out/createuser /app/createuser

# Solo /uploads necesita ser del usuario "app" (ahi escribe la API).
# Los binarios se quedan root:root 755: el proceso solo necesita leerlos.
# Un "chown -R /app" duplicaria los 24 MB de binarios en una capa nueva.
RUN mkdir -p /app/uploads && chown app:app /app/uploads

USER app
EXPOSE 8080

# Sin ENV de configuracion: todo entra por variables de entorno desde compose/.env
CMD ["/app/api"]
