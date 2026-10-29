package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// fakeRepo menyimpan data di memori, pengganti Oracle untuk unit test.
// Di Node ini mirip membuat mock repository dengan jest.fn().
type fakeRepo struct {
	mu     sync.Mutex
	users  map[int64]User
	hashes map[int64]string
	tokens map[string]*fakeToken
	nextID int64
}

type fakeToken struct {
	RefreshToken
	revoked bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{users: map[int64]User{}, hashes: map[int64]string{}, tokens: map[string]*fakeToken{}}
}

func (r *fakeRepo) CreateUser(_ context.Context, nu NewUser) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Username == nu.Username {
			return User{}, ErrUsernameTaken
		}
		if u.Email == nu.Email {
			return User{}, ErrEmailTaken
		}
	}
	r.nextID++
	u := User{ID: r.nextID, Username: nu.Username, Email: nu.Email, DisplayName: nu.DisplayName, Role: RoleUser, Status: StatusActive}
	r.users[u.ID] = u
	r.hashes[u.ID] = nu.PasswordHash
	return u, nil
}

func (r *fakeRepo) FindUserByLogin(_ context.Context, login string) (User, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, u := range r.users {
		if u.Username == login || u.Email == login {
			return u, r.hashes[id], nil
		}
	}
	return User{}, "", ErrNotFound
}

func (r *fakeRepo) FindUserByID(_ context.Context, id int64) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (r *fakeRepo) CreateRefreshToken(_ context.Context, t RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[t.TokenHash] = &fakeToken{RefreshToken: t}
	return nil
}

func (r *fakeRepo) RotateRefreshToken(_ context.Context, oldHash string, next RefreshToken, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.tokens[oldHash]
	if !ok {
		return 0, ErrInvalidRefreshToken
	}
	if old.revoked {
		for _, t := range r.tokens {
			if t.UserID == old.UserID {
				t.revoked = true
			}
		}
		return 0, ErrInvalidRefreshToken
	}
	if !now.Before(old.ExpiresAt) {
		return 0, ErrInvalidRefreshToken
	}
	old.revoked = true
	next.UserID = old.UserID
	r.tokens[next.TokenHash] = &fakeToken{RefreshToken: next}
	return old.UserID, nil
}

func (r *fakeRepo) RevokeRefreshToken(_ context.Context, hash string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tokens[hash]; ok {
		t.revoked = true
	}
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeRepo, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	repo := newFakeRepo()
	svc, err := NewService(repo, NewTokenMaker(priv, 15*time.Minute), 30*24*time.Hour)
	require.NoError(t, err)
	return svc, repo, pub
}

func TestRegisterNormalizesInput(t *testing.T) {
	svc, _, _ := newTestService(t)

	u, err := svc.Register(context.Background(), RegisterInput{
		Username: "  Kyrie_Light ", Email: "Kyrie@Example.com", Password: "rahasia123",
	})
	require.NoError(t, err)
	assert.Equal(t, "kyrie_light", u.Username)
	assert.Equal(t, "kyrie@example.com", u.Email)
	assert.Equal(t, "kyrie_light", u.DisplayName, "display name default = username")
}

func TestRegisterValidation(t *testing.T) {
	valid := RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"}
	tests := map[string]struct {
		change func(*RegisterInput)
		field  string
	}{
		"username terlalu pendek":      {func(in *RegisterInput) { in.Username = "ab" }, "username"},
		"username ada spasi":           {func(in *RegisterInput) { in.Username = "kyrie light" }, "username"},
		"email tidak valid":            {func(in *RegisterInput) { in.Email = "bukan-email" }, "email"},
		"email dengan nama":            {func(in *RegisterInput) { in.Email = "Kyrie <kyrie@example.com>" }, "email"},
		"password terlalu pendek":      {func(in *RegisterInput) { in.Password = "1234567" }, "password"},
		"password lebih 72 byte":       {func(in *RegisterInput) { in.Password = string(make([]byte, 73)) }, "password"},
		"display name terlalu panjang": {func(in *RegisterInput) { in.DisplayName = string(make([]rune, 51)) }, "display_name"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			in := valid
			tt.change(&in)
			_, err := svc.Register(context.Background(), in)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tt.field, verr.Field)
		})
	}
}

