package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/mail"
)

// Langkah 24c: "Lupa kata sandi?".
//
// Alurnya:
//  1. User mengisi email di /forgot-password. RequestPasswordReset SELALU
//     menjawab berhasil (selama format email benar), baik email itu
//     terdaftar atau tidak. Jika jawabannya berbeda, siapa pun bisa
//     mengecek email mana yang punya akun di Ice-Stream.
//  2. Jika email terdaftar dan akunnya aktif, sebuah token acak dibuat,
//     hash-nya disimpan, dan tautan berisi token dikirim ke email itu.
//  3. User membuka tautan, mengisi password baru. ResetPassword mengganti
//     password, menandai token terpakai, dan mencabut semua refresh token,
//     jadi semua perangkat yang sedang login harus login ulang.
//
// Langkah 2 berjalan di background (goroutine), bukan di dalam request.
// Alasannya: mencari user, menyimpan token, dan mengirim email butuh waktu.
// Jika dikerjakan di dalam request, email terdaftar akan dijawab lebih
// lambat daripada email yang tidak terdaftar, dan perbedaan waktu itu
// membocorkan hal yang sama seperti poin 1.
//
// Perbandingan: di Node biasanya dipakai antrean (BullMQ) atau sekadar
// "fire and forget" promise; di Spring ada @Async. Di sini cukup goroutine
// dengan batas jumlah (semaphore) dan WaitGroup supaya email yang sedang
// dikirim sempat selesai saat service dimatikan.

// Error untuk alur reset password.
var ErrInvalidResetToken = errors.New("tautan reset tidak valid atau sudah kedaluwarsa, minta tautan baru")

// Batas permintaan tautan per akun. Tanpa batas ini, orang lain bisa
// membanjiri kotak masuk seseorang dengan email reset.
const (
	resetMinInterval = time.Minute
	resetMaxPerHour  = 5
	// resetMaxInFlight membatasi berapa email yang boleh diproses
	// bersamaan. Permintaan di atas batas ini dibuang (dan dicatat di log).
	resetMaxInFlight = 16
	// resetJobTimeout membatasi satu pekerjaan background (cari user,
	// simpan token, kirim email).
	resetJobTimeout = 30 * time.Second
)

// Mailer mengirim email. Implementasinya mail.SMTPSender; test memakai
// versi palsu.
type Mailer interface {
	Send(ctx context.Context, m mail.Message) error
}

// PasswordResetToken adalah baris di tabel password_reset_tokens.
type PasswordResetToken struct {
	UserID    int64
	TokenHash string
	RequestIP string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// ResetRepository adalah akses database khusus reset password. Dipisah
// dari Repository supaya fitur ini bisa dinyalakan terpisah.
type ResetRepository interface {
	// RecentResetTokens mengembalikan jumlah token milik user yang dibuat
	// sejak since, dan waktu token terbaru (nol jika tidak ada).
	RecentResetTokens(ctx context.Context, userID int64, since time.Time) (count int, latest time.Time, err error)
	// CreateResetToken, dalam satu transaksi, menandai token lama milik
	// user yang belum terpakai sebagai terpakai, lalu menyimpan token baru.
	CreateResetToken(ctx context.Context, t PasswordResetToken) error
	// ResetPassword, dalam satu transaksi: mengunci token, memastikan
	// belum terpakai, belum kedaluwarsa, dan user-nya aktif; mengganti
	// password; menandai token terpakai; mencabut semua refresh token user.
	// Mengembalikan ErrInvalidResetToken jika token tidak bisa dipakai.
	ResetPassword(ctx context.Context, tokenHash, passwordHash string, now time.Time) (userID int64, err error)
}

// ResetConfig adalah pengaturan fitur reset password.
type ResetConfig struct {
	Repo   ResetRepository
	Mailer Mailer
	// BaseURL adalah alamat frontend, misalnya
	// "http://ice-stream.localhost:4000". Tautan di email =
	// BaseURL + "/reset-password?token=...".
	BaseURL string
	// TTL adalah umur tautan (misalnya 30 menit).
	TTL    time.Duration
	Logger *slog.Logger
}

// resetFeature menyimpan pengaturan dan pekerjaan background yang berjalan.
type resetFeature struct {
	ResetConfig
	sem chan struct{}
	wg  sync.WaitGroup
	// baseCtx dibatalkan saat service dimatikan lebih lama dari batas
	// waktu Close.
	baseCtx context.Context
	cancel  context.CancelFunc
}

// EnablePasswordReset menyalakan RequestPasswordReset dan ResetPassword.
// Tanpa ini, keduanya mengembalikan error (dipakai di test lama yang
// tidak butuh fitur ini).
func (s *Service) EnablePasswordReset(cfg ResetConfig) error {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("APP_BASE_URL harus berupa URL http(s), dapat %q", cfg.BaseURL)
	}
	if cfg.TTL < 5*time.Minute || cfg.TTL > 24*time.Hour {
		return fmt.Errorf("PASSWORD_RESET_TTL harus antara 5m dan 24h, dapat %s", cfg.TTL)
	}
	if cfg.Repo == nil || cfg.Mailer == nil || cfg.Logger == nil {
		return errors.New("reset password butuh Repo, Mailer, dan Logger")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	ctx, cancel := context.WithCancel(context.Background())
	s.reset = &resetFeature{
		ResetConfig: cfg,
		sem:         make(chan struct{}, resetMaxInFlight),
		baseCtx:     ctx,
		cancel:      cancel,
	}
	return nil
}

// Close menunggu email reset yang sedang dikirim selesai, paling lama
// sampai ctx habis. Dipanggil saat service dimatikan.
func (s *Service) Close(ctx context.Context) {
	if s.reset == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		s.reset.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		s.reset.cancel()
		<-done
	}
	s.reset.cancel()
}

