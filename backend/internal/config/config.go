// Package config membaca konfigurasi aplikasi dari environment variable.
//
// Hampir semua nilai punya default yang aman untuk development lokal.
// Yang wajib diisi hanya rahasia dan lokasi file kunci (lihat .env.example).
package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config menampung semua pengaturan yang dibutuhkan API gateway.
type Config struct {
	// Env bernilai "development" atau "production".
	Env string
	// HTTPAddr adalah alamat yang didengarkan server, misalnya ":8080".
	HTTPAddr string
	// CORSAllowedOrigins berisi domain frontend yang boleh memanggil API.
	CORSAllowedOrigins []string
	// ShutdownTimeout adalah batas waktu menunggu request yang sedang berjalan
	// selesai saat server dimatikan.
	ShutdownTimeout time.Duration
	// TrustedProxies berisi IP/CIDR proxy yang boleh mengisi header
	// X-Forwarded-For. Kosong = header itu diabaikan (paling aman).
	TrustedProxies []string
	// AuthGRPCAddr adalah alamat AuthService, misalnya "auth:50051".
	AuthGRPCAddr string
	// JWTPublicKeyFile adalah file kunci publik untuk memeriksa access token.
	JWTPublicKeyFile string
	// Cookie mengatur cookie refresh token.
	Cookie CookieConfig
}

// CookieConfig mengatur cookie refresh token.
type CookieConfig struct {
	// Secure = cookie hanya dikirim lewat HTTPS. Wajib true di production.
	Secure bool
	// SameSite mengatur kapan browser mengirim cookie ke domain lain.
	SameSite http.SameSite
}

// DBConfig berisi pengaturan koneksi ke Oracle.
type DBConfig struct {
	Host string
	Port int
	// Service adalah nama service Oracle. Oracle Free memakai "FREEPDB1"
	// untuk database aplikasi (pluggable database).
	Service  string
	User     string
	Password string
	// MaxOpenConns membatasi jumlah koneksi sekaligus ke Oracle.
	MaxOpenConns int
}

// IsProduction mengembalikan true jika aplikasi berjalan di mode production.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

// Load membaca konfigurasi API gateway dari environment variable dan memvalidasinya.
func Load() (Config, error) {
	shutdownTimeout, err := getDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	env := getString("APP_ENV", "development")
	// Default Secure mengikuti APP_ENV: true di production, false di laptop
	// (http://localhost tidak memakai HTTPS).
	cookieSecure, err := getBool("COOKIE_SECURE", env == "production")
	if err != nil {
		return Config{}, err
	}
	sameSite, err := parseSameSite(getString("COOKIE_SAMESITE", "lax"))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:                env,
		HTTPAddr:           ":" + getString("HTTP_PORT", "8080"),
		CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		ShutdownTimeout:    shutdownTimeout,
		TrustedProxies:     getList("TRUSTED_PROXIES", nil),
		AuthGRPCAddr:       getString("AUTH_GRPC_ADDR", "localhost:50051"),
		JWTPublicKeyFile:   getString("JWT_PUBLIC_KEY_FILE", ""),
		Cookie:             CookieConfig{Secure: cookieSecure, SameSite: sameSite},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Env != "development" && c.Env != "production" {
		return fmt.Errorf("APP_ENV harus \"development\" atau \"production\", dapat %q", c.Env)
	}
	if err := validPort("HTTP_PORT", strings.TrimPrefix(c.HTTPAddr, ":")); err != nil {
		return err
	}
	if len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS tidak boleh kosong")
	}
	if c.JWTPublicKeyFile == "" {
		return fmt.Errorf("JWT_PUBLIC_KEY_FILE wajib diisi")
	}
	// Browser menolak cookie SameSite=None yang tidak Secure.
	if c.Cookie.SameSite == http.SameSiteNoneMode && !c.Cookie.Secure {
		return fmt.Errorf("COOKIE_SAMESITE=none wajib disertai COOKIE_SECURE=true")
	}
	return nil
}

// parseSameSite mengubah "lax", "strict", atau "none" menjadi http.SameSite.
func parseSameSite(v string) (http.SameSite, error) {
	switch strings.ToLower(v) {
	case "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	}
	return 0, fmt.Errorf("COOKIE_SAMESITE harus lax, strict, atau none, dapat %q", v)
}

// loadDB membaca pengaturan Oracle (DB_*). Dipakai oleh service yang
// memegang database, seperti AuthService. Gateway tidak memakainya.
func loadDB(defaultUser string) (DBConfig, error) {
	port, err := getInt("DB_PORT", 1521)
	if err != nil {
		return DBConfig{}, err
	}
	maxOpen, err := getInt("DB_MAX_OPEN_CONNS", 10)
	if err != nil {
		return DBConfig{}, err
	}
	return DBConfig{
		Host:         getString("DB_HOST", "localhost"),
		Port:         port,
		Service:      getString("DB_SERVICE", "FREEPDB1"),
		User:         getString("DB_USER", defaultUser),
		Password:     os.Getenv("DB_PASSWORD"), // sengaja tanpa default
		MaxOpenConns: maxOpen,
	}, nil
}

func (d DBConfig) validate() error {
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("DB_PORT tidak valid: %d", d.Port)
	}
	if d.MaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS minimal 1, dapat %d", d.MaxOpenConns)
	}
	// Password tidak diberi default agar tidak ada password bawaan yang
	// ikut ter-commit. Isi lewat .env (lokal) atau Secret (Kubernetes).
	if d.Password == "" {
		return fmt.Errorf("DB_PASSWORD wajib diisi")
	}
	return nil
}

// validPort memeriksa bahwa nilai port berupa angka 1 sampai 65535.
func validPort(key, port string) error {
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s tidak valid: %q", key, port)
	}
	return nil
}

func getString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("%s tidak valid (contoh: 10s, 1m): %w", key, err)
	}
	return d, nil
}

func getBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, fmt.Errorf("%s harus true atau false: %w", key, err)
	}
	return b, nil
}

func getInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("%s harus berupa angka: %w", key, err)
	}
	return n, nil
}

// getList memecah nilai yang dipisah koma, misalnya "http://a.com, http://b.com".
func getList(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
