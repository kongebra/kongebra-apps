# syntax=docker/dockerfile:1

# --- Byggesteg: node bygger TanStack Start (SSR) til .output ---
FROM node:22-bookworm-slim AS build
WORKDIR /app

# Deps først for lag-cache.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# --- Runtime: distroless node ---
# ponytail: Go-tjenestene bruker distroless/static (SPEC/CLAUDE image-filosofi),
# men en Node-SSR-server trenger et Node-runtime. distroless/nodejs er den
# nærmeste analogen: ingen shell/pakkebehandler, nonroot, minimal angrepsflate.
# nitro-outputen (.output) er selvstendig (egen node_modules), så runtime trenger
# kun .output.
FROM gcr.io/distroless/nodejs22-debian12:nonroot AS runtime
WORKDIR /app

# VERSION settes av CI (docker-build gir --build-arg VERSION=<tag>). Eksponeres
# som env for evt. senere visning; ingen funksjonell avhengighet i v1.
ARG VERSION=dev
ENV APP_VERSION=${VERSION}
ENV NODE_ENV=production
ENV PORT=3000

COPY --from=build /app/.output ./.output

EXPOSE 3000

# distroless/nodejs har ENTRYPOINT=node; healthz-proben er en k8s httpGet
# (shell-fri, i motsetning til Docker CMD-SHELL) mot /healthz.
CMD ["/app/.output/server/index.mjs"]
