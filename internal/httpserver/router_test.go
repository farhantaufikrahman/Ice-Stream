package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/farhantaufikrahman/ice-stream/internal/config"
)

func newTestRouter(t *testing.T, checks map[string]ReadinessCheck) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		Env:                "development",
		CORSAllowedOrigins: []string{"http://localhost:5173"},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(cfg, logger, checks)
}

func TestHealthz(t *testing.T) {
	r := newTestRouter(t, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		checks     map[string]ReadinessCheck
		wantStatus int
	}{
		{
			name:       "tanpa dependensi dianggap siap",
			checks:     nil,
			wantStatus: http.StatusOK,
		},
		{
			name: "semua dependensi sehat",
			checks: map[string]ReadinessCheck{
				"oracle": func(context.Context) error { return nil },
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "satu dependensi gagal",
			checks: map[string]ReadinessCheck{
				"oracle": func(context.Context) error { return nil },
				"nats":   func(context.Context) error { return errors.New("connection refused") },
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRouter(t, tt.checks)

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestCORS(t *testing.T) {
	r := newTestRouter(t, nil)

	t.Run("origin yang diizinkan mendapat header CORS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("origin asing ditolak", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Origin", "https://situs-asing.com")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})
}
