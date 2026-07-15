# syntax=docker/dockerfile:1

FROM oven/bun:1.3.9-debian AS build
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
ARG VERSION=dev
ENV PUBLIC_APP_VERSION=${VERSION}
RUN bun run build

# Distroless Node runtime: no shell, nonroot (UID 65532). Serves the static dist/ via the
# built-in Node server (no curl/CMD-SHELL health check - k8s does an httpGet on /health).
FROM gcr.io/distroless/nodejs22-debian12:nonroot AS runtime
WORKDIR /app
ARG VERSION=dev
ENV APP_VERSION=${VERSION}
ENV NODE_ENV=production
ENV PORT=3000
COPY --from=build /app/dist ./dist
COPY server.mjs ./server.mjs
EXPOSE 3000
CMD ["/app/server.mjs"]
