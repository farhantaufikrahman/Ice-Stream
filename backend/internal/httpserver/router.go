// Package httpserver menyusun router Gin untuk API gateway Ice-Stream.
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
)

// ReadinessCheck memeriksa satu dependensi (Oracle, NATS, dan seterusnya).
// Kembalikan error jika dependensi belum siap menerima traffic.
type ReadinessCheck func(ctx context.Context) error

// NewRouter membuat router dengan middleware dan route dasar.
// checks berisi pemeriksaan untuk /readyz; boleh kosong di tahap awal.
func NewRouter(cfg config.Config, logger *slog.Logger, checks map[string]ReadinessCheck) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORSAllowedOrigins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true, // dibutuhkan untuk cookie refresh token
		MaxAge:           12 * time.Hour,
	}))

	// Probe Kubernetes ada di luar /api/v1, sesuai rancangan.
	r.GET("/healthz", healthz)
	r.GET("/readyz", readyz(checks))

	v1 := r.Group("/api/v1")
	v1.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	return r
}

// healthz (liveness) hanya menjawab "proses ini hidup".
// Jangan cek database di sini: kalau Oracle mati, Kubernetes akan
// me-restart pod terus-menerus padahal pod-nya sendiri sehat.
func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// readyz (readiness) menjawab "siap menerima traffic". Jika satu dependensi
// gagal, Kubernetes berhenti mengirim request ke pod ini sampai pulih.
func readyz(checks map[string]ReadinessCheck) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		results := make(map[string]string, len(checks))
		status := http.StatusOK
		for name, check := range checks {
			if err := check(ctx); err != nil {
				results[name] = err.Error()
				status = http.StatusServiceUnavailable
				continue
			}
			results[name] = "ok"
		}

		c.JSON(status, gin.H{"status": http.StatusText(status), "checks": results})
	}
}

// requestLogger mencatat setiap request dalam format log terstruktur.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.FullPath()),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}
