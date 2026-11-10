// Command api menjalankan API gateway Ice-Stream (REST/JSON untuk React).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/httpserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("server berhenti karena error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx otomatis dibatalkan saat menerima Ctrl+C (SIGINT) atau
	// SIGTERM, yaitu sinyal yang dikirim Kubernetes sebelum mematikan pod.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	publicKey, err := accesstoken.LoadPublicKey(cfg.JWTPublicKeyFile)
	if err != nil {
		return err
	}

	authConn, err := newGRPCClient(cfg.AuthGRPCAddr)
	if err != nil {
		return fmt.Errorf("menyiapkan klien auth %s: %w", cfg.AuthGRPCAddr, err)
	}
	defer authConn.Close()

	videoConn, err := newGRPCClient(cfg.VideoGRPCAddr)
	if err != nil {
		return fmt.Errorf("menyiapkan klien video %s: %w", cfg.VideoGRPCAddr, err)
	}
	defer videoConn.Close()

	// Gateway tidak terhubung ke Oracle. Yang dicek hanya AuthService, karena
	// tanpa auth hampir semua fitur tidak bisa dipakai. VideoService sengaja
	// tidak dicek: kalau video mati, login dan halaman lain tetap jalan, dan
	// endpoint video menjawab 503 sendiri.
	checks := map[string]httpserver.ReadinessCheck{
		"auth": grpcHealthCheck(healthpb.NewHealthClient(authConn), authv1.AuthService_ServiceDesc.ServiceName),
	}

	router, err := httpserver.NewRouter(cfg, logger, httpserver.Deps{
		Checks:   checks,
		Auth:     authv1.NewAuthServiceClient(authConn),
		Videos:   videov1.NewVideoServiceClient(videoConn),
		Verifier: accesstoken.NewVerifier(publicKey),
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server berjalan", slog.String("addr", cfg.HTTPAddr), slog.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("sinyal berhenti diterima, menunggu request selesai")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server berhenti dengan rapi")
	return nil
}

// newGRPCClient menyiapkan klien gRPC ke service lain di dalam cluster.
// grpc.NewClient belum membuka koneksi; koneksi dibuat saat RPC pertama dan
// otomatis disambung ulang jika putus. Di dalam cluster lalu lintas gRPC
// belum dienkripsi (insecure); mTLS bisa ditambahkan nanti.
func newGRPCClient(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Jeda sambung ulang bawaan bisa naik sampai 2 menit. Dibatasi 5 detik
		// supaya gateway cepat pulih setelah pod service restart.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.Config{BaseDelay: time.Second, Multiplier: 1.6, Jitter: 0.2, MaxDelay: 5 * time.Second},
			MinConnectTimeout: 5 * time.Second,
		}),
	)
}

// grpcHealthCheck memanggil health check standar gRPC milik service lain.
func grpcHealthCheck(client healthpb.HealthClient, service string) httpserver.ReadinessCheck {
	return func(ctx context.Context) error {
		resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
		if err != nil {
			return err
		}
		if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			return fmt.Errorf("status %s", resp.GetStatus())
		}
		return nil
	}
}
