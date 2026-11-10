package config

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Kosongkan env agar test tidak terpengaruh isi terminal kamu.
	for _, key := range []string{
		"APP_ENV", "HTTP_PORT", "CORS_ALLOWED_ORIGINS", "SHUTDOWN_TIMEOUT",
		"TRUSTED_PROXIES", "AUTH_GRPC_ADDR", "VIDEO_GRPC_ADDR", "COOKIE_SECURE", "COOKIE_SAMESITE",
	} {
		t.Setenv(key, "")
	}
	// JWT_PUBLIC_KEY_FILE tidak punya default, jadi wajib diisi.
	t.Setenv("JWT_PUBLIC_KEY_FILE", "/secrets/jwt-public.pem")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, []string{"http://localhost:5173"}, cfg.CORSAllowedOrigins)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.Empty(t, cfg.TrustedProxies)
	assert.Equal(t, "localhost:50051", cfg.AuthGRPCAddr)
	assert.Equal(t, "localhost:50052", cfg.VideoGRPCAddr)
	assert.Equal(t, "/secrets/jwt-public.pem", cfg.JWTPublicKeyFile)
	// Di development cookie tidak Secure, karena localhost memakai http.
	assert.Equal(t, CookieConfig{Secure: false, SameSite: http.SameSiteLaxMode}, cfg.Cookie)
}

func TestLoadFromEnv(t *testing.T) {
	// t.Setenv otomatis mengembalikan nilai lama setelah test selesai.
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_PORT", "4000")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://ice-stream.vercel.app, http://localhost:5173")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")
	t.Setenv("AUTH_GRPC_ADDR", "auth:50051")
	t.Setenv("VIDEO_GRPC_ADDR", "video:50052")
	t.Setenv("JWT_PUBLIC_KEY_FILE", "/etc/ice-stream/jwt/jwt-public.pem")
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("COOKIE_SAMESITE", "None")

	cfg, err := Load()
	require.NoError(t, err)

	assert.True(t, cfg.IsProduction())
	assert.Equal(t, ":4000", cfg.HTTPAddr)
	assert.Equal(t, []string{"https://ice-stream.vercel.app", "http://localhost:5173"}, cfg.CORSAllowedOrigins)
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, []string{"10.0.0.0/8"}, cfg.TrustedProxies)
	assert.Equal(t, "auth:50051", cfg.AuthGRPCAddr)
	assert.Equal(t, "video:50052", cfg.VideoGRPCAddr)
	assert.Equal(t, "/etc/ice-stream/jwt/jwt-public.pem", cfg.JWTPublicKeyFile)
	// Di production Secure otomatis true.
	assert.Equal(t, CookieConfig{Secure: true, SameSite: http.SameSiteNoneMode}, cfg.Cookie)
}

func TestLoadInvalid(t *testing.T) {
	tests := map[string]map[string]string{
		"APP_ENV salah":               {"APP_ENV": "staging"},
		"HTTP_PORT bukan angka":       {"HTTP_PORT": "abc"},
		"HTTP_PORT di luar batas":     {"HTTP_PORT": "70000"},
		"SHUTDOWN_TIMEOUT salah":      {"SHUTDOWN_TIMEOUT": "sepuluh"},
		"JWT_PUBLIC_KEY_FILE kosong":  {"JWT_PUBLIC_KEY_FILE": ""},
		"COOKIE_SECURE bukan boolean": {"COOKIE_SECURE": "ya"},
		"COOKIE_SAMESITE salah":       {"COOKIE_SAMESITE": "kadang"},
		"SameSite none tanpa Secure":  {"COOKIE_SAMESITE": "none", "COOKIE_SECURE": "false"},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			// Isi nilai wajib dulu supaya hanya env di atas yang membuat Load gagal.
			t.Setenv("JWT_PUBLIC_KEY_FILE", "/secrets/jwt-public.pem")
			for k, v := range env {
				t.Setenv(k, v)
			}
			_, err := Load()
			assert.Error(t, err)
		})
	}
}
