FROM node:24-alpine AS build

ARG VITE_API_BASE_URL
RUN test -n "$VITE_API_BASE_URL" || (echo "VITE_API_BASE_URL build argument is required" >&2; exit 1)
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM caddy:2.10-alpine

COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/web/dist /srv
RUN addgroup -S web && adduser -S -D -H -G web web && chown -R web:web /srv /config /data
USER web
ENV PORT=8080
EXPOSE 8080
