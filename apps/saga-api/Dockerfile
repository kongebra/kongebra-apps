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
# Deno is yt-dlp's required + auto-detected JS runtime for YouTube extraction (the EJS
# deprecation: modern YouTube needs a JS runtime to solve the player challenge, or yt-dlp
# fails with "add --js-runtimes RUNTIME"). On PATH -> yt-dlp auto-uses it, no flag needed.
# Runtime needs a writable cache dir: DENO_DIR=/work is set in the k8s deployment (readOnlyRootFilesystem).
ARG DENO_VERSION=2.9.1
ADD --chmod=0755 https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_linux /usr/local/bin/yt-dlp
ADD https://github.com/denoland/deno/releases/download/v${DENO_VERSION}/deno-x86_64-unknown-linux-gnu.zip /tmp/deno.zip
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates unzip \
    && unzip -d /usr/local/bin /tmp/deno.zip \
    && chmod 0755 /usr/local/bin/deno \
    && rm /tmp/deno.zip \
    && apt-get purge -y unzip \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /saga-api /saga-api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/saga-api"]
