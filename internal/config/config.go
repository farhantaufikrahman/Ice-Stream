// Package config membaca konfigurasi aplikasi dari environment variable.
//
// Semua nilai punya default yang aman untuk development lokal, sehingga
// `go run ./cmd/api` langsung jalan tanpa file .env.
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

	cfg := Config{
		Env:                getString("APP_ENV", "development"),
		HTTPAddr:           ":" + getString("HTTP_PORT", "8080"),
		CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		ShutdownTimeout:    shutdownTimeout,
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
