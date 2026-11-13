// Package auth berisi logika AuthService: pendaftaran, login, dan token.
package auth

import (
	"errors"
	"time"
)

// Role dan status akun, sama dengan CHECK constraint di tabel users.
const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"

	StatusActive = "ACTIVE"
	StatusBanned = "BANNED"
)

// Error yang bisa dikembalikan Service. Lapisan gRPC menerjemahkannya
// menjadi kode status gRPC (lihat grpc.go).
var (
	ErrUsernameTaken       = errors.New("username sudah dipakai")
	ErrEmailTaken          = errors.New("email sudah dipakai")
	ErrInvalidCredentials  = errors.New("username/email atau password salah")
	ErrInvalidRefreshToken = errors.New("refresh token tidak valid")
	ErrUserBanned          = errors.New("akun diblokir")
	ErrNotFound            = errors.New("data tidak ditemukan")
)

// User adalah data akun yang aman dikirim ke luar (tanpa password_hash).
type User struct {
	ID          int64
	Username    string
	Email       string
	DisplayName string
	Role        string
	Status      string
	CreatedAt   time.Time
}

// NewUser adalah data untuk membuat akun baru.
type NewUser struct {
	Username     string
	Email        string
	PasswordHash string
	DisplayName  string
}

// RefreshToken adalah baris di tabel refresh_tokens.
type RefreshToken struct {
	UserID    int64
	TokenHash string
	UserAgent string
	IPAddress string
	ExpiresAt time.Time
}

// ClientInfo mencatat perangkat yang login.
type ClientInfo struct {
	UserAgent string
	IPAddress string
}

// TokenPair adalah hasil login atau refresh.
type TokenPair struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

// ValidationError menandai input dari user yang tidak valid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
