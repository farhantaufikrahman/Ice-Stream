package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeIdentities adalah IdentityRepository di memori. Data user tetap di
// fakeRepo, jadi Login dan login penyedia melihat user yang sama.
type fakeIdentities struct {
	repo *fakeRepo
	// links: "provider/subject" -> user id.
	links    map[string]int64
	verified map[int64]bool
	codes    map[string]*fakeCode
}

type fakeCode struct {
	userID  int64
	expires time.Time
	used    bool
}

func newFakeIdentities(repo *fakeRepo) *fakeIdentities {
	return &fakeIdentities{repo: repo, links: map[string]int64{}, verified: map[int64]bool{}, codes: map[string]*fakeCode{}}
}

func (f *fakeIdentities) FindUserByIdentity(ctx context.Context, id Identity, _ time.Time) (User, error) {
	userID, ok := f.links[id.Provider+"/"+id.Subject]
	if !ok {
		return User{}, ErrNotFound
	}
	return f.repo.FindUserByID(ctx, userID)
}

func (f *fakeIdentities) FindUserByEmail(ctx context.Context, email string) (LinkedUser, error) {
	u, _, err := f.repo.FindUserByLogin(ctx, email)
	if err != nil || u.Email != email {
		return LinkedUser{}, ErrNotFound
	}
	return LinkedUser{User: u, EmailVerified: f.verified[u.ID]}, nil
}

func (f *fakeIdentities) LinkIdentity(_ context.Context, userID int64, id Identity, _ time.Time) error {
	key := id.Provider + "/" + id.Subject
	if _, ok := f.links[key]; ok {
		return ErrIdentityTaken
	}
	for k, uid := range f.links {
		if uid == userID && strings.HasPrefix(k, id.Provider+"/") {
			return ErrProviderAlreadyLinked
		}
	}
	f.links[key] = userID
	return nil
}

func (f *fakeIdentities) CreateUserWithIdentity(ctx context.Context, nu NewUser, id Identity, now time.Time) (User, error) {
	u, err := f.repo.CreateUser(ctx, nu)
	if err != nil {
		return User{}, err
	}
	f.verified[u.ID] = true
	return u, f.LinkIdentity(ctx, u.ID, id, now)
}

func (f *fakeIdentities) CreateLoginCode(_ context.Context, userID int64, codeHash string, expiresAt, _ time.Time) error {
	f.codes[codeHash] = &fakeCode{userID: userID, expires: expiresAt}
	return nil
}

func (f *fakeIdentities) ConsumeLoginCode(_ context.Context, codeHash string, now time.Time) (int64, error) {
	c, ok := f.codes[codeHash]
	if !ok || c.used || !now.Before(c.expires) {
		return 0, ErrInvalidLoginCode
	}
	c.used = true
	return c.userID, nil
}

func newIdentityService(t *testing.T) (*Service, *fakeRepo, *fakeIdentities, *time.Time) {
	t.Helper()
	svc, repo, _ := newTestService(t)
	ids := newFakeIdentities(repo)
	svc.EnableIdentityLogin(ids)
	clock := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }
	return svc, repo, ids, &clock
}

func githubUser(subject, email string) Identity {
	return Identity{Provider: ProviderGitHub, Subject: subject, Email: email, EmailVerified: true, Name: "Budi Santoso", UsernameHint: "Budi-Dev"}
}

func TestLoginWithIdentityCreatesUser(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newIdentityService(t)

	code, err := svc.LoginWithIdentity(ctx, githubUser("583231", " Budi@Example.com "))
	require.NoError(t, err)
	require.NotEmpty(t, code)

	u, pair, err := svc.ExchangeLoginCode(ctx, code, ClientInfo{UserAgent: "test"})
	require.NoError(t, err)
	assert.Equal(t, "budi_dev", u.Username, "dari login GitHub, dirapikan")
	assert.Equal(t, "budi@example.com", u.Email)
	assert.Equal(t, "Budi Santoso", u.DisplayName)
	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)
	assert.Empty(t, repo.hashes[u.ID], "tanpa kata sandi")

	// Kode hanya bisa dipakai sekali.
	_, _, err = svc.ExchangeLoginCode(ctx, code, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidLoginCode)

	// Login berikutnya memakai user yang sama.
	code, err = svc.LoginWithIdentity(ctx, githubUser("583231", "budi@example.com"))
	require.NoError(t, err)
	again, _, err := svc.ExchangeLoginCode(ctx, code, ClientInfo{})
	require.NoError(t, err)
	assert.Equal(t, u.ID, again.ID)

	// Tidak bisa login dengan kata sandi (kosong atau apa pun).
	for _, pw := range []string{"", "sembarang123"} {
		_, _, err = svc.Login(ctx, "budi_dev", pw, ClientInfo{})
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	}
}

