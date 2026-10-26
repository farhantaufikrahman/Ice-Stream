-include .env
export

.PHONY: run test test-race fmt vet tidy

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
