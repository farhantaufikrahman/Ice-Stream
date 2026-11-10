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

	"github.com/nats-io/nats.go"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/grpcserver"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/httpserver"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
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

	authConn, err := grpcserver.NewClient(cfg.AuthGRPCAddr)
	if err != nil {
		return fmt.Errorf("menyiapkan klien auth %s: %w", cfg.AuthGRPCAddr, err)
	}
	defer authConn.Close()

	videoConn, err := grpcserver.NewClient(cfg.VideoGRPCAddr)
	if err != nil {
		return fmt.Errorf("menyiapkan klien video %s: %w", cfg.VideoGRPCAddr, err)
	}
	defer videoConn.Close()

	// NATS dipakai untuk menerima siaran danmaku. RetryOnFailedConnect: kalau
	// NATS belum siap saat gateway start, gateway tetap jalan dan terus
	// mencoba menyambung di background. Selama itu hanya danmaku live yang
	// tidak sampai; login, video, dan komentar tetap normal.
	nc, err := events.Connect(cfg.NATSURL, "api-gateway", nats.RetryOnFailedConnect(true))
	if err != nil {
		return err
	}
	defer nc.Close()
	hub := live.NewHub(live.NewNATSBroker(nc), live.Options{
		Subject:    events.SubjectDanmakuLive,
		Transform:  httpserver.EncodeLiveDanmaku,
		MaxClients: cfg.LiveMaxClients,
	}, logger)

	// Gateway tidak terhubung ke Oracle. Yang dicek hanya AuthService, karena
	// tanpa auth hampir semua fitur tidak bisa dipakai. VideoService sengaja
	// tidak dicek: kalau video mati, login dan halaman lain tetap jalan, dan
	// endpoint video menjawab 503 sendiri.
	checks := map[string]httpserver.ReadinessCheck{
		"auth": grpcHealthCheck(healthpb.NewHealthClient(authConn), authv1.AuthService_ServiceDesc.ServiceName),
	}

	router, err := httpserver.NewRouter(cfg, logger, httpserver.Deps{
		Checks: checks,
		Auth:   authv1.NewAuthServiceClient(authConn),
		Videos: videov1.NewVideoServiceClient(videoConn),
		// UserService berjalan di proses AuthService, jadi memakai koneksi yang sama.
		Users: userv1.NewUserServiceClient(authConn),
		// CommentService berjalan di proses VideoService, jadi memakai koneksi video.
		Comments: commentv1.NewCommentServiceClient(videoConn),
		// DanmakuService juga berjalan di proses VideoService.
		Danmaku:  danmakuv1.NewDanmakuServiceClient(videoConn),
		Live:     hub,
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

	// Shutdown menunggu semua request selesai, padahal koneksi SSE tidak
	// pernah selesai sendiri. hub.Close memutus semuanya begitu Shutdown
	// dimulai (browser lalu menyambung ulang ke pod lain).
	srv.RegisterOnShutdown(hub.Close)

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
