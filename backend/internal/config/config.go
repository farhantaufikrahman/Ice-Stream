// Package config membaca konfigurasi aplikasi dari environment variable.
//
// Semua nilai punya default yang aman untuk development lokal, kecuali
// DB_PASSWORD yang wajib diisi lewat .env atau Secret Kubernetes.
package config

import (
	"fmt"
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
	// DB berisi pengaturan koneksi ke Oracle.
	DB DBConfig
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

// Load membaca konfigurasi dari environment variable dan memvalidasinya.
func Load() (Config, error) {
	shutdownTimeout, err := getDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	dbPort, err := getInt("DB_PORT", 1521)
	if err != nil {
		return Config{}, err
	}
	dbMaxOpen, err := getInt("DB_MAX_OPEN_CONNS", 10)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:                getString("APP_ENV", "development"),
		HTTPAddr:           ":" + getString("HTTP_PORT", "8080"),
		CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		ShutdownTimeout:    shutdownTimeout,
		DB: DBConfig{
			Host:         getString("DB_HOST", "localhost"),
			Port:         dbPort,
			Service:      getString("DB_SERVICE", "FREEPDB1"),
			User:         getString("DB_USER", "auth_svc"),
			Password:     os.Getenv("DB_PASSWORD"), // sengaja tanpa default
			MaxOpenConns: dbMaxOpen,
		},
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
	port := strings.TrimPrefix(c.HTTPAddr, ":")
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("HTTP_PORT tidak valid: %q", port)
	}
	if len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS tidak boleh kosong")
	}
	if c.DB.Port < 1 || c.DB.Port > 65535 {
		return fmt.Errorf("DB_PORT tidak valid: %d", c.DB.Port)
	}
	if c.DB.MaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS minimal 1, dapat %d", c.DB.MaxOpenConns)
	}
	// Password tidak diberi default agar tidak ada password bawaan yang
	// ikut ter-commit. Isi lewat .env (lokal) atau Secret (Kubernetes).
	if c.DB.Password == "" {
		return fmt.Errorf("DB_PASSWORD wajib diisi")
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
