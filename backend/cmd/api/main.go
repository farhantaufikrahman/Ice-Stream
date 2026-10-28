// Command api menjalankan API gateway Ice-Stream (REST/JSON untuk React).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/database"
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

	db, err := database.OpenOracle(cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()

	// Cek sekali saat start hanya untuk log. Kalau gagal, server tetap jalan
	// dan /readyz menjawab 503 sampai Oracle siap.
	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	if err := db.PingContext(pingCtx); err != nil {
		logger.Warn("oracle belum bisa dihubungi", slog.Any("error", err))
	} else {
		logger.Info("terhubung ke oracle", slog.String("host", cfg.DB.Host), slog.String("service", cfg.DB.Service))
	}
	cancelPing()

	checks := map[string]httpserver.ReadinessCheck{
		"oracle": db.PingContext,
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.NewRouter(cfg, logger, checks),
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
