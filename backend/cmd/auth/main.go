// Command auth menjalankan AuthService (gRPC) Ice-Stream.
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

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/auth"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/gormdb"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/grpcserver"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/mail"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/metrics"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/user"
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

	authRepo := auth.NewOracleRepository(db)
	svc, err := auth.NewService(authRepo, auth.NewTokenMaker(key, cfg.AccessTokenTTL), cfg.RefreshTokenTTL)
	if err != nil {
		return err
	}
	// "Masuk dengan Google / GitHub" (langkah 26). Protokol OAuth-nya ada
	// di gateway; AuthService hanya menyimpan identitas dan kode login.
	svc.EnableIdentityLogin(authRepo)

	// "Lupa kata sandi?" (langkah 24c). Email dikirim di background; saat
	// service dimatikan, email yang sedang dikirim ditunggu paling lama
	// SHUTDOWN_TIMEOUT.
	if err := svc.EnablePasswordReset(auth.ResetConfig{
		Repo:    authRepo,
		Mailer:  mail.SMTPSender{Addr: cfg.Mail.SMTPAddr, From: cfg.Mail.From, Timeout: 10 * time.Second},
		BaseURL: cfg.Mail.AppBaseURL,
		TTL:     cfg.Mail.PasswordResetTTL,
		Logger:  logger,
	}); err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		svc.Close(closeCtx)
	}()

	// Bucket images untuk foto profil disiapkan di background. Berbeda dengan
	// VideoService, AuthService TIDAK boleh berhenti hanya karena storage
	// belum siap: login dan register tidak butuh storage. Selama bucket
	// belum siap, hanya upload foto profil yang gagal.
	store := user.NewS3Store(cfg.Storage)
	go ensureAvatarBucket(ctx, store, logger)
	users := user.NewService(user.NewOracleRepository(db), store, logger, cfg.MediaPublicURL)

	// Metrik Prometheus (langkah 28) di port terpisah (METRICS_PORT).
	reg := metrics.NewRegistry()
	go metrics.Serve(ctx, cfg.MetricsAddr, reg, logger)
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.LogUnary(logger), metrics.NewGRPC(reg).Unary()))
	authv1.RegisterAuthServiceServer(srv, auth.NewGRPCServer(svc, logger))
	// UserService berjalan di proses yang sama (port 50051). Satu server gRPC
	// boleh melayani beberapa service, mirip satu aplikasi Spring Boot dengan
	// beberapa controller.
	userv1.RegisterUserServiceServer(srv, user.NewGRPCServer(users, logger))

	// Health check standar gRPC, dipakai probe Kubernetes dan grpcurl.
	// Statusnya NOT_SERVING selama Oracle tidak bisa dihubungi.
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	go grpcserver.WatchHealth(ctx, healthSrv, authv1.AuthService_ServiceDesc.ServiceName, []grpcserver.Check{
		// Storage sengaja tidak dicek di sini, alasannya sama seperti di atas.
		{Name: "oracle", Run: sqlDB.PingContext},
	}, logger)

	// Reflection membuat grpcurl bisa melihat daftar RPC tanpa file .proto.
	// Hanya dinyalakan di development.
	if !cfg.IsProduction() {
		reflection.Register(srv)
	}

	logger.Info("auth service siap",
		slog.String("env", cfg.Env),
		slog.String("avatar_bucket", cfg.Storage.Bucket),
		slog.String("media_url", cfg.MediaPublicURL),
		slog.String("smtp_addr", cfg.Mail.SMTPAddr),
		slog.String("app_base_url", cfg.Mail.AppBaseURL),
	)
	return grpcserver.Serve(ctx, srv, healthSrv, cfg.GRPCAddr, cfg.ShutdownTimeout, logger)
}

// ensureAvatarBucket membuat bucket foto profil, dan mencoba lagi setiap 10
// detik sampai berhasil (misalnya saat RustFS masih start).
func ensureAvatarBucket(ctx context.Context, store *user.S3Store, logger *slog.Logger) {
	for {
		setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := store.EnsureBucket(setupCtx)
		cancel()
		if err == nil {
			logger.Info("bucket foto profil siap")
			return
		}
		logger.Warn("bucket foto profil belum siap, dicoba lagi 10 detik lagi", slog.Any("error", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}
