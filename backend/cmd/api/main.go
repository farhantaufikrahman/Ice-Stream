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

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.NewRouter(cfg, logger, nil),
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
