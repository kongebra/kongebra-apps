# Build-context er apps/tronderleikan (IKKE competition/) fordi competition bruker
# pkg-modulen via replace ../pkg. CI sender denne fila som dockerfile-input.
# build
FROM golang:1.26-alpine AS build
WORKDIR /src
# go.mod/go.sum først for layer-cachet go mod download (pkg/go.mod må med: replace-target)
COPY pkg/go.mod pkg/go.sum pkg/
COPY competition/go.mod competition/go.sum competition/
WORKDIR /src/competition
RUN go mod download
COPY pkg/ /src/pkg/
COPY competition/ /src/competition/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o /app .

# run
FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
EXPOSE 8080
# distroless har ingen shell/curl: health via binær-flagg, k8s exec-probe ["/app", "-health"]
ENTRYPOINT ["/app"]
