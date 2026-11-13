package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// AuthConfig menampung pengaturan AuthService (gRPC).
type AuthConfig struct {
	// Env bernilai "development" atau "production".
	Env string
	// GRPCAddr adalah alamat server gRPC, misalnya ":50051".
	GRPCAddr string
	// MetricsAddr adalah alamat server /metrics Prometheus (langkah 28).
	MetricsAddr string
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
	// Storage dipakai UserService untuk menyimpan foto profil (bucket
	// "images").
	Storage StorageConfig
	// MediaPublicURL adalah alamat publik bucket images, misalnya
	// "http://media.ice-stream.localhost:4000/images". URL foto profil =
	// MediaPublicURL + "/" + key.
	MediaPublicURL string
	// Mail adalah pengaturan email "Lupa kata sandi?" (langkah 24c).
	Mail MailConfig
}

// MailConfig adalah pengaturan pengiriman email dan tautan reset password.
type MailConfig struct {
	// SMTPAddr adalah host:port server SMTP. Di cluster lokal: Mailpit
	// ("mailpit:1025"), yang menangkap semua email tanpa mengirimnya.
	SMTPAddr string
	// SMTPTLS: "none" (Mailpit), "starttls" (port 587), atau "tls" (port 465).
	SMTPTLS string
	// SMTPUsername dan SMTPPassword untuk login SMTP. Resend: username
	// "resend", password = API key (dari Secret, bukan ConfigMap).
	SMTPUsername string
	SMTPPassword string
	// From adalah pengirim email, misalnya
	// "Ice-Stream <no-reply@ice-stream.localhost>".
	From string
	// AppBaseURL adalah alamat frontend untuk tautan di email, misalnya
	// "http://ice-stream.localhost:4000".
	AppBaseURL string
	// PasswordResetTTL adalah umur tautan reset password.
	PasswordResetTTL time.Duration
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
	db, err := loadDB("auth_svc")
	if err != nil {
		return AuthConfig{}, err
	}

	resetTTL, err := getDuration("PASSWORD_RESET_TTL", 30*time.Minute)
	if err != nil {
		return AuthConfig{}, err
	}

	storage := loadStorage("images")

	cfg := AuthConfig{
		Env:               getString("APP_ENV", "development"),
		GRPCAddr:          ":" + getString("GRPC_PORT", "50051"),
		MetricsAddr:       ":" + getString("METRICS_PORT", "9091"),
		ShutdownTimeout:   shutdownTimeout,
		DB:                db,
		JWTPrivateKeyFile: getString("JWT_PRIVATE_KEY_FILE", ""),
		AccessTokenTTL:    accessTTL,
		RefreshTokenTTL:   refreshTTL,
		Storage:           storage,
		MediaPublicURL:    loadMediaURL(storage),
		Mail: MailConfig{
			SMTPAddr:         getString("SMTP_ADDR", "localhost:1025"),
			SMTPTLS:          strings.ToLower(getString("SMTP_TLS", "none")),
			SMTPUsername:     getString("SMTP_USERNAME", ""),
			SMTPPassword:     getString("SMTP_PASSWORD", ""),
			From:             getString("MAIL_FROM", "Ice-Stream <no-reply@ice-stream.localhost>"),
			AppBaseURL:       strings.TrimRight(getString("APP_BASE_URL", "http://localhost:5173"), "/"),
			PasswordResetTTL: resetTTL,
		},
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
	if err := validMetricsPort(c.MetricsAddr, c.GRPCAddr); err != nil {
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
	if err := c.Storage.validate(); err != nil {
		return err
	}
	if err := validMediaURL(c.MediaPublicURL, "/images"); err != nil {
		return err
	}
	if err := c.Mail.validate(); err != nil {
		return err
	}
	return c.DB.validate()
}

func (m MailConfig) validate() error {
	host, port, err := net.SplitHostPort(m.SMTPAddr)
	if err != nil || host == "" {
		return fmt.Errorf("SMTP_ADDR harus berbentuk host:port, dapat %q", m.SMTPAddr)
	}
	if err := validPort("SMTP_ADDR", port); err != nil {
		return err
	}
	switch m.SMTPTLS {
	case "none", "starttls", "tls":
	default:
		return fmt.Errorf("SMTP_TLS harus none, starttls, atau tls, dapat %q", m.SMTPTLS)
	}
	if m.SMTPUsername != "" && m.SMTPTLS == "none" {
		return errors.New("SMTP_USERNAME butuh SMTP_TLS=starttls atau tls, supaya password tidak terkirim tanpa enkripsi")
	}
	if m.SMTPUsername != "" && m.SMTPPassword == "" {
		return errors.New("SMTP_PASSWORD wajib diisi jika SMTP_USERNAME diisi")
	}
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("MAIL_FROM tidak valid (contoh: Ice-Stream <no-reply@contoh.com>): %w", err)
	}
	u, err := url.Parse(m.AppBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("APP_BASE_URL harus berupa alamat http(s) tanpa path, dapat %q", m.AppBaseURL)
	}
	if m.PasswordResetTTL < 5*time.Minute || m.PasswordResetTTL > 24*time.Hour {
		return fmt.Errorf("PASSWORD_RESET_TTL harus antara 5m dan 24h, dapat %s", m.PasswordResetTTL)
	}
	return nil
}
