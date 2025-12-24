FROM node:22-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci || npm i
COPY . .

# Bake Vite env at build-time
ARG VITE_API_BASE
ARG VITE_ADMIN_KEY
ENV VITE_API_BASE=${VITE_API_BASE}
ENV VITE_ADMIN_KEY=${VITE_ADMIN_KEY}

RUN npm run build

FROM nginx:alpine AS prod
COPY ./nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
