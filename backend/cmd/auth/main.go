// Command auth menjalankan AuthService (gRPC) Ice-Stream.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/auth"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/gormdb"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("auth service berhenti karena error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadAuth()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	key, err := auth.LoadPrivateKey(cfg.JWTPrivateKeyFile)
	if err != nil {
		return err
	}

	db, sqlDB, err := gormdb.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	svc, err := auth.NewService(auth.NewOracleRepository(db), auth.NewTokenMaker(key, cfg.AccessTokenTTL), cfg.RefreshTokenTTL)
	if err != nil {
		return err
	}

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(logUnary(logger)))
	authv1.RegisterAuthServiceServer(srv, auth.NewGRPCServer(svc, logger))

	// Health check standar gRPC, dipakai probe Kubernetes dan grpcurl.
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	go watchOracle(ctx, sqlDB.PingContext, healthSrv, logger)

	// Reflection membuat grpcurl bisa melihat daftar RPC tanpa file .proto.
	// Hanya dinyalakan di development.
	if !cfg.IsProduction() {
		reflection.Register(srv)
	}

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("auth service berjalan", slog.String("addr", cfg.GRPCAddr), slog.String("env", cfg.Env))
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("sinyal berhenti diterima, menunggu RPC selesai")
	}

	healthSrv.Shutdown() // semua status menjadi NOT_SERVING
	stopped := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		logger.Info("auth service berhenti dengan rapi")
	case <-time.After(cfg.ShutdownTimeout):
		srv.Stop()
		logger.Warn("batas waktu shutdown habis, koneksi diputus paksa")
	}
	return nil
}

// watchOracle mengecek Oracle tiap 5 detik dan memperbarui status health.
// Saat Oracle tidak bisa dihubungi, health menjawab NOT_SERVING sehingga
// Kubernetes berhenti mengirim traffic ke pod ini (mirip /readyz di gateway).
func watchOracle(ctx context.Context, ping func(context.Context) error, h *health.Server, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := healthpb.HealthCheckResponse_UNKNOWN
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := ping(pingCtx)
		cancel()

		next := healthpb.HealthCheckResponse_SERVING
		if err != nil {
			next = healthpb.HealthCheckResponse_NOT_SERVING
		}
		if next != last {
			// "" = status seluruh server; nama service = status AuthService saja.
			h.SetServingStatus("", next)
			h.SetServingStatus(authv1.AuthService_ServiceDesc.ServiceName, next)
			logger.Info("status oracle berubah", slog.String("status", next.String()), slog.Any("error", err))
			last = next
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// logUnary mencatat setiap RPC: nama method, kode hasil, dan durasinya.
// Interceptor gRPC = middleware di Gin/Express.
func logUnary(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.LogAttrs(ctx, slog.LevelInfo, "grpc request",
			slog.String("method", info.FullMethod),
			slog.String("code", status.Code(err).String()),
			slog.Duration("latency", time.Since(start)),
		)
		return resp, err
	}
}
