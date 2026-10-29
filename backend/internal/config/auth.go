package config

import (
	"fmt"
	"strings"
	"time"
)

// AuthConfig menampung pengaturan AuthService (gRPC).
type AuthConfig struct {
	// Env bernilai "development" atau "production".
	Env string
	// GRPCAddr adalah alamat server gRPC, misalnya ":50051".
	GRPCAddr string
	// ShutdownTimeout adalah batas waktu menunggu RPC yang sedang berjalan
	// selesai saat server dimatikan.
	ShutdownTimeout time.Duration
	DB              DBConfig
	// JWTPrivateKeyFile adalah path file PEM kunci privat Ed25519 untuk
	// menandatangani access token.
	JWTPrivateKeyFile string
	// AccessTokenTTL adalah umur access token (rancangan: 15 menit).
	AccessTokenTTL time.Duration
	// RefreshTokenTTL adalah umur refresh token.
	RefreshTokenTTL time.Duration
}

// IsProduction mengembalikan true jika service berjalan di mode production.
func (c AuthConfig) IsProduction() bool {
	return c.Env == "production"
}

// LoadAuth membaca konfigurasi AuthService dari environment variable.
func LoadAuth() (AuthConfig, error) {
	shutdownTimeout, err := getDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return AuthConfig{}, err
	}
	accessTTL, err := getDuration("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return AuthConfig{}, err
	}
	refreshTTL, err := getDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	if err != nil {
		return AuthConfig{}, err
	}
	db, err := loadDB()
	if err != nil {
		return AuthConfig{}, err
	}

	cfg := AuthConfig{
		Env:               getString("APP_ENV", "development"),
		GRPCAddr:          ":" + getString("GRPC_PORT", "50051"),
		ShutdownTimeout:   shutdownTimeout,
		DB:                db,
		JWTPrivateKeyFile: getString("JWT_PRIVATE_KEY_FILE", ""),
		AccessTokenTTL:    accessTTL,
		RefreshTokenTTL:   refreshTTL,
	}
	if err := cfg.validate(); err != nil {
		return AuthConfig{}, err
	}
	return cfg, nil
}

func (c AuthConfig) validate() error {
	if c.Env != "development" && c.Env != "production" {
		return fmt.Errorf("APP_ENV harus \"development\" atau \"production\", dapat %q", c.Env)
	}
	if err := validPort("GRPC_PORT", strings.TrimPrefix(c.GRPCAddr, ":")); err != nil {
		return err
	}
	if c.JWTPrivateKeyFile == "" {
		return fmt.Errorf("JWT_PRIVATE_KEY_FILE wajib diisi")
	}
	if c.AccessTokenTTL < time.Minute || c.AccessTokenTTL > time.Hour {
		return fmt.Errorf("ACCESS_TOKEN_TTL harus antara 1m dan 1h, dapat %s", c.AccessTokenTTL)
	}
	if c.RefreshTokenTTL <= c.AccessTokenTTL {
		return fmt.Errorf("REFRESH_TOKEN_TTL harus lebih lama dari ACCESS_TOKEN_TTL")
	}
	return c.DB.validate()
}
