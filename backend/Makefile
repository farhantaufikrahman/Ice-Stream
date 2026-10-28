# Baca .env (kalau ada) lalu jadikan environment variable untuk perintah di bawah.
-include .env
export

.PHONY: run test test-race fmt vet tidy docker-build docker-run docker-push k8s-up k8s-restart k8s-logs k8s-forward migrate-build migrate-push k8s-migrate k8s-forward-db

IMAGE ?= $(DOCKERHUB_USER)/ice-stream-api:dev

MIGRATE_IMAGE ?= $(DOCKERHUB_USER)/ice-stream-migrations:dev

docker-push: 
	docker push $(IMAGE)

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

migrate-build: ## Build image migrasi (Flyway + file SQL di folder migrations)
	docker build -t $(MIGRATE_IMAGE) migrations

migrate-push: ## Upload image migrasi ke Docker Hub
	docker push $(MIGRATE_IMAGE)

k8s-migrate: ## Jalankan migrasi schema AUTH_SVC di Kubernetes lalu tampilkan lognya
	kubectl -n ice-stream delete job migrate-auth --ignore-not-found
	kubectl apply -f ../infra/k8s/jobs/migrate-auth.yaml
	kubectl -n ice-stream wait --for=condition=complete job/migrate-auth --timeout=300s
	kubectl -n ice-stream logs job/migrate-auth

k8s-forward-db: ## Buka Oracle di localhost:1521 (untuk make run dan aplikasi database di laptop)
	kubectl -n ice-stream port-forward svc/oracle 1521:1521