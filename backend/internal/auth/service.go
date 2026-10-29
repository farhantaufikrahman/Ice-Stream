package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Repository adalah semua akses database yang dibutuhkan Service.
// Implementasi Oracle ada di oracle_repository.go; test memakai versi palsu.
// Pola ini mirip interface Repository di Spring Data atau "repository
// layer" di aplikasi Express.
type Repository interface {
	// CreateUser menyimpan user baru. Mengembalikan ErrUsernameTaken atau
	// ErrEmailTaken jika melanggar UNIQUE constraint.
	CreateUser(ctx context.Context, u NewUser) (User, error)
	// FindUserByLogin mencari user (yang belum dihapus) berdasarkan username
	// atau email, beserta password hash-nya. ErrNotFound jika tidak ada.
	FindUserByLogin(ctx context.Context, login string) (User, string, error)
	// FindUserByID mencari user yang belum dihapus. ErrNotFound jika tidak ada.
	FindUserByID(ctx context.Context, id int64) (User, error)
	// CreateRefreshToken menyimpan refresh token baru.
	CreateRefreshToken(ctx context.Context, t RefreshToken) error
	// RotateRefreshToken, dalam satu transaksi, mencabut token dengan hash
	// oldHash lalu menyimpan token pengganti untuk user yang sama.
	// Mengembalikan ErrInvalidRefreshToken jika token tidak ada, sudah
	// kedaluwarsa, atau sudah pernah dicabut. Untuk kasus terakhir, semua
	// token milik user itu juga dicabut (kemungkinan token dicuri).
	RotateRefreshToken(ctx context.Context, oldHash string, next RefreshToken, now time.Time) (userID int64, err error)
	// RevokeRefreshToken mencabut token jika masih aktif.
	RevokeRefreshToken(ctx context.Context, hash string, now time.Time) error
}

// Service berisi aturan bisnis AuthService, terpisah dari gRPC dan Oracle
// supaya mudah diuji.
type Service struct {
	repo       Repository
	tokens     *TokenMaker
	refreshTTL time.Duration
	now        func() time.Time
	// dummyHash dipakai saat user tidak ditemukan, supaya waktu respons login
	// sama dengan saat password salah. Tanpa ini, penyerang bisa menebak
	// email mana yang terdaftar dari cepat-lambatnya respons.
	dummyHash string
}

// NewService membuat Service.
func NewService(repo Repository, tokens *TokenMaker, refreshTTL time.Duration) (*Service, error) {
	dummy, err := HashPassword("dummy-password-untuk-menyamakan-waktu")
	if err != nil {
		return nil, err
	}
	return &Service{
		repo:       repo,
		tokens:     tokens,
		refreshTTL: refreshTTL,
		now:        func() time.Time { return time.Now().UTC() },
		dummyHash:  dummy,
	}, nil
}

// RegisterInput adalah data dari form daftar.
type RegisterInput struct {
	Username    string
	Email       string
	Password    string
	DisplayName string
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

// Register membuat akun baru dengan role USER.
func (s *Service) Register(ctx context.Context, in RegisterInput) (User, error) {
	nu, err := s.validateRegister(in)
	if err != nil {
		return User{}, err
	}
	nu.PasswordHash, err = HashPassword(in.Password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	return s.repo.CreateUser(ctx, nu)
}

func (s *Service) validateRegister(in RegisterInput) (NewUser, error) {
	// Username dan email disimpan huruf kecil, supaya "Budi" dan "budi"
	// tidak bisa jadi dua akun berbeda.
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !usernamePattern.MatchString(username) {
		return NewUser{}, &ValidationError{"username", "3-30 karakter, hanya huruf, angka, dan garis bawah"}
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return NewUser{}, &ValidationError{"email", "format email tidak valid"}
	}

	// bcrypt hanya membaca 72 byte pertama, jadi password lebih panjang ditolak.
	if utf8.RuneCountInString(in.Password) < 8 || len(in.Password) > 72 {
		return NewUser{}, &ValidationError{"password", "minimal 8 karakter dan maksimal 72 byte"}
	}

	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = username
	}
	if utf8.RuneCountInString(display) > 50 {
		return NewUser{}, &ValidationError{"display_name", "maksimal 50 karakter"}
	}

	return NewUser{Username: username, Email: email, DisplayName: display}, nil
}

// Login memeriksa username/email dan password, lalu membuat token.
func (s *Service) Login(ctx context.Context, login, password string, client ClientInfo) (User, TokenPair, error) {
	u, hash, err := s.repo.FindUserByLogin(ctx, strings.ToLower(strings.TrimSpace(login)))
	if errors.Is(err, ErrNotFound) {
		CheckPassword(s.dummyHash, password)
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, TokenPair{}, err
	}
	if !CheckPassword(hash, password) {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	// Status dicek SETELAH password benar, supaya orang lain tidak bisa
	// mengetahui akun mana yang diblokir.
	if u.Status != StatusActive {
		return User{}, TokenPair{}, ErrUserBanned
	}

	now := s.now()
	refresh, refreshHash, err := NewRefreshToken()
	if err != nil {
		return User{}, TokenPair{}, err
	}
	rt := s.newRefreshRow(u.ID, refreshHash, client, now)
	if err := s.repo.CreateRefreshToken(ctx, rt); err != nil {
		return User{}, TokenPair{}, err
	}

	pair, err := s.tokenPair(u, refresh, rt.ExpiresAt, now)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	return u, pair, nil
}

// Refresh menukar refresh token lama dengan pasangan token baru (rotasi).
func (s *Service) Refresh(ctx context.Context, refreshToken string, client ClientInfo) (TokenPair, error) {
	if refreshToken == "" {
		return TokenPair{}, ErrInvalidRefreshToken
	}
	now := s.now()
	next, nextHash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	rt := s.newRefreshRow(0, nextHash, client, now)

	userID, err := s.repo.RotateRefreshToken(ctx, HashRefreshToken(refreshToken), rt, now)
	if err != nil {
		return TokenPair{}, err
	}

	u, err := s.repo.FindUserByID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return TokenPair{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return TokenPair{}, err
	}
	if u.Status != StatusActive {
		// Akun diblokir setelah login: cabut token baru yang barusan dibuat.
		if err := s.repo.RevokeRefreshToken(ctx, nextHash, now); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, ErrUserBanned
	}
	return s.tokenPair(u, next, rt.ExpiresAt, now)
}

// Logout mencabut refresh token. Token kosong atau tidak dikenal tidak
// dianggap error, supaya logout selalu berhasil dari sisi user.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.repo.RevokeRefreshToken(ctx, HashRefreshToken(refreshToken), s.now())
}

func (s *Service) newRefreshRow(userID int64, hash string, client ClientInfo, now time.Time) RefreshToken {
	return RefreshToken{
		UserID:    userID,
		TokenHash: hash,
		UserAgent: truncate(client.UserAgent, 255),
		IPAddress: truncate(client.IPAddress, 45),
		ExpiresAt: now.Add(s.refreshTTL),
	}
}

func (s *Service) tokenPair(u User, refresh string, refreshExp, now time.Time) (TokenPair, error) {
	access, accessExp, err := s.tokens.AccessToken(u, now)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:           access,
		AccessTokenExpiresAt:  accessExp,
		RefreshToken:          refresh,
		RefreshTokenExpiresAt: refreshExp,
	}, nil
}

// truncate memotong string ke maksimal n karakter (bukan byte).
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
