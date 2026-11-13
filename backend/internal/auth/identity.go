package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Masuk dengan Google / GitHub (langkah 26, migrasi V7).
//
// AuthService tidak berbicara dengan Google atau GitHub. Protokol OAuth
// dijalankan gateway; setelah gateway yakin siapa user itu di penyedia,
// gateway memanggil LoginWithIdentity. Pembagian ini sama dengan
// "Spring Security OAuth2 Client" di aplikasi Java atau Passport.js di
// Express: bagian web menangani redirect, bagian domain hanya menerima
// identitas yang sudah diperiksa.

// Penyedia login yang didukung, sama dengan CHECK constraint
// user_identities_provider_ck.
const (
	ProviderGoogle = "google"
	ProviderGitHub = "github"
)

// LoginCodeTTL adalah umur kode login sekali pakai. Browser langsung
// diarahkan ke aplikasi, jadi beberapa detik sudah cukup; dua menit
// memberi ruang untuk jaringan yang lambat.
const LoginCodeTTL = 2 * time.Minute

var (
	// ErrEmailNotVerified: penyedia tidak memberi email terverifikasi, jadi
	// akun baru tidak bisa dibuat (email wajib dan harus milik user itu).
	ErrEmailNotVerified = errors.New("akun ini tidak punya email terverifikasi")
	// ErrEmailNeedsPassword: email sudah dipakai akun Ice-Stream yang
	// emailnya belum terbukti milik pendaftarnya. Lihat LoginWithIdentity.
	ErrEmailNeedsPassword = errors.New("email ini sudah terdaftar; masuk dengan kata sandi, atau atur ulang kata sandi lewat \"Lupa kata sandi?\" lalu coba lagi")
	// ErrProviderAlreadyLinked: user Ice-Stream itu sudah terhubung ke akun
	// lain di penyedia yang sama.
	ErrProviderAlreadyLinked = errors.New("akun Ice-Stream dengan email ini sudah terhubung ke akun lain di penyedia yang sama")
	// ErrInvalidLoginCode: kode tidak ada, kedaluwarsa, atau sudah dipakai.
	ErrInvalidLoginCode = errors.New("kode login tidak valid atau kedaluwarsa")
)

// Identity adalah user menurut penyedia login, sudah diperiksa gateway.
type Identity struct {
	Provider string
	// Subject adalah id user di penyedia ("sub" Google, "id" GitHub).
	Subject       string
	Email         string
	EmailVerified bool
	// Name adalah nama tampilan dari penyedia (boleh kosong).
	Name string
	// UsernameHint adalah usulan username, misalnya login GitHub.
	// Boleh kosong; bagian depan email dipakai sebagai gantinya.
	UsernameHint string
}

// LinkedUser adalah user yang ditemukan lewat email, beserta status
// verifikasi emailnya.
type LinkedUser struct {
	User
	EmailVerified bool
}

// IdentityRepository adalah akses database untuk login lewat penyedia.
// Dipisah dari Repository, sama seperti ResetRepository.
type IdentityRepository interface {
	// FindUserByIdentity mencari user (yang belum dihapus) yang terhubung
	// ke (provider, subject), lalu mencatat last_login_at dan email
	// terbaru. ErrNotFound jika belum terhubung.
	FindUserByIdentity(ctx context.Context, id Identity, now time.Time) (User, error)
	// FindUserByEmail mencari user yang belum dihapus. ErrNotFound jika
	// tidak ada.
	FindUserByEmail(ctx context.Context, email string) (LinkedUser, error)
	// LinkIdentity menghubungkan id ke userID. ErrProviderAlreadyLinked
	// jika userID sudah punya akun lain di penyedia itu; ErrIdentityTaken
	// jika (provider, subject) sudah terhubung ke user lain (dua login
	// bersamaan).
	LinkIdentity(ctx context.Context, userID int64, id Identity, now time.Time) error
	// CreateUserWithIdentity, dalam satu transaksi, membuat user tanpa
	// kata sandi (email langsung terverifikasi) dan menghubungkan id.
	// ErrUsernameTaken, ErrEmailTaken, atau ErrIdentityTaken jika
	// melanggar UNIQUE constraint.
	CreateUserWithIdentity(ctx context.Context, u NewUser, id Identity, now time.Time) (User, error)
	// CreateLoginCode menyimpan hash kode login.
	CreateLoginCode(ctx context.Context, userID int64, codeHash string, expiresAt, now time.Time) error
	// ConsumeLoginCode, dalam satu transaksi, mengunci kode, memastikan
	// belum dipakai dan belum kedaluwarsa, lalu menandainya terpakai.
	// ErrInvalidLoginCode jika tidak bisa dipakai.
	ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (userID int64, err error)
}

