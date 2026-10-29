package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAuthDefaults(t *testing.T) {
	for _, key := range []string{
		"APP_ENV", "GRPC_PORT", "SHUTDOWN_TIMEOUT", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL",
		"DB_HOST", "DB_PORT", "DB_SERVICE", "DB_USER", "DB_MAX_OPEN_CONNS",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("DB_PASSWORD", "rahasia")
	t.Setenv("JWT_PRIVATE_KEY_FILE", "/secrets/jwt-private.pem")

	cfg, err := LoadAuth()
	require.NoError(t, err)

	assert.Equal(t, ":50051", cfg.GRPCAddr)
	assert.Equal(t, 15*time.Minute, cfg.AccessTokenTTL)
	assert.Equal(t, 30*24*time.Hour, cfg.RefreshTokenTTL)
	assert.Equal(t, "/secrets/jwt-private.pem", cfg.JWTPrivateKeyFile)
	assert.Equal(t, "auth_svc", cfg.DB.User)
}

func TestLoadAuthInvalid(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"GRPC_PORT salah":               {"GRPC_PORT", "abc"},
		"JWT_PRIVATE_KEY_FILE kosong":   {"JWT_PRIVATE_KEY_FILE", ""},
		"ACCESS_TOKEN_TTL terlalu lama": {"ACCESS_TOKEN_TTL", "2h"},
		"REFRESH lebih pendek":          {"REFRESH_TOKEN_TTL", "10m"},
		"DB_PASSWORD kosong":            {"DB_PASSWORD", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DB_PASSWORD", "rahasia")
			t.Setenv("JWT_PRIVATE_KEY_FILE", "/secrets/jwt-private.pem")
			t.Setenv(tt.key, tt.value)
			_, err := LoadAuth()
			assert.Error(t, err)
		})
	}
}
