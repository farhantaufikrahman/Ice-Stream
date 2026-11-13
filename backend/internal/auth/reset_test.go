package auth

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/mail"
)

// fakeResetRepo menyimpan token reset di memori dan memakai fakeRepo untuk
// data user dan refresh token, supaya efek ResetPassword bisa dicek.
type fakeResetRepo struct {
	users  *fakeRepo
	mu     sync.Mutex
	tokens map[string]*fakeResetToken
}

type fakeResetToken struct {
	PasswordResetToken
	used bool
}

func (r *fakeResetRepo) RecentResetTokens(_ context.Context, userID int64, since time.Time) (int, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var (
		n      int
		latest time.Time
	)
	for _, t := range r.tokens {
		if t.UserID == userID && !t.CreatedAt.Before(since) {
			n++
			if t.CreatedAt.After(latest) {
				latest = t.CreatedAt
			}
		}
	}
	return n, latest, nil
}

func (r *fakeResetRepo) CreateResetToken(_ context.Context, t PasswordResetToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, old := range r.tokens {
		if old.UserID == t.UserID {
			old.used = true
		}
	}
	r.tokens[t.TokenHash] = &fakeResetToken{PasswordResetToken: t}
	return nil
}

func (r *fakeResetRepo) ResetPassword(_ context.Context, tokenHash, passwordHash string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[tokenHash]
	if !ok || t.used || !now.Before(t.ExpiresAt) {
		return 0, ErrInvalidResetToken
	}
	r.users.mu.Lock()
	defer r.users.mu.Unlock()
	u, ok := r.users.users[t.UserID]
	if !ok || u.Status != StatusActive {
		return 0, ErrInvalidResetToken
	}
	r.users.hashes[t.UserID] = passwordHash
	t.used = true
	for _, rt := range r.users.tokens {
		if rt.UserID == t.UserID {
			rt.revoked = true
		}
	}
	return t.UserID, nil
}

// fakeMailer mencatat email yang "dikirim".
type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *fakeMailer) messages() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.sent...)
}

type resetEnv struct {
	svc    *Service
	users  *fakeRepo
	resets *fakeResetRepo
	mailer *fakeMailer
	now    time.Time
}

func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()
	svc, users, _ := newTestService(t)
	env := &resetEnv{
		svc:    svc,
		users:  users,
		resets: &fakeResetRepo{users: users, tokens: map[string]*fakeResetToken{}},
		mailer: &fakeMailer{},
		now:    time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC),
	}
	svc.now = func() time.Time { return env.now }
	require.NoError(t, svc.EnablePasswordReset(ResetConfig{
		Repo:    env.resets,
		Mailer:  env.mailer,
		BaseURL: "http://ice-stream.localhost:4000/",
		TTL:     30 * time.Minute,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "budi", Email: "budi@example.com", Password: "rahasia123", DisplayName: "Budi",
	})
	require.NoError(t, err)
	return env
}

// request meminta tautan reset lalu menunggu pekerjaan background selesai.
func (e *resetEnv) request(t *testing.T, email string) {
	t.Helper()
	require.NoError(t, e.svc.RequestPasswordReset(context.Background(), email, ClientInfo{IPAddress: "10.0.0.1"}))
	e.svc.reset.wg.Wait()
}

// tokenFrom mengambil token dari tautan di email.
func tokenFrom(t *testing.T, m mail.Message) string {
	t.Helper()
	i := strings.Index(m.Text, "http://")
	require.GreaterOrEqual(t, i, 0, "email harus berisi tautan")
	line := m.Text[i:]
	line = line[:strings.IndexByte(line, '\n')]
	u, err := url.Parse(line)
	require.NoError(t, err)
	assert.Equal(t, "/reset-password", u.Path)
	assert.Equal(t, "ice-stream.localhost:4000", u.Host, "garis miring di akhir BaseURL dibuang")
	return u.Query().Get("token")
}

func TestRequestPasswordResetSendsLink(t *testing.T) {
	env := newResetEnv(t)
	env.request(t, "  BUDI@example.com ")

	msgs := env.mailer.messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "budi@example.com", msgs[0].To)
	assert.Contains(t, msgs[0].Text, "Halo Budi")
	assert.Contains(t, msgs[0].Text, "berlaku 30 menit")
	token := tokenFrom(t, msgs[0])
	assert.Len(t, token, 43)

	stored, ok := env.resets.tokens[HashRefreshToken(token)]
	require.True(t, ok, "yang disimpan hanya hash token")
	assert.Equal(t, env.now.Add(30*time.Minute), stored.ExpiresAt)
	assert.Equal(t, "10.0.0.1", stored.RequestIP)
}