// ErrIdentityTaken: (provider, subject) sudah terhubung ke user lain.
// Hanya terjadi saat dua login pertama berjalan bersamaan; Service
// mengulang pencarian.
var ErrIdentityTaken = errors.New("akun penyedia sudah terhubung")

// EnableIdentityLogin menyalakan LoginWithIdentity dan ExchangeLoginCode.
func (s *Service) EnableIdentityLogin(repo IdentityRepository) {
	s.identities = repo
}

// LoginWithIdentity mencari atau membuat user untuk id, lalu membuat kode
// login sekali pakai. Gateway menaruh kode ini di URL aplikasi, dan
// aplikasi menukarnya dengan ExchangeLoginCode.
//
// Aturan menghubungkan akun:
//  1. (provider, subject) sudah terhubung: pakai user itu.
//  2. Email terverifikasi di penyedia sama dengan email user yang emailnya
//     juga sudah terverifikasi di Ice-Stream (lewat login penyedia, atau
//     lewat "Lupa kata sandi?" yang membuktikan pemilik email): hubungkan.
//  3. Email sama tetapi belum terverifikasi di Ice-Stream: tolak
//     (ErrEmailNeedsPassword). Tanpa aturan ini, orang lain bisa mendaftar
//     duluan memakai email korban dengan kata sandinya sendiri, lalu ikut
//     masuk ke akun itu setelah korban memakai Google ("account
//     pre-hijacking").
//  4. Email belum dipakai: buat user baru tanpa kata sandi.
func (s *Service) LoginWithIdentity(ctx context.Context, id Identity) (string, error) {
	if s.identities == nil {
		return "", errors.New("login penyedia belum dinyalakan")
	}
	id, err := cleanIdentity(id)
	if err != nil {
		return "", err
	}
	now := s.now()
	u, err := s.findOrCreateUser(ctx, id, now)
	if err != nil {
		return "", err
	}
	if u.Status != StatusActive {
		return "", ErrUserBanned
	}
	code, hash, err := NewRefreshToken()
	if err != nil {
		return "", err
	}
	if err := s.identities.CreateLoginCode(ctx, u.ID, hash, now.Add(LoginCodeTTL), now); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Service) findOrCreateUser(ctx context.Context, id Identity, now time.Time) (User, error) {
	// Dua kali: percobaan kedua hanya untuk login pertama yang berjalan
	// bersamaan (ErrIdentityTaken), yang kalah cukup membaca hasil yang
	// menang.
	for attempt := 0; ; attempt++ {
		u, err := s.identities.FindUserByIdentity(ctx, id, now)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return u, err
		}
		u, err = s.linkOrCreate(ctx, id, now)
		if errors.Is(err, ErrIdentityTaken) && attempt == 0 {
			continue
		}
		return u, err
	}
}

