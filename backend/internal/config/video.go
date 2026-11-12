package config

import (
	"fmt"
	"net"
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
	// AuthGRPCAddr adalah alamat proses AuthService, misalnya "auth:50051".
	// UserService berjalan di proses itu; VideoService memintanya daftar
	// subscriber untuk notifikasi video baru.
	AuthGRPCAddr string
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
	// ViewDedupWindow adalah jarak waktu minimal agar penonton yang sama
	// dihitung sebagai view baru untuk video yang sama.
	ViewDedupWindow time.Duration
	// Live adalah pengaturan siaran langsung (langkah 25a).
	Live LiveConfig
}

// LiveConfig adalah pengaturan LiveService, yang berjalan di proses
// VideoService dan berbicara dengan MediaMTX.
type LiveConfig struct {
	// MediaMTXAPIURL adalah alamat Control API MediaMTX, misalnya
	// "http://mediamtx:9997". Dibaca setiap PollInterval.
	MediaMTXAPIURL string
	// HookAddr adalah alamat server HTTP kecil yang menjawab hook auth
	// MediaMTX ("boleh siaran?"), misalnya ":8090". Tidak dibuka ke luar
	// cluster.
	HookAddr string
	// IngestURL adalah alamat RTMP yang diisi kreator di OBS, misalnya
	// "rtmp://localhost:1935/live".
	IngestURL string
	// PlaybackURL adalah alamat publik HLS MediaMTX, misalnya
	// "http://live.ice-stream.localhost:4000".
	PlaybackURL string
	// WHIPURL adalah alamat publik WebRTC MediaMTX untuk siaran dari browser
	// (langkah 25b), misalnya "http://webrtc.ice-stream.localhost:4000".
	WHIPURL string
	// PublishTokenTTL adalah lama token siaran browser berlaku.
	PublishTokenTTL time.Duration
	// PollInterval adalah jarak antar pembacaan status dari MediaMTX.
	PollInterval time.Duration
	// Grace adalah batas tenggang sebelum siaran yang hilang ditutup.
	Grace time.Duration
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
	viewWindow, err := getDuration("VIEW_DEDUP_WINDOW", 30*time.Minute)
	if err != nil {
		return VideoConfig{}, err
	}
	maxUploadMB, err := getInt("MAX_UPLOAD_MB", 2048)
	if err != nil {
		return VideoConfig{}, err
	}
	livePoll, err := getDuration("LIVE_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return VideoConfig{}, err
	}
	liveGrace, err := getDuration("LIVE_GRACE", 15*time.Second)
	if err != nil {
		return VideoConfig{}, err
	}
	liveTokenTTL, err := getDuration("LIVE_PUBLISH_TOKEN_TTL", 5*time.Minute)
	if err != nil {
		return VideoConfig{}, err
	}
	db, err := loadDB("video_svc")
	if err != nil {
		return VideoConfig{}, err
	}
	storage := loadStorage("videos")

	cfg := VideoConfig{
		Env:             getString("APP_ENV", "development"),
		GRPCAddr:        ":" + getString("GRPC_PORT", "50052"),
		ShutdownTimeout: shutdownTimeout,
		DB:              db,
		Storage:         storage,
		NATSURL:         getString("NATS_URL", "nats://localhost:4222"),
		AuthGRPCAddr:    getString("AUTH_GRPC_ADDR", "localhost:50051"),
		MediaPublicURL:  loadMediaURL(storage),
		MaxUploadBytes:  int64(maxUploadMB) << 20,
		UploadURLTTL:    uploadTTL,
		PlaybackURLTTL:  playbackTTL,
		ViewDedupWindow: viewWindow,
		Live: LiveConfig{
			MediaMTXAPIURL: strings.TrimRight(getString("MEDIAMTX_API_URL", "http://localhost:9997"), "/"),
			HookAddr:       ":" + getString("LIVE_HOOK_PORT", "8090"),
			IngestURL:      strings.TrimRight(getString("LIVE_INGEST_URL", "rtmp://localhost:1935/live"), "/"),
			PlaybackURL:    strings.TrimRight(getString("LIVE_PLAYBACK_URL", "http://localhost:8888"), "/"),
			// Port WebRTC bawaan MediaMTX.
			WHIPURL:         strings.TrimRight(getString("LIVE_WHIP_URL", "http://localhost:8889"), "/"),
			PublishTokenTTL: liveTokenTTL,
			PollInterval:    livePoll,
			Grace:           liveGrace,
		},
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
	if c.ViewDedupWindow < time.Minute || c.ViewDedupWindow > 24*time.Hour {
		return fmt.Errorf("VIEW_DEDUP_WINDOW harus antara 1m dan 24h, dapat %s", c.ViewDedupWindow)
	}
	if err := c.Storage.validate(); err != nil {
		return err
	}
	if err := validNATSURL(c.NATSURL); err != nil {
		return err
	}
	if _, port, err := net.SplitHostPort(c.AuthGRPCAddr); err != nil || validPort("AUTH_GRPC_ADDR", port) != nil {
		return fmt.Errorf("AUTH_GRPC_ADDR harus berbentuk host:port seperti auth:50051, dapat %q", c.AuthGRPCAddr)
	}
	if err := validMediaURL(c.MediaPublicURL, "/videos"); err != nil {
		return err
	}
	if err := c.Live.validate(); err != nil {
		return err
	}
	return c.DB.validate()
}

func (l LiveConfig) validate() error {
	for key, v := range map[string]string{"MEDIAMTX_API_URL": l.MediaMTXAPIURL, "LIVE_PLAYBACK_URL": l.PlaybackURL, "LIVE_WHIP_URL": l.WHIPURL} {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" {
			return fmt.Errorf("%s harus berupa alamat http(s) tanpa path, dapat %q", key, v)
		}
	}
	if u, err := url.Parse(l.IngestURL); err != nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") || u.Host == "" {
		return fmt.Errorf("LIVE_INGEST_URL harus berupa URL seperti rtmp://localhost:1935/live, dapat %q", l.IngestURL)
	}
	if err := validPort("LIVE_HOOK_PORT", strings.TrimPrefix(l.HookAddr, ":")); err != nil {
		return err
	}
	// Token hanya dipakai saat tombol "Mulai siaran" ditekan, jadi tidak
	// perlu lama. Batas bawah memberi waktu untuk jaringan yang lambat.
	if l.PublishTokenTTL < 30*time.Second || l.PublishTokenTTL > time.Hour {
		return fmt.Errorf("LIVE_PUBLISH_TOKEN_TTL harus antara 30s dan 1h, dapat %s", l.PublishTokenTTL)
	}
	if l.PollInterval < time.Second || l.PollInterval > time.Minute {
		return fmt.Errorf("LIVE_POLL_INTERVAL harus antara 1s dan 1m, dapat %s", l.PollInterval)
	}
	// Batas tenggang harus lebih lama dari jarak pembacaan, kalau tidak
	// siaran bisa ditutup di antara dua pembacaan.
	if l.Grace < 2*l.PollInterval || l.Grace > 10*time.Minute {
		return fmt.Errorf("LIVE_GRACE harus minimal 2x LIVE_POLL_INTERVAL dan maksimal 10m, dapat %s", l.Grace)
	}
	return nil
}

