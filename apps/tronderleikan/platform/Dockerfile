# Build-context er apps/tronderleikan (IKKE platform/) fordi platform bruker
# pkg-modulen via replace ../pkg. CI sender denne fila som dockerfile-input.
# build
FROM golang:1.26-alpine AS build
WORKDIR /src
# go.mod/go.sum først for layer-cachet go mod download (pkg/go.mod må med: replace-target)
COPY pkg/go.mod pkg/go.sum pkg/
COPY platform/go.mod platform/go.sum platform/
WORKDIR /src/platform
RUN go mod download
COPY pkg/ /src/pkg/
COPY platform/ /src/platform/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o /app .

# run
FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
EXPOSE 8080
# distroless har ingen shell/curl: health via binær-flagg, k8s exec-probe ["/app", "-health"]
ENTRYPOINT ["/app"]
