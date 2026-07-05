# build stage: full toolchain
FROM node:22-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VERSION=dev
ENV VITE_APP_VERSION=${VERSION}
RUN npm run build

# runtime: only the Nitro .output on a slim base
FROM node:22-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
# saga-api base URL for SSR; overridden in k8s to the in-cluster service.
ENV SAGA_API_URL=http://saga-api:8080
COPY --from=build /src/.output ./.output
USER 65532:65532
EXPOSE 3000
ENTRYPOINT ["node", ".output/server/index.mjs"]
