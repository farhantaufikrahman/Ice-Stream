package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// VideoConfig menampung pengaturan VideoService (gRPC).
type VideoConfig struct {
	// Env bernilai "development" atau "production".
	Env string
	// GRPCAddr adalah alamat server gRPC, misalnya ":50052".
	GRPCAddr string
	// ShutdownTimeout adalah batas waktu menunggu RPC yang sedang berjalan
	// selesai saat server dimatikan.
	ShutdownTimeout time.Duration
	DB              DBConfig
	Storage         StorageConfig
	// NATSURL adalah alamat server NATS, misalnya "nats://nats:4222".
	NATSURL string
	// MediaPublicURL adalah alamat publik folder bucket untuk file HLS dan
	// thumbnail, misalnya "http://media.ice-stream.localhost:4000/videos".
	// URL file = MediaPublicURL + "/" + key, misalnya
	// ".../videos/hls/42/master.m3u8". Nanti bisa diganti alamat CDN.
	MediaPublicURL string
	// MaxUploadBytes adalah ukuran file video terbesar yang boleh diunggah.
	MaxUploadBytes int64
	// UploadURLTTL adalah umur URL presigned untuk mengunggah.
	UploadURLTTL time.Duration
	// PlaybackURLTTL adalah umur URL presigned untuk memutar video.
	PlaybackURLTTL time.Duration
}

// StorageConfig berisi pengaturan object storage yang kompatibel S3
// (RustFS di Kubernetes; nanti bisa AWS S3 atau Cloudflare R2).
type StorageConfig struct {
	// Endpoint dipakai VideoService sendiri, misalnya "http://storage:9000"
	// (nama Service di dalam cluster).
	Endpoint string
	// PublicEndpoint adalah alamat yang bisa dibuka browser, misalnya
	// "http://localhost:9000". Dipakai untuk membuat URL presigned, karena
	// nama host ikut ditandatangani di dalam URL.
	PublicEndpoint string
	// Region wajib ada di tanda tangan S3. RustFS menerima nilai apa pun;
	// "us-east-1" adalah default yang umum.
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	// CORSAllowedOrigins berisi domain frontend yang boleh mengunggah
	// langsung ke bucket.
	CORSAllowedOrigins []string
}

// IsProduction mengembalikan true jika service berjalan di mode production.
func (c VideoConfig) IsProduction() bool {
	return c.Env == "production"
}

// LoadVideo membaca konfigurasi VideoService dari environment variable.
func LoadVideo() (VideoConfig, error) {
	shutdownTimeout, err := getDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return VideoConfig{}, err
	}
	uploadTTL, err := getDuration("UPLOAD_URL_TTL", 15*time.Minute)
	if err != nil {
		return VideoConfig{}, err
	}
	playbackTTL, err := getDuration("PLAYBACK_URL_TTL", time.Hour)
	if err != nil {
		return VideoConfig{}, err
	}
	maxUploadMB, err := getInt("MAX_UPLOAD_MB", 2048)
	if err != nil {
		return VideoConfig{}, err
	}
	db, err := loadDB("video_svc")
	if err != nil {
		return VideoConfig{}, err
	}
	endpoint := getString("STORAGE_ENDPOINT", "http://localhost:9000")
	publicEndpoint := getString("STORAGE_PUBLIC_ENDPOINT", endpoint)
	bucket := getString("STORAGE_BUCKET", "videos")
	// Default: file HLS dibuka langsung dari storage, misalnya
	// http://localhost:9000/videos. Garis miring di akhir dibuang.
	mediaURL := strings.TrimSuffix(getString("MEDIA_PUBLIC_URL", strings.TrimSuffix(publicEndpoint, "/")+"/"+bucket), "/")

	cfg := VideoConfig{
		Env:             getString("APP_ENV", "development"),
		GRPCAddr:        ":" + getString("GRPC_PORT", "50052"),
		ShutdownTimeout: shutdownTimeout,
		DB:              db,
		Storage: StorageConfig{
			Endpoint:       endpoint,
			PublicEndpoint: publicEndpoint,
			Region:         getString("STORAGE_REGION", "us-east-1"),
			// Sengaja tanpa default, sama seperti DB_PASSWORD.
			AccessKey:          getString("STORAGE_ACCESS_KEY", ""),
			SecretKey:          getString("STORAGE_SECRET_KEY", ""),
			Bucket:             bucket,
			CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		},
		NATSURL:        getString("NATS_URL", "nats://localhost:4222"),
		MediaPublicURL: mediaURL,
		MaxUploadBytes: int64(maxUploadMB) << 20,
		UploadURLTTL:   uploadTTL,
		PlaybackURLTTL: playbackTTL,
	}
	if err := cfg.validate(); err != nil {
		return VideoConfig{}, err
	}
	return cfg, nil
}

func (c VideoConfig) validate() error {
	if c.Env != "development" && c.Env != "production" {
		return fmt.Errorf("APP_ENV harus \"development\" atau \"production\", dapat %q", c.Env)
	}
	if err := validPort("GRPC_PORT", strings.TrimPrefix(c.GRPCAddr, ":")); err != nil {
		return err
	}
	// Batas 5 GiB: ukuran terbesar satu kali PUT di API S3. File yang lebih
	// besar butuh multipart upload (belum dibuat).
	if c.MaxUploadBytes < 1<<20 || c.MaxUploadBytes > 5<<30 {
		return fmt.Errorf("MAX_UPLOAD_MB harus antara 1 dan 5120")
	}
	// Batas 7 hari adalah batas URL presigned di API S3 (SigV4).
	if c.UploadURLTTL < time.Minute || c.UploadURLTTL > 7*24*time.Hour {
		return fmt.Errorf("UPLOAD_URL_TTL harus antara 1m dan 168h, dapat %s", c.UploadURLTTL)
	}
	if c.PlaybackURLTTL < time.Minute || c.PlaybackURLTTL > 7*24*time.Hour {
		return fmt.Errorf("PLAYBACK_URL_TTL harus antara 1m dan 168h, dapat %s", c.PlaybackURLTTL)
	}
	if err := c.Storage.validate(); err != nil {
		return err
	}
	if err := validNATSURL(c.NATSURL); err != nil {
		return err
	}
	if u, err := url.Parse(c.MediaPublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
		return fmt.Errorf("MEDIA_PUBLIC_URL harus berupa URL seperti http://media.ice-stream.localhost:4000/videos, dapat %q", c.MediaPublicURL)
	}
	return c.DB.validate()
}

// validNATSURL memeriksa bentuk NATS_URL, misalnya nats://nats:4222.
func validNATSURL(v string) error {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "nats" && u.Scheme != "tls") || u.Host == "" {
		return fmt.Errorf("NATS_URL harus berupa URL seperti nats://nats:4222, dapat %q", v)
	}
	return nil
}

func (s StorageConfig) validate() error {
	for key, v := range map[string]string{"STORAGE_ENDPOINT": s.Endpoint, "STORAGE_PUBLIC_ENDPOINT": s.PublicEndpoint} {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("%s harus berupa URL seperti http://storage:9000, dapat %q", key, v)
		}
	}
	if s.AccessKey == "" || s.SecretKey == "" {
		return fmt.Errorf("STORAGE_ACCESS_KEY dan STORAGE_SECRET_KEY wajib diisi")
	}
	if s.Bucket == "" {
		return fmt.Errorf("STORAGE_BUCKET tidak boleh kosong")
	}
	if len(s.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS tidak boleh kosong")
	}
	return nil
}