func TestRequestPasswordResetHidesUnknownEmail(t *testing.T) {
	env := newResetEnv(t)
	env.request(t, "tidakada@example.com")
	assert.Empty(t, env.mailer.messages(), "email tidak terdaftar tetap dijawab berhasil, tapi tidak ada email")

	env.users.users[1] = User{ID: 1, Username: "budi", Email: "budi@example.com", Status: StatusBanned}
	env.request(t, "budi@example.com")
	assert.Empty(t, env.mailer.messages(), "akun diblokir tidak mendapat tautan")

	err := env.svc.RequestPasswordReset(context.Background(), "bukan-email", ClientInfo{})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "email", verr.Field)
}

func TestRequestPasswordResetThrottle(t *testing.T) {
	env := newResetEnv(t)
	env.request(t, "budi@example.com")
	env.request(t, "budi@example.com")
	assert.Len(t, env.mailer.messages(), 1, "kurang dari 1 menit: dilewati")

	for i := 0; i < 6; i++ {
		env.now = env.now.Add(2 * time.Minute)
		env.request(t, "budi@example.com")
	}
	assert.Len(t, env.mailer.messages(), 5, "maksimal 5 tautan per jam")

	env.now = env.now.Add(time.Hour)
	env.request(t, "budi@example.com")
	assert.Len(t, env.mailer.messages(), 6, "setelah 1 jam boleh lagi")
}

func TestResetPassword(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()
	_, pair, err := env.svc.Login(ctx, "budi", "rahasia123", ClientInfo{})
	require.NoError(t, err)

	env.request(t, "budi@example.com")
	token := tokenFrom(t, env.mailer.messages()[0])

	err = env.svc.ResetPassword(ctx, token, "pendek")
	var verr *ValidationError
	require.ErrorAs(t, err, &verr, "aturan password sama dengan saat daftar")

	require.NoError(t, env.svc.ResetPassword(ctx, token, "passwordBaru9"))

	_, _, err = env.svc.Login(ctx, "budi", "rahasia123", ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidCredentials, "password lama tidak berlaku")
	_, _, err = env.svc.Login(ctx, "budi", "passwordBaru9", ClientInfo{})
	assert.NoError(t, err)
	_, err = env.svc.Refresh(ctx, pair.RefreshToken, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidRefreshToken, "sesi lama dicabut")

	assert.ErrorIs(t, env.svc.ResetPassword(ctx, token, "passwordLain9"), ErrInvalidResetToken, "token hanya sekali pakai")
	assert.ErrorIs(t, env.svc.ResetPassword(ctx, "pendek", "passwordLain9"), ErrInvalidResetToken)
}

func TestResetPasswordExpiredOrReplaced(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()

	env.request(t, "budi@example.com")
	first := tokenFrom(t, env.mailer.messages()[0])
	env.now = env.now.Add(2 * time.Minute)
	env.request(t, "budi@example.com")
	second := tokenFrom(t, env.mailer.messages()[1])

	assert.ErrorIs(t, env.svc.ResetPassword(ctx, first, "passwordBaru9"), ErrInvalidResetToken, "tautan lama mati saat tautan baru diminta")

	env.now = env.now.Add(30 * time.Minute)
	assert.ErrorIs(t, env.svc.ResetPassword(ctx, second, "passwordBaru9"), ErrInvalidResetToken, "kedaluwarsa setelah 30 menit")
}

func TestEnablePasswordResetValidation(t *testing.T) {
	svc, _, _ := newTestService(t)
	base := ResetConfig{
		Repo: &fakeResetRepo{}, Mailer: &fakeMailer{}, BaseURL: "http://ice-stream.localhost:4000",
		TTL: 30 * time.Minute, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	assert.NoError(t, svc.EnablePasswordReset(base))

	bad := base
	bad.BaseURL = "ice-stream.localhost"
	assert.Error(t, svc.EnablePasswordReset(bad))
	bad = base
	bad.TTL = time.Minute
	assert.Error(t, svc.EnablePasswordReset(bad))

	plain, _, _ := newTestService(t)
	assert.Error(t, plain.ResetPassword(context.Background(), strings.Repeat("a", 43), "passwordBaru9"), "belum dinyalakan")
}
