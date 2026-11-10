# Baca .env (kalau ada) lalu jadikan environment variable untuk perintah di bawah.
-include .env
export

.PHONY: run run-auth proto proto-lint docker-build-auth docker-push-auth test test-race fmt vet tidy docker-build docker-run docker-push k8s-up k8s-restart k8s-logs k8s-logs-auth k8s-forward migrate-build migrate-push k8s-migrate k8s-forward-db run-video docker-build-video docker-push-video k8s-logs-video k8s-migrate-video k8s-forward-storage run-transcoder docker-build-transcoder docker-push-transcoder k8s-logs-transcoder k8s-forward-nats

IMAGE ?= $(DOCKERHUB_USER)/ice-stream-api:dev

AUTH_IMAGE ?= $(DOCKERHUB_USER)/ice-stream-auth:dev

VIDEO_IMAGE ?= $(DOCKERHUB_USER)/ice-stream-video:dev

MIGRATE_IMAGE ?= $(DOCKERHUB_USER)/ice-stream-migrations:dev

TRANSCODER_IMAGE ?= $(DOCKERHUB_USER)/ice-stream-transcoder:dev

docker-push: ## Upload image ke Docker Hub (jalankan docker login sekali dulu)
	docker push $(IMAGE)

docker-push-auth: ## Upload image AuthService ke Docker Hub
	docker push $(AUTH_IMAGE)

docker-push-video: ## Upload image VideoService ke Docker Hub
	docker push $(VIDEO_IMAGE)

docker-push-transcoder: ## Upload image transcoder ke Docker Hub
	docker push $(TRANSCODER_IMAGE)

run: ## Jalankan API gateway
	go run ./cmd/api


# Bucket dan MEDIA_PUBLIC_URL diisi khusus di sini, karena .env dipakai
# bersama VideoService (bucket videos). Foto profil ada di bucket images.
run-auth: ## Jalankan AuthService + UserService (gRPC) di port 50051; butuh make k8s-forward-db dan k8s-forward-storage
	STORAGE_BUCKET=images MEDIA_PUBLIC_URL= go run ./cmd/auth

# $$VIDEO_DB_PASSWORD (dua tanda $) dibaca oleh shell dari .env, bukan oleh
# make, jadi password tidak ikut tercetak di terminal.
run-video: ## Jalankan VideoService (gRPC) di port 50052; butuh make k8s-forward-db dan k8s-forward-storage
	DB_USER=video_svc DB_PASSWORD="$$VIDEO_DB_PASSWORD" GRPC_PORT=50052 go run ./cmd/video

# HTTP_PORT=8081 supaya tidak bentrok dengan gateway (make run) di 8080.
run-transcoder: ## Jalankan transcoder; butuh ffmpeg terpasang, make k8s-forward-nats, dan make k8s-forward-storage
	HTTP_PORT=8081 go run ./cmd/transcoder

proto: ## Buat ulang kode Go dari file .proto (folder gen/)
	buf generate

proto-lint: ## Periksa gaya penulisan file .proto
	buf lint

test: ## Jalankan semua test
	go test ./...

test-race: ## Test + pendeteksi race condition (butuh gcc: sudo apt install build-essential)
	go test -race ./...

fmt: ## Rapikan format kode
	gofmt -w .

vet: ## Cari bug umum secara statis
	go vet ./...

tidy: ## Sinkronkan go.mod dan go.sum
	go mod tidy

docker-build: ## Build image Docker untuk API gateway
	docker build --target api -t $(IMAGE) .

docker-build-auth: ## Build image Docker untuk AuthService
	docker build --target auth -t $(AUTH_IMAGE) .

docker-build-video: ## Build image Docker untuk VideoService
	docker build --target video -t $(VIDEO_IMAGE) .

docker-build-transcoder: ## Build image Docker untuk transcoder (Go + ffmpeg)
	docker build --target transcoder -t $(TRANSCODER_IMAGE) .

docker-run: ## Jalankan image gateway di Docker (tanpa Kubernetes) di port 4000; AuthService dan VideoService harus jalan (make run-auth, make run-video)
	docker run --rm -p 4000:8080 --env-file .env -e HTTP_PORT=8080 \
		-e AUTH_GRPC_ADDR=host.docker.internal:50051 \
		-e VIDEO_GRPC_ADDR=host.docker.internal:50052 \
		-e JWT_PUBLIC_KEY_FILE=/secrets/jwt-public.pem \
		-v $(CURDIR)/secrets/jwt-public.pem:/secrets/jwt-public.pem:ro \
		$(IMAGE)

k8s-up: ## Pasang atau perbarui semua manifest di namespace ice-stream
	kubectl apply -k ../infra/k8s/

k8s-restart: ## Restart pod gateway, auth, video, dan transcoder supaya memakai image yang baru di-push
	kubectl -n ice-stream rollout restart deployment/api deployment/auth deployment/video deployment/transcoder
	kubectl -n ice-stream rollout status deployment/auth --timeout=120s
	kubectl -n ice-stream rollout status deployment/video --timeout=120s
	kubectl -n ice-stream rollout status deployment/transcoder --timeout=120s
	kubectl -n ice-stream rollout status deployment/api --timeout=60s

k8s-logs: ## Lihat log API gateway secara live
	kubectl -n ice-stream logs -f deployment/api

k8s-logs-auth: ## Lihat log AuthService secara live
	kubectl -n ice-stream logs -f deployment/auth

k8s-logs-video: ## Lihat log VideoService secara live
	kubectl -n ice-stream logs -f deployment/video

k8s-logs-transcoder: ## Lihat log transcoder secara live
	kubectl -n ice-stream logs -f deployment/transcoder

k8s-forward: ## Akses API dari PC
	kubectl -n ice-stream port-forward svc/api 4010:80

migrate-build: ## Build image migrasi (Flyway + file SQL di folder migrations)
	docker build -t $(MIGRATE_IMAGE) migrations

migrate-push: ## Upload image migrasi ke Docker Hub
	docker push $(MIGRATE_IMAGE)

k8s-migrate: ## Jalankan migrasi schema AUTH_SVC di Kubernetes lalu tampilkan lognya
	kubectl -n ice-stream delete job migrate-auth --ignore-not-found
	kubectl apply -f ../infra/k8s/jobs/migrate-auth.yaml
	kubectl -n ice-stream wait --for=condition=complete job/migrate-auth --timeout=300s
	kubectl -n ice-stream logs job/migrate-auth

k8s-forward-db: ## Buka Oracle di localhost:1521 (untuk make run-auth dan aplikasi database di laptop)
	kubectl -n ice-stream port-forward svc/oracle 1521:1521

k8s-migrate-video: ## Buat user VIDEO_SVC + tabel videos di Kubernetes lalu tampilkan lognya
	kubectl -n ice-stream delete job migrate-video --ignore-not-found
	kubectl apply -f ../infra/k8s/jobs/migrate-video.yaml
	kubectl -n ice-stream wait --for=condition=complete job/migrate-video --timeout=300s
	kubectl -n ice-stream logs job/migrate-video -c bootstrap
	kubectl -n ice-stream logs job/migrate-video -c video

k8s-forward-storage: ## Buka RustFS di localhost:9000 (API S3) dan localhost:9001 (console)
	kubectl -n ice-stream port-forward svc/storage 9000:9000 9001:9001

k8s-forward-nats: ## Buka NATS di localhost:4222 (client) dan localhost:8222 (pemantauan)
	kubectl -n ice-stream port-forward svc/nats 4222:4222 8222:8222