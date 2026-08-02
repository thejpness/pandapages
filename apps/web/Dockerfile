# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd
ARG SUPABASE_AUTH_ORIGIN
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.31.2-alpine3.23-slim@sha256:defdc1caa3ea8401e9b6b2a5d336c1e3d17c6243fa4e12e6c3a466a16fb137ea

FROM ${NODE_IMAGE} AS node-base
ARG NPM_VERSION=12.0.1
WORKDIR /app
RUN npm install --global npm@${NPM_VERSION} \
  && npm cache clean --force

FROM node-base AS dev
COPY package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

FROM node-base AS builder
ARG SUPABASE_AUTH_ORIGIN
ARG VITE_API_BASE=""
ARG VITE_SUPABASE_URL
ARG VITE_SUPABASE_PUBLISHABLE_KEY
ENV VITE_API_BASE=$VITE_API_BASE
ENV VITE_SUPABASE_URL=$VITE_SUPABASE_URL
ENV VITE_SUPABASE_PUBLISHABLE_KEY=$VITE_SUPABASE_PUBLISHABLE_KEY

RUN test -n "$VITE_SUPABASE_URL" \
  && test "$VITE_SUPABASE_URL" = "$SUPABASE_AUTH_ORIGIN" \
  && printf '%s' "$VITE_SUPABASE_PUBLISHABLE_KEY" \
  | grep -Eq '^sb_publishable_[A-Za-z0-9_-]{20,}$'

COPY package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

COPY . .
RUN npm run build

FROM ${NGINX_IMAGE} AS prod
ARG SUPABASE_AUTH_ORIGIN
USER root
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
RUN printf '%s' "$SUPABASE_AUTH_ORIGIN" \
    | grep -Eq '^https://[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$' \
  && grep -q '__SUPABASE_AUTH_ORIGIN__' /etc/nginx/conf.d/default.conf \
  && sed -i "s|__SUPABASE_AUTH_ORIGIN__|$SUPABASE_AUTH_ORIGIN|g" /etc/nginx/conf.d/default.conf \
  && ! grep -q '__SUPABASE_AUTH_ORIGIN__' /etc/nginx/conf.d/default.conf
RUN rm -f /sbin/apk
USER 101
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/ || exit 1