func TestLoginWithIdentityUsernameTaken(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newIdentityService(t)
	_, err := svc.Register(ctx, RegisterInput{Username: "budi_dev", Email: "lain@example.com", Password: "rahasia123"})
	require.NoError(t, err)

	code, err := svc.LoginWithIdentity(ctx, githubUser("1", "budi@example.com"))
	require.NoError(t, err)
	u, _, err := svc.ExchangeLoginCode(ctx, code, ClientInfo{})
	require.NoError(t, err)
	assert.Regexp(t, `^budi_dev_\d{4}$`, u.Username)
}

func TestLoginWithIdentityLinksVerifiedEmailOnly(t *testing.T) {
	ctx := context.Background()
	svc, _, ids, _ := newIdentityService(t)
	existing, err := svc.Register(ctx, RegisterInput{Username: "budi", Email: "budi@example.com", Password: "rahasia123"})
	require.NoError(t, err)

	// Email belum terbukti milik pendaftar: jangan disambungkan.
	_, err = svc.LoginWithIdentity(ctx, githubUser("1", "budi@example.com"))
	assert.ErrorIs(t, err, ErrEmailNeedsPassword)

	// Setelah terbukti (misalnya lewat "Lupa kata sandi?"), disambungkan.
	ids.verified[existing.ID] = true
	code, err := svc.LoginWithIdentity(ctx, githubUser("1", "budi@example.com"))
	require.NoError(t, err)
	u, _, err := svc.ExchangeLoginCode(ctx, code, ClientInfo{})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, u.ID)
	// Kata sandi lama tetap berlaku.
	_, _, err = svc.Login(ctx, "budi", "rahasia123", ClientInfo{})
	assert.NoError(t, err)

	// Akun GitHub lain dengan email yang sama: user itu sudah punya GitHub.
	_, err = svc.LoginWithIdentity(ctx, githubUser("2", "budi@example.com"))
	assert.ErrorIs(t, err, ErrProviderAlreadyLinked)
	// Google boleh ditambahkan.
	_, err = svc.LoginWithIdentity(ctx, Identity{Provider: ProviderGoogle, Subject: "g-1", Email: "budi@example.com", EmailVerified: true})
	assert.NoError(t, err)
}

func TestLoginWithIdentityRejects(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, clock := newIdentityService(t)
	var verr *ValidationError

	_, err := svc.LoginWithIdentity(ctx, Identity{Provider: "facebook", Subject: "1", Email: "a@b.co", EmailVerified: true})
	assert.ErrorAs(t, err, &verr)
	_, err = svc.LoginWithIdentity(ctx, Identity{Provider: ProviderGoogle, Subject: " ", Email: "a@b.co", EmailVerified: true})
	assert.ErrorAs(t, err, &verr)
	_, err = svc.LoginWithIdentity(ctx, Identity{Provider: ProviderGoogle, Subject: "1", Email: "a@b.co"})
	assert.ErrorIs(t, err, ErrEmailNotVerified, "email belum diverifikasi penyedia")
	_, err = svc.LoginWithIdentity(ctx, Identity{Provider: ProviderGitHub, Subject: "1", Email: "bukan email", EmailVerified: true})
	assert.ErrorIs(t, err, ErrEmailNotVerified)

	// Kode kedaluwarsa.
	code, err := svc.LoginWithIdentity(ctx, githubUser("9", "c@d.co"))
	require.NoError(t, err)
	*clock = clock.Add(LoginCodeTTL)
	_, _, err = svc.ExchangeLoginCode(ctx, code, ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidLoginCode)
	_, _, err = svc.ExchangeLoginCode(ctx, "", ClientInfo{})
	assert.ErrorIs(t, err, ErrInvalidLoginCode)

	// Akun diblokir.
	code, err = svc.LoginWithIdentity(ctx, githubUser("9", "c@d.co"))
	require.NoError(t, err)
	for id, u := range repo.users {
		u.Status = StatusBanned
		repo.users[id] = u
	}
	_, _, err = svc.ExchangeLoginCode(ctx, code, ClientInfo{})
	assert.ErrorIs(t, err, ErrUserBanned)
	_, err = svc.LoginWithIdentity(ctx, githubUser("9", "c@d.co"))
	assert.ErrorIs(t, err, ErrUserBanned)
}

func TestUsernameBase(t *testing.T) {
	for in, want := range map[Identity]string{
		{UsernameHint: "Budi-Dev"}:                            "budi_dev",
		{Email: "siti.aminah+yt@gmail.com"}:                   "siti_aminah_yt",
		{Email: "ab@x.co"}:                                    "user_ab",
		{UsernameHint: "日本語"}:                                 "user",
		{UsernameHint: strings.Repeat("a", 40)}:               strings.Repeat("a", 30),
		{UsernameHint: "__x__", Email: "ignored@example.com"}: "user_x",
	} {
		got := usernameBase(in)
		assert.Equal(t, want, got, "%+v", in)
		assert.Regexp(t, usernamePattern, got)
	}
}