func (s *Service) linkOrCreate(ctx context.Context, id Identity, now time.Time) (User, error) {
	if !id.EmailVerified || id.Email == "" {
		return User{}, ErrEmailNotVerified
	}
	existing, err := s.identities.FindUserByEmail(ctx, id.Email)
	switch {
	case err == nil:
		if !existing.EmailVerified {
			return User{}, ErrEmailNeedsPassword
		}
		if err := s.identities.LinkIdentity(ctx, existing.ID, id, now); err != nil {
			return User{}, err
		}
		return existing.User, nil
	case !errors.Is(err, ErrNotFound):
		return User{}, err
	}

	nu := NewUser{Email: id.Email, DisplayName: displayNameFor(id)}
	// Username yang diusulkan bisa sudah dipakai; coba beberapa akhiran
	// acak. Lima kali sudah sangat cukup untuk 10.000 kemungkinan akhiran.
	base := usernameBase(id)
	for attempt := 0; attempt < 5; attempt++ {
		nu.Username = base
		if attempt > 0 {
			suffix, err := randomDigits(4)
			if err != nil {
				return User{}, err
			}
			nu.Username = base[:min(len(base), 25)] + "_" + suffix
		}
		u, err := s.identities.CreateUserWithIdentity(ctx, nu, id, now)
		if errors.Is(err, ErrUsernameTaken) {
			continue
		}
		if errors.Is(err, ErrEmailTaken) {
			// Email baru saja dipakai pendaftaran lain: ulangi dari atas.
			return User{}, ErrIdentityTaken
		}
		return u, err
	}
	return User{}, fmt.Errorf("tidak menemukan username kosong untuk %q", base)
}

// ExchangeLoginCode menukar kode dari LoginWithIdentity dengan pasangan
// token, sama seperti hasil Login.
func (s *Service) ExchangeLoginCode(ctx context.Context, code string, client ClientInfo) (User, TokenPair, error) {
	if s.identities == nil {
		return User{}, TokenPair{}, errors.New("login penyedia belum dinyalakan")
	}
	if code == "" || len(code) > 100 {
		return User{}, TokenPair{}, ErrInvalidLoginCode
	}
	now := s.now()
	userID, err := s.identities.ConsumeLoginCode(ctx, HashRefreshToken(code), now)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	u, err := s.repo.FindUserByID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return User{}, TokenPair{}, ErrInvalidLoginCode
	}
	if err != nil {
		return User{}, TokenPair{}, err
	}
	if u.Status != StatusActive {
		return User{}, TokenPair{}, ErrUserBanned
	}
	return s.issueTokens(ctx, u, client, now)
}

// cleanIdentity memeriksa data dari gateway dan merapikan email.
func cleanIdentity(id Identity) (Identity, error) {
	if id.Provider != ProviderGoogle && id.Provider != ProviderGitHub {
		return Identity{}, &ValidationError{"provider", "harus google atau github"}
	}
	id.Subject = strings.TrimSpace(id.Subject)
	if id.Subject == "" || utf8.RuneCountInString(id.Subject) > 100 {
		return Identity{}, &ValidationError{"subject", "wajib diisi, maksimal 100 karakter"}
	}
	id.Email = strings.ToLower(strings.TrimSpace(id.Email))
	if id.Email != "" {
		addr, err := mail.ParseAddress(id.Email)
		if err != nil || addr.Address != id.Email || len(id.Email) > 254 {
			// Email aneh dari penyedia diperlakukan seperti tidak ada.
			id.Email, id.EmailVerified = "", false
		}
	}
	return id, nil
}

var notUsernameChar = regexp.MustCompile(`[^a-z0-9_]+`)

// usernameBase membuat usulan username yang lolos usernamePattern dari
// UsernameHint atau bagian depan email, misalnya "Budi.Santoso" menjadi
// "budi_santoso".
func usernameBase(id Identity) string {
	raw := id.UsernameHint
	if raw == "" {
		raw, _, _ = strings.Cut(id.Email, "@")
	}
	s := notUsernameChar.ReplaceAllString(strings.ToLower(raw), "_")
	s = strings.Trim(s, "_")
	if len(s) > 30 {
		s = s[:30]
	}
	if len(s) < 3 {
		s = "user_" + s
		s = strings.TrimRight(s, "_")
	}
	return s
}

// displayNameFor: nama dari penyedia (maksimal 50 karakter), atau
// UsernameHint, atau bagian depan email.
func displayNameFor(id Identity) string {
	name := strings.TrimSpace(id.Name)
	if name == "" {
		name = id.UsernameHint
	}
	if name == "" {
		name, _, _ = strings.Cut(id.Email, "@")
	}
	return truncate(name, 50)
}

// randomDigits membuat n angka acak, misalnya "0427".
func randomDigits(n int) (string, error) {
	var b strings.Builder
	for range n {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String(), nil
}
