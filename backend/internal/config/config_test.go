package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Kosongkan env agar test tidak terpengaruh isi terminal kamu.
	for _, key := range []string{"APP_ENV", "HTTP_PORT", "CORS_ALLOWED_ORIGINS", "SHUTDOWN_TIMEOUT"} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, []string{"http://localhost:5173"}, cfg.CORSAllowedOrigins)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
}

func TestLoadFromEnv(t *testing.T) {
	// t.Setenv otomatis mengembalikan nilai lama setelah test selesai.
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_PORT", "4000")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://ice-stream.vercel.app, http://localhost:5173")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")

	cfg, err := Load()
	require.NoError(t, err)

	assert.True(t, cfg.IsProduction())
	assert.Equal(t, ":4000", cfg.HTTPAddr)
	assert.Equal(t, []string{"https://ice-stream.vercel.app", "http://localhost:5173"}, cfg.CORSAllowedOrigins)
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
}

func TestLoadInvalid(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"APP_ENV salah":           {"APP_ENV", "staging"},
		"HTTP_PORT bukan angka":   {"HTTP_PORT", "abc"},
		"HTTP_PORT di luar batas": {"HTTP_PORT", "70000"},
		"SHUTDOWN_TIMEOUT salah":  {"SHUTDOWN_TIMEOUT", "sepuluh"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			_, err := Load()
			assert.Error(t, err)
		})
	}
}
