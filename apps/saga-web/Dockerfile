# build stage: full toolchain
# node:24-slim = current Active LTS (Krypton), ships npm 11 - matches the dev/CI
# toolchain that generates package-lock.json. node:22 (npm 10) rejected the npm-11
# lockfile as out-of-sync ("Missing <transitive> from lock file").
FROM node:24-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VERSION=dev
ENV VITE_APP_VERSION=${VERSION}
RUN npm run build

# runtime: only the Nitro .output on a slim base
FROM node:24-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
# saga-api base URL for SSR; overridden in k8s to the in-cluster service.
ENV SAGA_API_URL=http://saga-api:8080
COPY --from=build /src/.output ./.output
USER 65532:65532
EXPOSE 3000
ENTRYPOINT ["node", ".output/server/index.mjs"]
