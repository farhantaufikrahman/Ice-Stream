// Command video menjalankan VideoService (gRPC) Ice-Stream.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/gormdb"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/grpcserver"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/video"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("video service berhenti karena error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadVideo()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, sqlDB, err := gormdb.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	store := video.NewS3Store(cfg.Storage)
	// Bucket dan aturan CORS disiapkan sekali saat start. Kalau storage belum
	// siap, service berhenti dan Kubernetes mencoba lagi (restart).
	setupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = store.EnsureBucket(setupCtx, cfg.Storage.CORSAllowedOrigins)
	cancel()
	if err != nil {
		return err
	}

	svc := video.NewService(video.NewOracleRepository(db), store, video.Options{
		MaxUploadBytes: cfg.MaxUploadBytes,
		UploadURLTTL:   cfg.UploadURLTTL,
		PlaybackURLTTL: cfg.PlaybackURLTTL,
	})

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.LogUnary(logger)))
	videov1.RegisterVideoServiceServer(srv, video.NewGRPCServer(svc, logger))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	go grpcserver.WatchHealth(ctx, healthSrv, videov1.VideoService_ServiceDesc.ServiceName, []grpcserver.Check{
		{Name: "oracle", Run: sqlDB.PingContext},
		{Name: "storage", Run: store.Ping},
	}, logger)

	if !cfg.IsProduction() {
		reflection.Register(srv)
	}

	logger.Info("video service siap", slog.String("env", cfg.Env), slog.String("bucket", cfg.Storage.Bucket))
	return grpcserver.Serve(ctx, srv, healthSrv, cfg.GRPCAddr, cfg.ShutdownTimeout, logger)
}
