// Package metrics menyiapkan metrik Prometheus untuk semua service
// Ice-Stream (langkah 28): jumlah dan durasi request HTTP di gateway,
// jumlah dan durasi RPC di service gRPC, plus metrik bawaan Go (goroutine,
// memori, GC) dan proses (CPU, file descriptor).
//
// Metrik dilayani di port terpisah (METRICS_PORT), bukan di port API,
// supaya /metrics tidak ikut terbuka ke internet lewat Gateway API.
// Prometheus mengambil (scrape) angka-angka ini secara berkala.
//
// Padanannya: prom-client + express-prom-bundle di Node.js, Micrometer +
// Spring Boot Actuator (/actuator/prometheus) di Java.
// Dokumentasi: https://pkg.go.dev/github.com/prometheus/client_golang/prometheus
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// NewRegistry membuat registry baru berisi metrik Go dan proses. Registry
// sendiri (bukan prometheus.DefaultRegisterer) membuat test tidak saling
// bentrok karena metrik yang sama didaftarkan dua kali.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

// Handler melayani GET /metrics dalam format teks Prometheus.
func Handler(reg *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	return mux
}

// Serve menjalankan server /metrics di addr sampai ctx selesai. Error saat
// start hanya dicatat: service tetap jalan walau metriknya tidak bisa
// diambil.
func Serve(ctx context.Context, addr string, reg *prometheus.Registry, logger *slog.Logger) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(reg),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Info("server metrik berjalan", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server metrik berhenti", slog.Any("error", err))
	}
}

// Bucket durasi (detik): dari 5 ms sampai 10 detik. Upload thumbnail dan
// panggilan ke Oracle biasanya di bawah 1 detik.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// HTTP mencatat request di API gateway.
type HTTP struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewHTTP mendaftarkan metrik HTTP ke reg.
func NewHTTP(reg prometheus.Registerer) *HTTP {
	m := &HTTP{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "icestream_http_requests_total",
			Help: "Jumlah request HTTP per route dan kode status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "icestream_http_request_duration_seconds",
			Help:    "Lama request HTTP per route.",
			Buckets: durationBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// Middleware mencatat setiap request. Label route memakai pola Gin
// (misalnya /api/v1/videos/:id), bukan URL asli, supaya jumlah deret
// waktunya tidak meledak (setiap id video menjadi deret baru).
// Koneksi SSE (danmaku/chat live) ikut tercatat dengan durasi selama
// koneksinya terbuka.
func (m *HTTP) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched" // 404: jangan pakai URL asli sebagai label
		}
		method := c.Request.Method
		m.requests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
		m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	}
}

// GRPC mencatat RPC di service gRPC (AuthService, VideoService, dan
// service lain di proses yang sama).
type GRPC struct {
	handled  *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewGRPC mendaftarkan metrik gRPC ke reg.
func NewGRPC(reg prometheus.Registerer) *GRPC {
	m := &GRPC{
		handled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "icestream_grpc_server_handled_total",
			Help: "Jumlah RPC yang selesai per method dan kode hasil.",
		}, []string{"method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "icestream_grpc_server_handling_seconds",
			Help:    "Lama RPC per method.",
			Buckets: durationBuckets,
		}, []string{"method"}),
	}
	reg.MustRegister(m.handled, m.duration)
	return m
}

// Unary adalah interceptor gRPC (= middleware) yang mencatat setiap RPC.
// method berbentuk "/video.v1.VideoService/GetVideo".
func (m *GRPC) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		m.handled.WithLabelValues(info.FullMethod, status.Code(err).String()).Inc()
		m.duration.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
		return resp, err
	}
}
