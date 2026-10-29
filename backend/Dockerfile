# syntax=docker/dockerfile:1

# Satu Dockerfile, dua image. Pilih dengan --target:
#   docker build --target api  -t .../ice-stream-api:dev  .
#   docker build --target auth -t .../ice-stream-auth:dev .
#
# Kenapa dibedakan? API gateway memakai go-ora (pure Go), jadi bisa jadi
# binary statis di image distroless yang sangat kecil. AuthService memakai
# GORM + godror yang butuh CGO dan Oracle Instant Client, jadi butuh libc
# (glibc) dan library Oracle di dalam image.

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
# Target "auth": binary CGO + Oracle Instant Client di debian-slim
# =====================================================================
# Image golang berbasis Debian sudah berisi gcc, yang dibutuhkan CGO.
# Builder dan runtime sama-sama Debian 13 (trixie) supaya versi glibc cocok.
FROM golang:${GO_VERSION}-trixie AS build-auth
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/auth

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

FROM debian:trixie-slim AS auth
ARG INSTANTCLIENT_DIR
# libaio1t64: dibutuhkan Instant Client. Di Debian 13 namanya berakhiran
# t64 dan filenya libaio.so.1t64, sedangkan Instant Client mencari
# libaio.so.1, jadi dibuatkan symlink.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libaio1t64 \
 && rm -rf /var/lib/apt/lists/* \
 && ln -s /usr/lib/x86_64-linux-gnu/libaio.so.1t64 /usr/lib/x86_64-linux-gnu/libaio.so.1
COPY --from=instantclient ${INSTANTCLIENT_DIR} ${INSTANTCLIENT_DIR}
# Daftarkan folder Instant Client ke linker supaya godror menemukan
# libclntsh.so tanpa perlu LD_LIBRARY_PATH.
RUN echo "${INSTANTCLIENT_DIR}" > /etc/ld.so.conf.d/oracle-instantclient.conf \
 && ldconfig
COPY --from=build-auth /out/app /app
ENV APP_ENV=production
# Debian tidak punya user 65532, tapi UID angka tetap bisa dipakai.
USER 65532:65532
ENTRYPOINT ["/app"]