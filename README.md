# Ice-Stream

Platform video dan siaran langsung bergaya Bilibili: unggah video, transcoding HLS otomatis, danmaku (komentar yang melayang di atas video), komentar, playlist, langganan kanal, siaran langsung dari OBS atau browser, Creator Studio, dan halaman moderasi admin.

Proyek belajar dengan arsitektur microservice: React di depan, API gateway REST di tengah, service gRPC di belakang, semuanya berjalan di Kubernetes lokal (Docker Desktop).

## Fitur

- **Akun**: daftar, masuk, lupa kata sandi lewat email, masuk dengan Google atau GitHub. Access token JWT Ed25519 (15 menit) + refresh token di cookie httpOnly.
- **Video**: unggah langsung ke storage per bagian 50 MB (multipart, URL presigned, bisa dilanjutkan jika terputus), transcoding HLS 360p dan 720p, pilihan thumbnail, visibilitas Publik/Tidak publik/Pribadi, draf.
- **Menonton**: pemutar HLS, danmaku live (Server-Sent Events), komentar dan balasan, suka, Tonton nanti, playlist, riwayat tonton dengan posisi terakhir, bagikan dengan waktu mulai.
- **Penemuan**: beranda dengan rekomendasi, Trending 7 hari, pencarian video/kreator/playlist, kategori dan tag.
- **Kanal**: halaman kanal, ikuti, notifikasi video baru.
- **Siaran langsung**: RTMP (OBS) atau WHIP dari browser lewat MediaMTX, chat siaran dengan mode lambat dan khusus pengikut, rekaman siaran (jika dinyalakan) menjadi video draf.
- **Creator Studio**: dasbor, konten, analitik tayangan, kelola komentar dan danmaku.
- **Moderasi (admin)**: antrean video yang dilaporkan, turunkan video dengan alasan, tolak laporan, pulihkan video. Pemilik video diberi notifikasi beserta alasannya.
- **Operasional**: rate limit di gateway, metrik Prometheus + dasbor Grafana, health/readiness probe, CI/CD GitHub Actions, analisis kode SonarQube Cloud.

## Arsitektur

```
Browser (React + Vite)
   │  http://ice-stream.localhost:4000
   ▼
NGINX Gateway Fabric (Gateway API, port 4000)
   ├── web          nginx yang menyajikan hasil build React
   ├── api          API gateway REST/JSON (Go + Gin), rate limit, cek JWT
   │     ├── gRPC ──► auth   AuthService + UserService ──► Oracle (schema AUTH_SVC)
   │     └── gRPC ──► video  VideoService, CommentService, DanmakuService,
   │                         LiveService, ModerationService ──► Oracle (VIDEO_SVC)
   ├── storage      RustFS (S3-compatible): file asli, HLS, thumbnail, foto profil
   ├── live         MediaMTX: RTMP 1935, HLS, WebRTC/WHIP
   └── mail         Mailpit (SMTP palsu untuk development)

video ──NATS JetStream──► transcoder (ffmpeg) ──► storage
video ──NATS──► api (danmaku dan chat live ke browser lewat SSE)
```

| Bagian | Teknologi |
| --- | --- |
| Frontend | React 19, React Router, Vite, hls.js, Vitest |
| Gateway | Go, Gin, golang.org/x/time/rate, Prometheus client_golang |
| Service | Go, gRPC + Protocol Buffers (buf), GORM + godror (Oracle) |
| Data | Oracle Database Free 23, RustFS (S3), NATS JetStream |
| Media | ffmpeg (HLS), MediaMTX (RTMP/WebRTC) |
| Infrastruktur | Docker, Kubernetes (Docker Desktop), Gateway API (NGINX Gateway Fabric), Kustomize, Flyway, Helm |
| Online | Cloudflare Tunnel + DNS, Vercel (frontend), Resend (email) |
| Kualitas & monitoring | GitHub Actions, SonarQube Cloud, Prometheus, Grafana (kube-prometheus-stack) |

Padanan untuk yang terbiasa dengan Node.js: gateway berperan seperti aplikasi Express, interceptor gRPC seperti middleware Express, dan GORM seperti Sequelize/Prisma. Di Java, susunan ini mirip Spring Cloud Gateway di depan beberapa service Spring Boot yang saling memanggil lewat gRPC.

## Struktur folder

```
backend/
  cmd/            api, auth, video, transcoder (satu folder = satu program)
  internal/       logika tiap service, gateway (httpserver), config, metrics
  proto/          kontrak gRPC (.proto); kode Go-nya di gen/ (make proto)
  migrations/     SQL Flyway: auth/, video/, bootstrap/
frontend/
  src/            halaman (pages/), komponen, API client, hooks
  nginx/          konfigurasi nginx untuk image produksi
infra/k8s/        semua manifest Kubernetes versi lokal (kubectl apply -k)
infra/online/     overlay versi online: Cloudflare Tunnel, alamat https, Resend
infra/monitoring/ nilai Helm Prometheus + Grafana, PodMonitor, dasbor
.github/workflows ci.yml (tes + SonarQube), cd.yml (Docker Hub + Vercel)
```

## Menjalankan di Kubernetes lokal

Perintah di bawah dijalankan di **Ubuntu (WSL)** dari folder `backend/`, kecuali disebut lain.

