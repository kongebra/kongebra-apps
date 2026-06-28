# build-stage: full toolchain
FROM node:22-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm install
COPY . .
ARG VERSION=dev
ENV VITE_APP_VERSION=${VERSION}
RUN npm run build

# runtime: kun .output + prod-deps på slank base (web er eneste tailnet-vendte tjeneste)
FROM node:22-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /src/.output ./.output
EXPOSE 3000
ENTRYPOINT ["node", ".output/server/index.mjs"]
