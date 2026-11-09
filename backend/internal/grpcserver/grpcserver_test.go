package grpcserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestWatchHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var storageDown atomic.Bool
	storageDown.Store(true)
	checks := []Check{
		{Name: "oracle", Run: func(context.Context) error { return nil }},
		{Name: "storage", Run: func(context.Context) error {
			if storageDown.Load() {
				return errors.New("connection refused")
			}
			return nil
		}},
	}
	h := health.NewServer()
	go WatchHealth(ctx, h, "video.v1.VideoService", checks, slog.New(slog.NewTextHandler(io.Discard, nil)))

	status := func() healthpb.HealthCheckResponse_ServingStatus {
		resp, err := h.Check(ctx, &healthpb.HealthCheckRequest{Service: "video.v1.VideoService"})
		if err != nil {
			return healthpb.HealthCheckResponse_UNKNOWN
		}
		return resp.GetStatus()
	}

	// Satu dependensi mati = NOT_SERVING.
	require.Eventually(t, func() bool { return status() == healthpb.HealthCheckResponse_NOT_SERVING }, time.Second, 10*time.Millisecond)

	// Setelah pulih, status menjadi SERVING pada pengecekan berikutnya (5 detik).
	storageDown.Store(false)
	assert.Eventually(t, func() bool { return status() == healthpb.HealthCheckResponse_SERVING }, 7*time.Second, 50*time.Millisecond)
}