// RequestPasswordReset menerima permintaan tautan reset. Hanya format
// email yang diperiksa di sini; sisanya dikerjakan di background.
func (s *Service) RequestPasswordReset(_ context.Context, email string, client ClientInfo) error {
	if s.reset == nil {
		return errors.New("reset password belum dinyalakan")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := netmail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return &ValidationError{"email", "format email tidak valid"}
	}

	select {
	case s.reset.sem <- struct{}{}:
	default:
		s.reset.Logger.Warn("permintaan reset password dibuang, terlalu banyak yang sedang diproses")
		return nil
	}
	s.reset.wg.Add(1)
	go func() {
		defer s.reset.wg.Done()
		defer func() { <-s.reset.sem }()
		// Context request sengaja TIDAK dipakai: request sudah selesai
		// (dan context-nya dibatalkan) sebelum pekerjaan ini selesai.
		ctx, cancel := context.WithTimeout(s.reset.baseCtx, resetJobTimeout)
		defer cancel()
		if err := s.sendResetLink(ctx, email, client); err != nil {
			s.reset.Logger.Error("gagal memproses reset password", slog.Any("error", err))
		}
	}()
	return nil
}

// sendResetLink adalah pekerjaan background RequestPasswordReset.
func (s *Service) sendResetLink(ctx context.Context, email string, client ClientInfo) error {
	// Username tidak boleh berisi "@", jadi pencarian ini hanya cocok
	// dengan kolom email.
	u, _, err := s.repo.FindUserByLogin(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.Status != StatusActive {
		return nil
	}

	now := s.now()
	count, latest, err := s.reset.Repo.RecentResetTokens(ctx, u.ID, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if count >= resetMaxPerHour || (!latest.IsZero() && now.Sub(latest) < resetMinInterval) {
		s.reset.Logger.Info("permintaan reset password dilewati karena terlalu sering", slog.Int64("user_id", u.ID))
		return nil
	}

	token, hash, err := NewRefreshToken()
	if err != nil {
		return err
	}
	expires := now.Add(s.reset.TTL)
	if err := s.reset.Repo.CreateResetToken(ctx, PasswordResetToken{
		UserID:    u.ID,
		TokenHash: hash,
		RequestIP: truncate(client.IPAddress, 45),
		ExpiresAt: expires,
		CreatedAt: now,
	}); err != nil {
		return err
	}

	link := s.reset.BaseURL + "/reset-password?token=" + url.QueryEscape(token)
	if err := s.reset.Mailer.Send(ctx, resetEmail(u, link, s.reset.TTL)); err != nil {
		return fmt.Errorf("kirim email reset ke user %d: %w", u.ID, err)
	}
	s.reset.Logger.Info("email reset password terkirim", slog.Int64("user_id", u.ID))
	return nil
}

// resetEmail menyusun isi email. Token hanya ada di tautan, tidak pernah
// ditulis ke log.
func resetEmail(u User, link string, ttl time.Duration) mail.Message {
	text := fmt.Sprintf(`Halo %s,

Kami menerima permintaan untuk mengatur ulang kata sandi akun Ice-Stream
@%s. Buka tautan berikut untuk membuat kata sandi baru:

%s

Tautan ini hanya bisa dipakai sekali dan berlaku %d menit.

Jika kamu tidak meminta ini, abaikan email ini. Kata sandimu tidak berubah.

Ice-Stream
`, u.DisplayName, u.Username, link, int(ttl.Minutes()))
	return mail.Message{To: u.Email, Subject: "Atur ulang kata sandi Ice-Stream", Text: text}
}

// ResetPassword mengganti password memakai token dari email.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if s.reset == nil {
		return errors.New("reset password belum dinyalakan")
	}
	// Token asli selalu 43 karakter base64url (32 byte). Yang lain pasti
	// tidak valid, jadi tidak perlu bertanya ke database.
	if len(token) != 43 {
		return ErrInvalidResetToken
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	userID, err := s.reset.Repo.ResetPassword(ctx, HashRefreshToken(token), hash, s.now())
	if err != nil {
		return err
	}
	s.reset.Logger.InfoContext(ctx, "password diganti lewat tautan reset", slog.Int64("user_id", userID))
	return nil
}
