# syntax=docker/dockerfile:1

# Satu Dockerfile, empat image. Pilih dengan --target:
#   docker build --target api        -t .../ice-stream-api:dev        .
#   docker build --target auth       -t .../ice-stream-auth:dev       .
#   docker build --target video      -t .../ice-stream-video:dev      .
#   docker build --target transcoder -t .../ice-stream-transcoder:dev .
#
# Kenapa dibedakan? API gateway tidak memakai database (hanya memanggil
# service lain lewat gRPC), jadi bisa jadi binary statis di image distroless
# yang sangat kecil. AuthService dan VideoService memakai GORM + godror yang
# butuh CGO dan Oracle Instant Client, jadi butuh libc (glibc) dan library
# Oracle di dalam image.

# Samakan versi ini dengan baris `go` di go.mod.
ARG GO_VERSION=1.27

# Oracle Instant Client Basic Light 23.26.4 untuk Linux x86-64.
# SHA-256 file zip ini (dicek 2026-10-28). Build gagal kalau isi file berubah.
ARG INSTANTCLIENT_URL=https://download.oracle.com/otn_software/linux/instantclient/2326400/instantclient-basiclite-linux.x64-23.26.4.0.0.zip
ARG INSTANTCLIENT_SHA256=d049e593bba0041feb15c14ea894e2f1a437377462b99d9bdb6b7ab30b2dd660
ARG INSTANTCLIENT_DIR=/opt/oracle/instantclient_23_26

# =====================================================================
# Target "api": binary statis (CGO_ENABLED=0) di distroless/static
# =====================================================================
FROM golang:${GO_VERSION}-alpine AS build-api
WORKDIR /src

# Download dependensi dulu, terpisah dari kode, supaya layer ini
# di-cache dan tidak diulang setiap kali kamu mengubah kode.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO_ENABLED=0 menghasilkan binary statis yang tidak butuh libc.
# -trimpath dan -ldflags="-s -w" membuang info debug agar binary lebih kecil.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/api

# distroless/static hanya berisi sertifikat CA, zona waktu, dan user nonroot.
FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build-api /out/app /app
ENV APP_ENV=production
# Pakai UID angka (65532 = user nonroot di distroless), supaya Kubernetes
# bisa memastikan container tidak jalan sebagai root (runAsNonRoot).
USER 65532:65532
ENTRYPOINT ["/app"]

# =====================================================================
# Target "auth" dan "video": binary CGO + Oracle Instant Client
# =====================================================================
# Image golang berbasis Debian sudah berisi gcc, yang dibutuhkan CGO.
# Builder dan runtime sama-sama Debian 13 (trixie) supaya versi glibc cocok.
# Kedua binary dibangun di tahap yang sama; cache Go membuat build kedua cepat.
FROM golang:${GO_VERSION}-trixie AS build-cgo
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/auth ./cmd/auth \
 && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/video ./cmd/video

# Tahap terpisah untuk download + cek checksum Instant Client, supaya curl
# dan unzip tidak ikut masuk ke image akhir.
FROM debian:trixie-slim AS instantclient
ARG INSTANTCLIENT_URL
ARG INSTANTCLIENT_SHA256
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl unzip \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /tmp/instantclient.zip "${INSTANTCLIENT_URL}" \
 && echo "${INSTANTCLIENT_SHA256}  /tmp/instantclient.zip" | sha256sum -c - \
 && mkdir -p /opt/oracle \
 && unzip -q /tmp/instantclient.zip -d /opt/oracle \
 && rm /tmp/instantclient.zip

# Dasar bersama untuk semua service yang memakai Oracle.
FROM debian:trixie-slim AS oracle-runtime
ARG INSTANTCLIENT_DIR
# libaio1t64: dibutuhkan Instant Client. Di Debian 13 namanya berakhiran
# t64 dan filenya libaio.so.1t64, sedangkan Instant Client mencari
# libaio.so.1, jadi dibuatkan symlink.
# ca-certificates: supaya service bisa memanggil HTTPS (misalnya AWS S3 atau
# Cloudflare R2 nanti).
RUN apt-get update \
 && apt-get install -y --no-install-recommends libaio1t64 ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && ln -s /usr/lib/x86_64-linux-gnu/libaio.so.1t64 /usr/lib/x86_64-linux-gnu/libaio.so.1
COPY --from=instantclient ${INSTANTCLIENT_DIR} ${INSTANTCLIENT_DIR}
# Daftarkan folder Instant Client ke linker supaya godror menemukan
# libclntsh.so tanpa perlu LD_LIBRARY_PATH.
RUN echo "${INSTANTCLIENT_DIR}" > /etc/ld.so.conf.d/oracle-instantclient.conf \
 && ldconfig
ENV APP_ENV=production
# Debian tidak punya user 65532, tapi UID angka tetap bisa dipakai.
USER 65532:65532

FROM oracle-runtime AS auth
COPY --from=build-cgo /out/auth /app
ENTRYPOINT ["/app"]

FROM oracle-runtime AS video
COPY --from=build-cgo /out/video /app
ENTRYPOINT ["/app"]

# =====================================================================
# Target "transcoder": binary statis + ffmpeg di Alpine
# =====================================================================
# Transcoder tidak memakai Oracle, jadi binary-nya statis (CGO_ENABLED=0)
# seperti gateway. Bedanya, ia butuh program ffmpeg dan ffprobe, sehingga
# tidak bisa memakai distroless. Alpine dipilih karena paket ffmpeg-nya
# lengkap (termasuk encoder H.264 libx264) dan image-nya kecil.
FROM golang:${GO_VERSION}-alpine AS build-transcoder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/transcoder ./cmd/transcoder

FROM alpine:3.24 AS transcoder
RUN apk add --no-cache ffmpeg ca-certificates
COPY --from=build-transcoder /out/transcoder /app
ENV APP_ENV=production
# UID angka yang sama dengan service lain (bukan root).
USER 65532:65532
ENTRYPOINT ["/app"]