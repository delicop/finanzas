# ---------- Etapa base: dependencias ----------
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install

# ---------- Etapa DEV: servidor de Vite con hot-reload ----------
# Se usa en desarrollo (target: dev en docker-compose).
FROM node:22-alpine AS dev
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev"]

# ---------- Etapa BUILD: compilar a estaticos ----------
FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# Vite incrusta las VITE_* en el bundle EN TIEMPO DE BUILD, no de ejecucion.
# Por eso la URL de la API entra como build-arg y no como env del contenedor.
ARG VITE_API_URL
ENV VITE_API_URL=$VITE_API_URL
RUN npm run build

# ---------- Etapa PROD: nginx sirviendo los estaticos ----------
# Es la que usas en la Raspberry: imagen final de ~50 MB, sin Node adentro.
FROM nginx:1.27-alpine AS prod
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
