package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Kosongkan env agar test tidak terpengaruh isi terminal kamu.
	for _, key := range []string{
		"APP_ENV", "HTTP_PORT", "CORS_ALLOWED_ORIGINS", "SHUTDOWN_TIMEOUT",
		"DB_HOST", "DB_PORT", "DB_SERVICE", "DB_USER", "DB_MAX_OPEN_CONNS",
	} {
		t.Setenv(key, "")
	}
	// DB_PASSWORD tidak punya default, jadi wajib diisi.
	t.Setenv("DB_PASSWORD", "rahasia")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, []string{"http://localhost:5173"}, cfg.CORSAllowedOrigins)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, DBConfig{
		Host: "localhost", Port: 1521, Service: "FREEPDB1",
		User: "auth_svc", Password: "rahasia", MaxOpenConns: 10,
	}, cfg.DB)
}

func TestLoadFromEnv(t *testing.T) {
	// t.Setenv otomatis mengembalikan nilai lama setelah test selesai.
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_PORT", "4000")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://ice-stream.vercel.app, http://localhost:5173")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("DB_HOST", "oracle.ice-stream.svc.cluster.local")
	t.Setenv("DB_PORT", "1522")
	t.Setenv("DB_SERVICE", "OTHERPDB")
	t.Setenv("DB_USER", "video_svc")
	t.Setenv("DB_PASSWORD", "rahasia")
	t.Setenv("DB_MAX_OPEN_CONNS", "20")

	cfg, err := Load()
	require.NoError(t, err)

	assert.True(t, cfg.IsProduction())
	assert.Equal(t, ":4000", cfg.HTTPAddr)
	assert.Equal(t, []string{"https://ice-stream.vercel.app", "http://localhost:5173"}, cfg.CORSAllowedOrigins)
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, DBConfig{
		Host: "oracle.ice-stream.svc.cluster.local", Port: 1522, Service: "OTHERPDB",
		User: "video_svc", Password: "rahasia", MaxOpenConns: 20,
	}, cfg.DB)
}

func TestLoadInvalid(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"APP_ENV salah":           {"APP_ENV", "staging"},
		"HTTP_PORT bukan angka":   {"HTTP_PORT", "abc"},
		"HTTP_PORT di luar batas": {"HTTP_PORT", "70000"},
		"SHUTDOWN_TIMEOUT salah":  {"SHUTDOWN_TIMEOUT", "sepuluh"},
		"DB_PORT bukan angka":     {"DB_PORT", "abc"},
		"DB_PORT di luar batas":   {"DB_PORT", "0"},
		"DB_MAX_OPEN_CONNS nol":   {"DB_MAX_OPEN_CONNS", "0"},
		"DB_PASSWORD kosong":      {"DB_PASSWORD", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Isi password dulu supaya hanya tt.key yang membuat Load gagal.
			t.Setenv("DB_PASSWORD", "rahasia")
			t.Setenv(tt.key, tt.value)
			_, err := Load()
			assert.Error(t, err)
		})
	}
}