// loadStorage membaca pengaturan object storage (STORAGE_*). defaultBucket
// berbeda per service: "videos" untuk VideoService, "images" untuk
// UserService.
func loadStorage(defaultBucket string) StorageConfig {
	endpoint := getString("STORAGE_ENDPOINT", "http://localhost:9000")
	return StorageConfig{
		Endpoint:       endpoint,
		PublicEndpoint: getString("STORAGE_PUBLIC_ENDPOINT", endpoint),
		Region:         getString("STORAGE_REGION", "us-east-1"),
		// Sengaja tanpa default, sama seperti DB_PASSWORD.
		AccessKey:          getString("STORAGE_ACCESS_KEY", ""),
		SecretKey:          getString("STORAGE_SECRET_KEY", ""),
		Bucket:             getString("STORAGE_BUCKET", defaultBucket),
		CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
	}
}

// loadMediaURL membaca MEDIA_PUBLIC_URL. Default: file dibuka langsung dari
// storage, misalnya http://localhost:9000/videos. Garis miring di akhir
// dibuang.
func loadMediaURL(s StorageConfig) string {
	fallback := strings.TrimSuffix(s.PublicEndpoint, "/") + "/" + s.Bucket
	return strings.TrimSuffix(getString("MEDIA_PUBLIC_URL", fallback), "/")
}

// validMediaURL memeriksa MEDIA_PUBLIC_URL: URL http(s) lengkap tanpa query.
func validMediaURL(v, examplePath string) error {
	if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
		return fmt.Errorf("MEDIA_PUBLIC_URL harus berupa URL seperti http://media.ice-stream.localhost:4000%s, dapat %q", examplePath, v)
	}
	return nil
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
