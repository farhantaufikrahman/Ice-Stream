package grpcserver

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

// NewClient menyiapkan klien gRPC ke service lain di dalam cluster. Dipakai
// gateway (ke AuthService dan VideoService) dan VideoService (ke
// UserService, untuk notifikasi).
//
// grpc.NewClient belum membuka koneksi; koneksi dibuat saat RPC pertama dan
// otomatis disambung ulang jika putus. Di dalam cluster lalu lintas gRPC
// belum dienkripsi (insecure); mTLS bisa ditambahkan nanti.
func NewClient(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Jeda sambung ulang bawaan bisa naik sampai 2 menit. Dibatasi 5 detik
		// supaya cepat pulih setelah pod service tujuan restart.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.Config{BaseDelay: time.Second, Multiplier: 1.6, Jitter: 0.2, MaxDelay: 5 * time.Second},
			MinConnectTimeout: 5 * time.Second,
		}),
	)
}
