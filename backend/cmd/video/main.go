// Command video menjalankan VideoService (gRPC) Ice-Stream.
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

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/comment"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/danmaku"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/gormdb"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/grpcserver"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/livestream"
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

	// Bucket Key-Value untuk anti-dobel views, juga dibuat di sini.
	setupCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	views, err := video.NewNATSViewDeduper(setupCtx, js, cfg.ViewDedupWindow)
	cancel()
	if err != nil {
		return err
	}

	repo := video.NewOracleRepository(db)
	svc := video.NewService(repo, store, video.NewNATSPublisher(js), views, video.Options{
		MaxUploadBytes: cfg.MaxUploadBytes,
		UploadURLTTL:   cfg.UploadURLTTL,
		PlaybackURLTTL: cfg.PlaybackURLTTL,
		MediaBaseURL:   cfg.MediaPublicURL,
	})

	// Video yang dibuat sebelum fitur pencarian ada belum punya kata
	// pencarian. Diindeks di background supaya service tidak menunggu.
	go func() {
		n, err := svc.IndexPending(ctx)
		if err != nil {
			logger.Error("gagal mengindeks video untuk pencarian", slog.Any("error", err))
			return
		}
		if n > 0 {
			logger.Info("video lama selesai diindeks untuk pencarian", slog.Int("jumlah", n))
		}
	}()

	// File video yang dihapus tapi belum terhapus dari storage (misalnya
	// storage mati saat DeleteVideo) dibersihkan di background.
	go func() {
		n, err := svc.PurgeDeleted(ctx)
		if err != nil {
			logger.Error("gagal menghapus file video yang sudah dihapus", slog.Any("error", err))
			return
		}
		if n > 0 {
			logger.Info("file video yang sudah dihapus selesai dibersihkan", slog.Int("jumlah", n))
		}
	}()

	// Terima laporan status dari transcoder (PROCESSING, READY, FAILED).
	consumer, err := video.ConsumeTranscodeEvents(ctx, js, svc, logger)
	if err != nil {
		return err
	}
	defer consumer.Stop()

	// Notifikasi video baru: daftar subscriber diminta ke UserService
	// (proses AuthService). Kalau AuthService sedang mati, VideoService tetap
	// berjalan normal; hanya notifikasinya yang tertunda dan dicoba lagi.
	authConn, err := grpcserver.NewClient(cfg.AuthGRPCAddr)
	if err != nil {
		return fmt.Errorf("menyiapkan klien auth %s: %w", cfg.AuthGRPCAddr, err)
	}
	defer authConn.Close()
	notifier := video.NewNotifier(repo, video.NewGRPCSubscriberSource(userv1.NewUserServiceClient(authConn)))
	notifyConsumer, err := video.ConsumeNewVideos(ctx, js, notifier, logger)
	if err != nil {
		return err
	}
	defer notifyConsumer.Stop()

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.LogUnary(logger)))
	videov1.RegisterVideoServiceServer(srv, video.NewGRPCServer(svc, logger))
	// CommentService berjalan di proses yang sama (port 50052) dan memakai
	// koneksi Oracle yang sama, karena tabel comments ada di schema VIDEO_SVC.
	comments := comment.NewService(comment.NewOracleRepository(db))
	commentv1.RegisterCommentServiceServer(srv, comment.NewGRPCServer(comments, logger))
	// DanmakuService juga di proses ini: tabel danmaku ada di VIDEO_SVC, dan
	// danmaku baru disiarkan lewat koneksi NATS yang sama (core NATS).
	danmakus := danmaku.NewService(danmaku.NewOracleRepository(db), danmaku.NewNATSPublisher(nc), logger)
	danmakuv1.RegisterDanmakuServiceServer(srv, danmaku.NewGRPCServer(danmakus, logger))

	// LiveService (langkah 25a): stream key dan status siaran. Videonya
	// sendiri diurus MediaMTX; di sini hanya hook auth dan pembacaan status.
	// Langkah 25b-25c: token siaran browser dan chat (disiarkan lewat koneksi
	// NATS yang sama dengan danmaku).
	lives := livestream.NewService(livestream.NewOracleRepository(db), livestream.Options{
		IngestURL:       cfg.Live.IngestURL,
		PlaybackBaseURL: cfg.Live.PlaybackURL,
		Grace:           cfg.Live.Grace,
		WHIPBaseURL:     cfg.Live.WHIPURL,
		PublishTokenTTL: cfg.Live.PublishTokenTTL,
		Publisher:       livestream.NewNATSPublisher(nc),
		Logger:          logger,
	})
	livev1.RegisterLiveServiceServer(srv, livestream.NewGRPCServer(lives, logger))
	hookSrv := &http.Server{
		Addr:              cfg.Live.HookAddr,
		Handler:           livestream.HookHandler(lives, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go func() {
		if err := hookSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server hook MediaMTX berhenti", slog.Any("error", err))
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = hookSrv.Shutdown(shutdownCtx)
	}()
	go livestream.Syncer{
		Source:   livestream.MediaMTXClient{BaseURL: cfg.Live.MediaMTXAPIURL, HTTP: &http.Client{Timeout: cfg.Live.PollInterval}},
		Service:  lives,
		Interval: cfg.Live.PollInterval,
		Logger:   logger,
	}.Run(ctx)

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
		slog.String("view_dedup_window", cfg.ViewDedupWindow.String()),
		slog.String("auth_addr", cfg.AuthGRPCAddr),
		slog.String("mediamtx_api", cfg.Live.MediaMTXAPIURL),
		slog.String("live_hook_addr", cfg.Live.HookAddr),
		slog.String("live_whip_url", cfg.Live.WHIPURL),
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
