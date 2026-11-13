// Package grpcserver berisi bagian yang sama di semua service gRPC
// Ice-Stream (AuthService, VideoService): log per RPC, status health, dan
// cara mematikan server dengan rapi, serta cara membuat klien gRPC.
package grpcserver

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// Check adalah satu pemeriksaan dependensi, misalnya ping ke Oracle.
type Check struct {
	Name string
	Run  func(context.Context) error
}

// WatchHealth menjalankan semua checks tiap 5 detik dan memperbarui status
// health untuk server ("") dan untuk serviceName. Jika satu saja gagal,
// statusnya NOT_SERVING, sehingga Kubernetes berhenti mengirim traffic ke
// pod ini (mirip /readyz di gateway). Berhenti saat ctx selesai.
func WatchHealth(ctx context.Context, h *health.Server, serviceName string, checks []Check, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := healthpb.HealthCheckResponse_UNKNOWN
	for {
		next := healthpb.HealthCheckResponse_SERVING
		var failed []any
		for _, c := range checks {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := c.Run(checkCtx)
			cancel()
			if err != nil {
				next = healthpb.HealthCheckResponse_NOT_SERVING
				failed = append(failed, slog.String(c.Name, err.Error()))
			}
		}
		if next != last {
			h.SetServingStatus("", next)
			h.SetServingStatus(serviceName, next)
			logger.Info("status health berubah", slog.String("status", next.String()), slog.Group("gagal", failed...))
			last = next
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// LogUnary mencatat setiap RPC: nama method, kode hasil, dan durasinya.
// Interceptor gRPC = middleware di Gin/Express.
func LogUnary(logger *slog.Logger) grpc.UnaryServerInterceptor {
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

// Serve menjalankan srv di addr sampai ctx selesai (SIGTERM/Ctrl+C), lalu
// mematikannya dengan rapi: health menjadi NOT_SERVING, RPC yang sedang
// berjalan ditunggu sampai shutdownTimeout, sisanya diputus paksa.
func Serve(ctx context.Context, srv *grpc.Server, h *health.Server, addr string, shutdownTimeout time.Duration, logger *slog.Logger) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server gRPC berjalan", slog.String("addr", addr))
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

	h.Shutdown() // semua status menjadi NOT_SERVING
	stopped := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		logger.Info("server berhenti dengan rapi")
	case <-time.After(shutdownTimeout):
		srv.Stop()
		logger.Warn("batas waktu shutdown habis, koneksi diputus paksa")
	}
	return nil
}
