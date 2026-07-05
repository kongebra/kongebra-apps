# build
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o /saga-api .

# run
# NOT distroless: the yt-dlp standalone binary is PyInstaller-built and needs
# glibc + a writable TMPDIR to self-extract. debian-slim is the smallest base
# that runs it. k8s: mount an emptyDir at /work and set TMPDIR=/work
# (readOnlyRootFilesystem).
FROM debian:bookworm-slim
ARG YTDLP_VERSION=2026.06.09
ADD --chmod=0755 https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_linux /usr/local/bin/yt-dlp
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /saga-api /saga-api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/saga-api"]
