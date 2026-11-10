package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setVideoRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PASSWORD", "rahasia")
	t.Setenv("STORAGE_ACCESS_KEY", "kunci")
	t.Setenv("STORAGE_SECRET_KEY", "rahasia-storage")
}

func TestLoadVideoDefaults(t *testing.T) {
	for _, key := range []string{
		"APP_ENV", "GRPC_PORT", "SHUTDOWN_TIMEOUT", "DB_USER", "STORAGE_ENDPOINT",
		"STORAGE_PUBLIC_ENDPOINT", "STORAGE_REGION", "STORAGE_BUCKET", "MAX_UPLOAD_MB",
		"UPLOAD_URL_TTL", "PLAYBACK_URL_TTL", "CORS_ALLOWED_ORIGINS", "NATS_URL", "MEDIA_PUBLIC_URL",
		"VIEW_DEDUP_WINDOW", "AUTH_GRPC_ADDR",
	} {
		t.Setenv(key, "")
	}
	setVideoRequired(t)

	cfg, err := LoadVideo()
	require.NoError(t, err)

	assert.Equal(t, ":50052", cfg.GRPCAddr)
	assert.Equal(t, "video_svc", cfg.DB.User)
	assert.Equal(t, "http://localhost:9000", cfg.Storage.Endpoint)
	// Jika tidak diisi, alamat publik sama dengan alamat internal.
	assert.Equal(t, cfg.Storage.Endpoint, cfg.Storage.PublicEndpoint)
	assert.Equal(t, "videos", cfg.Storage.Bucket)
	assert.Equal(t, int64(2048)<<20, cfg.MaxUploadBytes)
	assert.Equal(t, 15*time.Minute, cfg.UploadURLTTL)
	assert.Equal(t, time.Hour, cfg.PlaybackURLTTL)
	assert.Equal(t, 30*time.Minute, cfg.ViewDedupWindow)
	assert.Equal(t, "nats://localhost:4222", cfg.NATSURL)
	assert.Equal(t, "localhost:50051", cfg.AuthGRPCAddr)
	// Jika tidak diisi, file HLS dibuka langsung dari storage.
	assert.Equal(t, "http://localhost:9000/videos", cfg.MediaPublicURL)
}

func TestLoadVideoMediaPublicURL(t *testing.T) {
	setVideoRequired(t)
	// Garis miring di akhir dibuang supaya URL file tidak berisi "//".
	t.Setenv("MEDIA_PUBLIC_URL", "http://media.ice-stream.localhost:4000/videos/")

	cfg, err := LoadVideo()
	require.NoError(t, err)
	assert.Equal(t, "http://media.ice-stream.localhost:4000/videos", cfg.MediaPublicURL)
}

func TestLoadVideoInvalid(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"GRPC_PORT salah":           {"GRPC_PORT", "abc"},
		"DB_PASSWORD kosong":        {"DB_PASSWORD", ""},
		"access key kosong":         {"STORAGE_ACCESS_KEY", ""},
		"secret key kosong":         {"STORAGE_SECRET_KEY", ""},
		"endpoint tanpa skema":      {"STORAGE_ENDPOINT", "storage:9000"},
		"endpoint dengan path":      {"STORAGE_PUBLIC_ENDPOINT", "http://localhost:9000/videos"},
		"MAX_UPLOAD_MB nol":         {"MAX_UPLOAD_MB", "0"},
		"MAX_UPLOAD_MB lebih 5 GiB": {"MAX_UPLOAD_MB", "6000"},
		"UPLOAD_URL_TTL kependekan": {"UPLOAD_URL_TTL", "10s"},
		"PLAYBACK_URL_TTL kelamaan": {"PLAYBACK_URL_TTL", "200h"},
		"NATS_URL bukan nats://":    {"NATS_URL", "http://nats:4222"},
		"MEDIA_PUBLIC_URL relatif":  {"MEDIA_PUBLIC_URL", "/videos"},
		"VIEW_DEDUP_WINDOW 10s":     {"VIEW_DEDUP_WINDOW", "10s"},
		"VIEW_DEDUP_WINDOW 48h":     {"VIEW_DEDUP_WINDOW", "48h"},
		"AUTH_GRPC_ADDR tanpa port": {"AUTH_GRPC_ADDR", "auth"},
		"AUTH_GRPC_ADDR port salah": {"AUTH_GRPC_ADDR", "auth:abc"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			setVideoRequired(t)
			t.Setenv(tt.key, tt.value)
			_, err := LoadVideo()
			assert.Error(t, err)
		})
	}
}