1. **Prasyarat**: Docker Desktop dengan Kubernetes aktif, `kubectl`, Go (versi sesuai baris `go` di `backend/go.mod`), Node.js 22, akun Docker Hub, dan NGINX Gateway Fabric terpasang di cluster ([panduan instalasi](https://docs.nginx.com/nginx-gateway-fabric/install/)).
2. **Rahasia** (tidak pernah di-commit):
   - Salin `infra/k8s/secrets/*.env.example` menjadi `*.env` di folder yang sama, lalu isi password dan kuncinya.
   - Buat pasangan kunci JWT Ed25519 di `backend/secrets/` lalu salin ke `infra/k8s/secrets/`:
     ```bash
     mkdir -p secrets
     openssl genpkey -algorithm ed25519 -out secrets/jwt-private.pem
     openssl pkey -in secrets/jwt-private.pem -pubout -out secrets/jwt-public.pem
     cp secrets/jwt-private.pem secrets/jwt-public.pem ../infra/k8s/secrets/
     ```
   - Salin `backend/.env.example` menjadi `backend/.env` dan isi `DOCKERHUB_USER`.
3. **Build dan push image**:
   ```bash
   make docker-build docker-build-auth docker-build-video docker-build-transcoder docker-build-web migrate-build
   make docker-push docker-push-auth docker-push-video docker-push-transcoder docker-push-web migrate-push
   ```
4. **Pasang dan migrasi**:
   ```bash
   make k8s-up
   kubectl -n ice-stream rollout status statefulset/oracle --timeout=600s
   make k8s-migrate
   make k8s-migrate-video
   make k8s-restart
   ```
5. **Buka** http://ice-stream.localhost:4000. Email lupa kata sandi muncul di http://mail.ice-stream.localhost:4000.
6. **Jadikan akun admin** (untuk menu Creator Studio, Moderasi): `make k8s-make-admin USERNAME=namakamu`, lalu keluar dan masuk lagi.

Untuk mengembangkan frontend dengan hot reload, jalankan `npm run dev` di folder `frontend/`. Proxy Vite meneruskan `/api` ke gateway di port 4000 (lihat `frontend/.env.example`).

## Tes

```bash
# backend/ (WSL)
make fmt vet test-race
# frontend/ (WSL)
npm run lint && npm test && npm run build
```

CI menjalankan pemeriksaan yang sama di setiap Pull Request ke `dev` dan `main`.

## Online

| Bagian | Alamat | Jalan di |
| --- | --- | --- |
| Frontend | https://icestream.vzkyrielight.my.id | Vercel (di-deploy `cd.yml`) |
| API | https://api-icestream.vzkyrielight.my.id | Kubernetes laptop, lewat Cloudflare Tunnel |
| Media (HLS, foto) | https://media-icestream.vzkyrielight.my.id | sama |
| Upload video | https://upload-icestream.vzkyrielight.my.id | sama (langsung ke RustFS) |
| Siaran (HLS, WHIP) | https://live-icestream.vzkyrielight.my.id, https://webrtc-icestream.vzkyrielight.my.id | sama |

Alur rilis: merge ke `main` → workflow **CD** menjalankan tes, push image `:main` ke Docker Hub, dan deploy frontend ke Vercel → di WSL jalankan `make deploy` (dari `backend/`) untuk memasang `infra/online/`, migrasi, dan restart service. `make deploy DEPLOY_TAG=dev` memakai image `:dev` dari laptop (sebelum ada rilis ke `main`). `make deploy-local` mengembalikan cluster ke versi lokal.

Rahasia versi online (tidak di-commit): `infra/online/secrets/cloudflared.env` (token tunnel) dan `infra/online/secrets/smtp.env` (API key Resend).

## Monitoring

```bash
# backend/ (WSL). Butuh helm dan infra/k8s/secrets/grafana.env
make monitoring-up
make grafana-forward      # http://localhost:3000, dasbor "Ice-Stream"
make prometheus-forward   # http://localhost:9095, menu Status > Targets
```

## Rate limit dan metrik

- **Rate limit** (gateway, `backend/internal/httpserver/ratelimit.go`): login/daftar/lupa sandi 10 percobaan lalu 1 per 6 detik per IP; refresh token 30 lalu 1 per 2 detik per IP; buat video 10 lalu 1 per 3 menit; URL bagian upload 30 lalu 1 per detik; upload gambar 10 lalu 1 per 10 detik; komentar 10 lalu 1 per 3 detik; danmaku dan chat 10 lalu 1 per detik; laporan 5 lalu 1 per 30 detik (per user). Jawaban saat terlampaui: `429` dengan header `Retry-After`. Matikan dengan `RATE_LIMIT_ENABLED=false`.
- **Metrik Prometheus** di `GET /metrics`, hanya di dalam cluster: gateway port 9090, AuthService 9091, VideoService 9092, transcoder di port HTTP-nya (8080). Contoh melihatnya:
  ```bash
  kubectl -n ice-stream port-forward deployment/api 9090:9090
  curl -s localhost:9090/metrics | grep icestream_
  ```

## Keterbatasan yang diketahui

- Rate limit disimpan di memori tiap pod gateway: dengan 2 pod, batasnya efektif dua kali lipat, dan kembali penuh setelah pod restart.
- File HLS video Pribadi atau yang diturunkan admin tetap bisa diputar oleh orang yang sudah tahu alamat lengkapnya. Alamatnya acak dan tidak pernah ditampilkan ke orang lain, tapi tidak dikunci per penonton.
- Role admin dibaca dari access token, jadi perubahan role baru terasa setelah masuk ulang (paling lama 15 menit).
- Cluster lokal memakai HTTP. Versi online mendapat HTTPS dari Cloudflare (`infra/online/` menyalakan `COOKIE_SECURE=true`); ganti semua password contoh sebelum online.
- Versi online berjalan di laptop lewat Cloudflare Tunnel, yang hanya meneruskan HTTP. RTMP (OBS) dan media WebRTC dari luar laptop tidak bisa masuk, jadi siaran online hanya untuk admin dari laptop itu sendiri (`LIVE_BROADCAST_ADMIN_ONLY=true`). Menonton dan chat tetap terbuka untuk semua. Situs juga mati saat laptop mati.
