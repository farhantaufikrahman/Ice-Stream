// Command video menjalankan VideoService (gRPC) Ice-Stream.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
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

	// NATS JetStream: antrean event ke transcoder. Stream dibuat di sini juga
	// (idempotent), jadi tidak masalah service mana yang start lebih dulu.
	nc, err := events.Connect(cfg.NATSURL, "video-service")
	if err != nil {
		return err
	}
	// Drain: tunggu pesan yang sedang diproses selesai, lalu tutup koneksi.
	defer nc.Drain()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	setupCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	err = events.EnsureStream(setupCtx, js)
	cancel()
	if err != nil {
		return err
	}

	svc := video.NewService(video.NewOracleRepository(db), store, video.NewNATSPublisher(js), video.Options{
		MaxUploadBytes: cfg.MaxUploadBytes,
		UploadURLTTL:   cfg.UploadURLTTL,
		PlaybackURLTTL: cfg.PlaybackURLTTL,
		MediaBaseURL:   cfg.MediaPublicURL,
	})

	// Terima laporan status dari transcoder (PROCESSING, READY, FAILED).
	consumer, err := video.ConsumeTranscodeEvents(ctx, js, svc, logger)
	if err != nil {
		return err
	}
	defer consumer.Stop()

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.LogUnary(logger)))
	videov1.RegisterVideoServiceServer(srv, video.NewGRPCServer(svc, logger))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	go grpcserver.WatchHealth(ctx, healthSrv, videov1.VideoService_ServiceDesc.ServiceName, []grpcserver.Check{
		{Name: "oracle", Run: sqlDB.PingContext},
		{Name: "storage", Run: store.Ping},
		{Name: "nats", Run: func(context.Context) error { return natsConnected(nc) }},
	}, logger)

	if !cfg.IsProduction() {
		reflection.Register(srv)
	}

	logger.Info("video service siap",
		slog.String("env", cfg.Env),
		slog.String("bucket", cfg.Storage.Bucket),
		slog.String("media_url", cfg.MediaPublicURL),
	)
	return grpcserver.Serve(ctx, srv, healthSrv, cfg.GRPCAddr, cfg.ShutdownTimeout, logger)
}

// natsConnected mengembalikan error jika koneksi NATS sedang terputus.
// Selama terputus, CompleteUpload tidak bisa mengirim event ke transcoder.
func natsConnected(nc *nats.Conn) error {
	if nc.Status() != nats.CONNECTED {
		return errors.New("NATS tidak terhubung: " + nc.Status().String())
	}
	return nil
}
