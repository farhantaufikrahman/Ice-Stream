package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTranscoderRequired(t *testing.T) {
	t.Helper()
	t.Setenv("STORAGE_ACCESS_KEY", "kunci")
	t.Setenv("STORAGE_SECRET_KEY", "rahasia-storage")
}

func TestLoadTranscoderDefaults(t *testing.T) {
	for _, key := range []string{
		"APP_ENV", "HTTP_PORT", "SHUTDOWN_TIMEOUT", "NATS_URL", "STORAGE_ENDPOINT",
		"STORAGE_BUCKET", "WORK_DIR", "FFMPEG_PRESET", "FFMPEG_THREADS", "JOB_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
	setTranscoderRequired(t)

	cfg, err := LoadTranscoder()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, "nats://localhost:4222", cfg.NATSURL)
	assert.Equal(t, "http://localhost:9000", cfg.Storage.Endpoint)
	assert.Equal(t, "videos", cfg.Storage.Bucket)
	assert.Equal(t, "veryfast", cfg.FFmpegPreset)
	assert.Equal(t, 2, cfg.FFmpegThreads)
	assert.Equal(t, time.Hour, cfg.JobTimeout)
	assert.NotEmpty(t, cfg.WorkDir)
}

func TestLoadTranscoderInvalid(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"access key kosong":    {"STORAGE_ACCESS_KEY", ""},
		"NATS_URL salah":       {"NATS_URL", "nats:4222"},
		"preset tidak dikenal": {"FFMPEG_PRESET", "kilat"},
		"threads nol":          {"FFMPEG_THREADS", "0"},
		"threads kebanyakan":   {"FFMPEG_THREADS", "64"},
		"JOB_TIMEOUT pendek":   {"JOB_TIMEOUT", "10s"},
		"HTTP_PORT salah":      {"HTTP_PORT", "abc"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			setTranscoderRequired(t)
			t.Setenv(tt.key, tt.value)
			_, err := LoadTranscoder()
			assert.Error(t, err)
		})
	}
}