func TestRegisterDuplicate(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"})
	require.NoError(t, err)

	_, err = svc.Register(ctx, RegisterInput{Username: "KYRIE", Email: "lain@example.com", Password: "rahasia123"})
	assert.ErrorIs(t, err, ErrUsernameTaken)
}

func TestLoginIssuesVerifiableAccessToken(t *testing.T) {
	svc, _, pub := newTestService(t)
	ctx := context.Background()
	reg, err := svc.Register(ctx, RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"})
	require.NoError(t, err)

	_, pair, err := svc.Login(ctx, "KYRIE@example.com", "rahasia123", ClientInfo{})
	require.NoError(t, err)
	assert.NotEmpty(t, pair.RefreshToken)

	// Periksa token dengan kunci PUBLIK, persis seperti yang nanti dilakukan gateway.
	var claims AccessClaims
	_, err = jwt.ParseWithClaims(pair.AccessToken, &claims, func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(TokenIssuer), jwt.WithAudience(TokenAudience))
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(reg.ID, 10), claims.Subject)
	assert.Equal(t, RoleUser, claims.Role)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), claims.ExpiresAt.Time, 5*time.Second)
}

func TestLoginFailures(t *testing.T) {
	svc, repo, _ := newTestService(t)
	ctx := context.Background()
	u, err := svc.Register(ctx, RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"})
	require.NoError(t, err)

	_, _, err = svc.Login(ctx, "kyrie", "password-salah", ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	_, _, err = svc.Login(ctx, "tidak-ada", "rahasia123", ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidCredentials, "user tidak ada harus terlihat sama dengan password salah")

	u.Status = StatusBanned
	repo.users[u.ID] = u
	_, _, err = svc.Login(ctx, "kyrie", "rahasia123", ClientInfo{})
	assert.ErrorIs(t, err, ErrUserBanned)
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"})
	require.NoError(t, err)
	_, first, err := svc.Login(ctx, "kyrie", "rahasia123", ClientInfo{})
	require.NoError(t, err)

	second, err := svc.Refresh(ctx, first.RefreshToken, ClientInfo{})
	require.NoError(t, err)
	assert.NotEqual(t, first.RefreshToken, second.RefreshToken)

	// Token lama dipakai lagi: ditolak, dan token baru ikut dicabut.
	_, err = svc.Refresh(ctx, first.RefreshToken, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	_, err = svc.Refresh(ctx, second.RefreshToken, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
}

func TestRefreshExpired(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"})
	require.NoError(t, err)
	_, pair, err := svc.Login(ctx, "kyrie", "rahasia123", ClientInfo{})
	require.NoError(t, err)

	// Majukan "jam" Service melewati umur refresh token.
	svc.now = func() time.Time { return time.Now().UTC().Add(31 * 24 * time.Hour) }
	_, err = svc.Refresh(ctx, pair.RefreshToken, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, RegisterInput{Username: "kyrie", Email: "kyrie@example.com", Password: "rahasia123"})
	require.NoError(t, err)
	_, pair, err := svc.Login(ctx, "kyrie", "rahasia123", ClientInfo{})
	require.NoError(t, err)

	require.NoError(t, svc.Logout(ctx, pair.RefreshToken))
	require.NoError(t, svc.Logout(ctx, pair.RefreshToken), "logout kedua kali tetap sukses")
	_, err = svc.Refresh(ctx, pair.RefreshToken, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
}

func TestMain(m *testing.M) {
	// Hash bcrypt asli sengaja lambat; di test cukup pakai biaya minimum.
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
