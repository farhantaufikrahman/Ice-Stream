# syntax=docker/dockerfile:1

# ---- Tahap 1: build ----
# Samakan versi ini dengan baris `go` di go.mod.
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

# Download dependensi dulu, terpisah dari kode, supaya layer ini
# di-cache dan tidak diulang setiap kali kamu mengubah kode.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO_ENABLED=0 menghasilkan binary statis yang tidak butuh libc,
# sehingga bisa jalan di image distroless.
# -trimpath dan -ldflags="-s -w" membuang info debug agar binary lebih kecil.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---- Tahap 2: runtime ----
# distroless/static hanya berisi sertifikat CA, zona waktu, dan user nonroot.
# Tidak ada shell atau package manager, jadi celah keamanannya sangat sedikit.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/api /api

ENV APP_ENV=production \
    HTTP_PORT=8080
EXPOSE 8080

# Pakai UID angka (65532 = user nonroot di distroless), supaya Kubernetes
# bisa memastikan container tidak jalan sebagai root (runAsNonRoot).
USER 65532:65532
ENTRYPOINT ["/api"]