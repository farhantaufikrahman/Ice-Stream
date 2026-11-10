package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// TranscoderConfig menampung pengaturan transcoder: worker yang mengubah
// video mentah menjadi HLS memakai ffmpeg.
type TranscoderConfig struct {
	// Env bernilai "development" atau "production".
	Env string
	// HTTPAddr adalah alamat endpoint /healthz untuk Kubernetes, misalnya ":8080".
	HTTPAddr string
	// ShutdownTimeout adalah batas waktu menunggu pekerjaan berhenti saat
	// pod dimatikan.
	ShutdownTimeout time.Duration
	// NATSURL adalah alamat server NATS, misalnya "nats://nats:4222".
	NATSURL string
	// Storage hanya memakai Endpoint, Region, kunci, dan Bucket. Transcoder
	// tidak membuat URL presigned, jadi tidak butuh alamat publik.
	Storage StorageConfig
	// WorkDir adalah folder sementara untuk file mentah dan hasil ffmpeg.
	// Satu video bisa butuh ruang sekitar 2-3 kali ukuran file aslinya.
	WorkDir string
	// FFmpegPreset mengatur kecepatan vs ukuran hasil encode H.264:
	// ultrafast, superfast, veryfast, faster, fast, medium, ...
	// Semakin cepat, semakin ringan untuk CPU tapi file sedikit lebih besar.
	FFmpegPreset string
	// FFmpegThreads membatasi jumlah thread ffmpeg supaya PC tidak penuh.
	FFmpegThreads int
	// JobTimeout adalah batas waktu memproses satu video.
	JobTimeout time.Duration
}

// IsProduction mengembalikan true jika transcoder berjalan di mode production.
func (c TranscoderConfig) IsProduction() bool {
	return c.Env == "production"
}

var validPresets = map[string]bool{
	"ultrafast": true, "superfast": true, "veryfast": true, "faster": true,
	"fast": true, "medium": true, "slow": true, "slower": true, "veryslow": true,
}

// LoadTranscoder membaca konfigurasi transcoder dari environment variable.
func LoadTranscoder() (TranscoderConfig, error) {
	shutdownTimeout, err := getDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return TranscoderConfig{}, err
	}
	jobTimeout, err := getDuration("JOB_TIMEOUT", time.Hour)
	if err != nil {
		return TranscoderConfig{}, err
	}
	threads, err := getInt("FFMPEG_THREADS", 2)
	if err != nil {
		return TranscoderConfig{}, err
	}
	endpoint := getString("STORAGE_ENDPOINT", "http://localhost:9000")

	cfg := TranscoderConfig{
		Env:             getString("APP_ENV", "development"),
		HTTPAddr:        ":" + getString("HTTP_PORT", "8080"),
		ShutdownTimeout: shutdownTimeout,
		NATSURL:         getString("NATS_URL", "nats://localhost:4222"),
		Storage: StorageConfig{
			Endpoint:       endpoint,
			PublicEndpoint: endpoint,
			Region:         getString("STORAGE_REGION", "us-east-1"),
			AccessKey:      getString("STORAGE_ACCESS_KEY", ""),
			SecretKey:      getString("STORAGE_SECRET_KEY", ""),
			Bucket:         getString("STORAGE_BUCKET", "videos"),
			// Tidak dipakai transcoder, diisi supaya validasi StorageConfig lolos.
			CORSAllowedOrigins: []string{"http://localhost"},
		},
		WorkDir:       getString("WORK_DIR", os.TempDir()),
		FFmpegPreset:  strings.ToLower(getString("FFMPEG_PRESET", "veryfast")),
		FFmpegThreads: threads,
		JobTimeout:    jobTimeout,
	}
	if err := cfg.validate(); err != nil {
		return TranscoderConfig{}, err
	}
	return cfg, nil
}

func (c TranscoderConfig) validate() error {
	if c.Env != "development" && c.Env != "production" {
		return fmt.Errorf("APP_ENV harus \"development\" atau \"production\", dapat %q", c.Env)
	}
	if err := validPort("HTTP_PORT", strings.TrimPrefix(c.HTTPAddr, ":")); err != nil {
		return err
	}
	if err := validNATSURL(c.NATSURL); err != nil {
		return err
	}
	if err := c.Storage.validate(); err != nil {
		return err
	}
	if !validPresets[c.FFmpegPreset] {
		return fmt.Errorf("FFMPEG_PRESET tidak dikenal: %q (contoh: veryfast)", c.FFmpegPreset)
	}
	if c.FFmpegThreads < 1 || c.FFmpegThreads > 16 {
		return fmt.Errorf("FFMPEG_THREADS harus antara 1 dan 16, dapat %d", c.FFmpegThreads)
	}
	if c.JobTimeout < time.Minute || c.JobTimeout > 6*time.Hour {
		return fmt.Errorf("JOB_TIMEOUT harus antara 1m dan 6h, dapat %s", c.JobTimeout)
	}
	if c.WorkDir == "" {
		return fmt.Errorf("WORK_DIR tidak boleh kosong")
	}
	return nil
}