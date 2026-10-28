# Baca .env (kalau ada) lalu jadikan environment variable untuk perintah di bawah.
-include .env
export

.PHONY: run test test-race fmt vet tidy docker-build docker-run k8s-up k8s-restart k8s-logs k8s-forward

IMAGE ?= ice-stream-api:dev

run: ## Jalankan API gateway
	go run ./cmd/api

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
	docker build -t $(IMAGE) .

docker-run: ## Jalankan image di Docker (tanpa Kubernetes) di port 4000
	docker run --rm -p 4000:8080 --env-file .env -e HTTP_PORT=8080 $(IMAGE)

k8s-up: ## Pasang atau perbarui semua manifest di namespace ice-stream
	kubectl apply -k ../infra/k8s/

k8s-restart: ## Restart pod supaya memakai image yang baru di-build
	kubectl -n ice-stream rollout restart deployment/api
	kubectl -n ice-stream rollout status deployment/api --timeout=60s

k8s-logs: ## Lihat log API gateway secara live
	kubectl -n ice-stream logs -f deployment/api

k8s-forward: ## Akses API dari PC
	kubectl -n ice-stream port-forward svc/api 4010:80
