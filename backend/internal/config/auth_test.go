package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAuthDefaults(t *testing.T) {
	for _, key := range []string{
		"APP_ENV", "GRPC_PORT", "METRICS_PORT", "SHUTDOWN_TIMEOUT", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL",
		"DB_HOST", "DB_PORT", "DB_SERVICE", "DB_USER", "DB_MAX_OPEN_CONNS",
		"STORAGE_ENDPOINT", "STORAGE_PUBLIC_ENDPOINT", "STORAGE_BUCKET", "MEDIA_PUBLIC_URL",
		"SMTP_ADDR", "MAIL_FROM", "APP_BASE_URL", "PASSWORD_RESET_TTL",
		"SMTP_TLS", "SMTP_USERNAME", "SMTP_PASSWORD",
	} {
		t.Setenv(key, "")
	}
	setAuthRequired(t)

	cfg, err := LoadAuth()
	require.NoError(t, err)

	assert.Equal(t, ":50051", cfg.GRPCAddr)
	assert.Equal(t, ":9091", cfg.MetricsAddr)
	assert.Equal(t, 15*time.Minute, cfg.AccessTokenTTL)
	assert.Equal(t, 30*24*time.Hour, cfg.RefreshTokenTTL)
	assert.Equal(t, "/secrets/jwt-private.pem", cfg.JWTPrivateKeyFile)
	assert.Equal(t, "auth_svc", cfg.DB.User)
	// Foto profil disimpan di bucket images, terpisah dari bucket videos.
	assert.Equal(t, "images", cfg.Storage.Bucket)
	assert.Equal(t, "http://localhost:9000/images", cfg.MediaPublicURL)
	// Tanpa pengaturan, email dikirim ke Mailpit lokal dan tautan menuju
	// Vite dev server.
	assert.Equal(t, MailConfig{
		SMTPAddr:         "localhost:1025",
		SMTPTLS:          "none",
		From:             "Ice-Stream <no-reply@ice-stream.localhost>",
		AppBaseURL:       "http://localhost:5173",
		PasswordResetTTL: 30 * time.Minute,
	}, cfg.Mail)
}

// Langkah 29: Resend (TLS port 465 + login).
func TestLoadAuthResend(t *testing.T) {
	setAuthRequired(t)
	t.Setenv("SMTP_ADDR", "smtp.resend.com:465")
	t.Setenv("SMTP_TLS", "TLS")
	t.Setenv("SMTP_USERNAME", "resend")
	t.Setenv("SMTP_PASSWORD", "re_rahasia")
	t.Setenv("MAIL_FROM", "Ice-Stream <no-reply@vzkyrielight.my.id>")

	cfg, err := LoadAuth()
	require.NoError(t, err)
	assert.Equal(t, "tls", cfg.Mail.SMTPTLS)
	assert.Equal(t, "resend", cfg.Mail.SMTPUsername)
	assert.Equal(t, "re_rahasia", cfg.Mail.SMTPPassword)
}

// setAuthRequired mengisi env yang wajib ada.
func setAuthRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PASSWORD", "rahasia")
	t.Setenv("JWT_PRIVATE_KEY_FILE", "/secrets/jwt-private.pem")
	t.Setenv("STORAGE_ACCESS_KEY", "kunci-storage")
	t.Setenv("STORAGE_SECRET_KEY", "rahasia-storage")
}

func TestLoadAuthSMTPPasswordRequired(t *testing.T) {
	setAuthRequired(t)
	t.Setenv("SMTP_TLS", "tls")
	t.Setenv("SMTP_USERNAME", "resend")
	t.Setenv("SMTP_PASSWORD", "")
	_, err := LoadAuth()
	assert.ErrorContains(t, err, "SMTP_PASSWORD")
}

func TestLoadAuthInvalid(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"GRPC_PORT salah":               {"GRPC_PORT", "abc"},
		"METRICS_PORT bentrok":          {"METRICS_PORT", "50051"},
		"JWT_PRIVATE_KEY_FILE kosong":   {"JWT_PRIVATE_KEY_FILE", ""},
		"ACCESS_TOKEN_TTL terlalu lama": {"ACCESS_TOKEN_TTL", "2h"},
		"REFRESH lebih pendek":          {"REFRESH_TOKEN_TTL", "10m"},
		"DB_PASSWORD kosong":            {"DB_PASSWORD", ""},
		"DB_PORT bukan angka":           {"DB_PORT", "abc"},
		"DB_PORT di luar batas":         {"DB_PORT", "0"},
		"DB_MAX_OPEN_CONNS nol":         {"DB_MAX_OPEN_CONNS", "0"},
		"STORAGE_ACCESS_KEY kosong":     {"STORAGE_ACCESS_KEY", ""},
		"MEDIA_PUBLIC_URL relatif":      {"MEDIA_PUBLIC_URL", "/images"},
		"SMTP_ADDR tanpa port":          {"SMTP_ADDR", "mailpit"},
		"MAIL_FROM bukan email":         {"MAIL_FROM", "Ice-Stream"},
		"APP_BASE_URL tanpa skema":      {"APP_BASE_URL", "ice-stream.localhost:4000"},
		"APP_BASE_URL dengan path":      {"APP_BASE_URL", "http://ice-stream.localhost:4000/app"},
		"PASSWORD_RESET_TTL kependekan": {"PASSWORD_RESET_TTL", "1m"},
		"SMTP_TLS salah":                {"SMTP_TLS", "ssl"},
		"SMTP login tanpa TLS":          {"SMTP_USERNAME", "resend"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			setAuthRequired(t)
			t.Setenv("SMTP_TLS", "")
			t.Setenv("SMTP_USERNAME", "")
			t.Setenv("SMTP_PASSWORD", "")
			t.Setenv(tt.key, tt.value)
			_, err := LoadAuth()
			assert.Error(t, err)
		})
	}
}
