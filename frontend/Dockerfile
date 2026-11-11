# Image frontend Ice-Stream: build React (Vite) lalu sajikan file statisnya
# dengan nginx. Dua tahap (multi-stage), jadi Node.js dan node_modules tidak
# ikut ke image akhir; yang tersisa hanya nginx + folder dist (sekitar 80 MB).
#
# Build dari folder backend:  make docker-build-web
# Atau langsung:              docker build -t <user>/ice-stream-web:dev frontend

# ---------- Tahap 1: build ----------
# Versi Node sama dengan "engines" di package.json (>= 22.22.0).
FROM node:22.22.0-alpine3.23 AS build
WORKDIR /app

# package*.json disalin lebih dulu: selama dependensi tidak berubah, layer
# npm ci diambil dari cache dan build berikutnya jauh lebih cepat.
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .
# Alamat API yang dipanggil browser. /api/v1 = origin yang sama dengan
# halaman; Gateway meneruskan /api ke Service api (infra/k8s/web/httproute.yaml).
# Nilai VITE_* ditanam saat build, bukan dibaca saat container berjalan.
ARG VITE_API_BASE_URL=/api/v1
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
RUN npm run build

# ---------- Tahap 2: sajikan ----------
# Varian "unprivileged" berjalan sebagai user nginx (uid 101) di port 8080,
# bukan root di port 80.
FROM nginxinc/nginx-unprivileged:1.28.2-alpine3.23 AS web
COPY nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
