package user

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo menyimpan profil di memori, menggantikan Oracle saat test.
type fakeRepo struct {
	mu    sync.Mutex
	users map[int64]Profile
	// setAvatarErr mensimulasikan Oracle yang gagal saat menyimpan avatar.
	setAvatarErr error
}

func (r *fakeRepo) FindByID(_ context.Context, id int64) (Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.users[id]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return p, nil
}

func (r *fakeRepo) FindByUsername(_ context.Context, username string) (Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.users {
		if p.Username == username {
			return p, nil
		}
	}
	return Profile{}, ErrNotFound
}

func (r *fakeRepo) FindByIDs(_ context.Context, ids []int64) ([]Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Profile
	for _, id := range ids {
		if p, ok := r.users[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *fakeRepo) UpdateProfile(_ context.Context, id int64, u ProfileUpdate, _ time.Time) (Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.users[id]
	if !ok {
		return Profile{}, ErrNotFound
	}
	if u.DisplayName != nil {
		p.DisplayName = *u.DisplayName
	}
	if u.Bio != nil {
		p.Bio = *u.Bio
	}
	r.users[id] = p
	return p, nil
}

func (r *fakeRepo) SetAvatarKey(_ context.Context, id int64, key string, _ time.Time) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setAvatarErr != nil {
		return "", r.setAvatarErr
	}
	p, ok := r.users[id]
	if !ok {
		return "", ErrNotFound
	}
	old := p.AvatarKey
	p.AvatarKey = key
	r.users[id] = p
	return old, nil
}

// fakeStore mencatat file yang tersimpan.
type fakeStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (s *fakeStore) Put(_ context.Context, key, _ string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = data
	return nil
}

func (s *fakeStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, key)
	return nil
}

func (s *fakeStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.files {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func newTestService() (*Service, *fakeRepo, *fakeStore) {
	repo := &fakeRepo{users: map[int64]Profile{
		7: {ID: 7, Username: "budi", DisplayName: "Budi", Email: "budi@example.com", Status: "ACTIVE"},
		8: {ID: 8, Username: "sari", DisplayName: "Sari", Email: "sari@example.com", Status: "ACTIVE"},
		9: {ID: 9, Username: "spam", DisplayName: "Spam", Email: "spam@example.com", Status: statusBanned},
	}}
	store := &fakeStore{files: map[string][]byte{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// fakeSubs (subscription_test.go) menambahkan method subscription.
	return NewService(&fakeSubs{fakeRepo: repo}, store, logger, "http://media.test/images"), repo, store
}

func TestGetProfile(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	// Pengunjung lain: tanpa email. Username tidak peka huruf besar/kecil.
	p, err := svc.GetProfile(ctx, 0, 0, " Budi ")
	require.NoError(t, err)
	assert.Equal(t, int64(7), p.ID)
	assert.Empty(t, p.Email)

	// Pemilik: dengan email.
	p, err = svc.GetProfile(ctx, 7, 7, "")
	require.NoError(t, err)
	assert.Equal(t, "budi@example.com", p.Email)

	// Akun diblokir: tidak terlihat oleh orang lain, tapi terlihat oleh dirinya.
	_, err = svc.GetProfile(ctx, 8, 9, "")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.GetProfile(ctx, 9, 9, "")
	assert.NoError(t, err)

	_, err = svc.GetProfile(ctx, 0, 0, "tidakada")
	assert.ErrorIs(t, err, ErrNotFound)
	var verr *ValidationError
	_, err = svc.GetProfile(ctx, 0, 0, "")
	assert.ErrorAs(t, err, &verr)
}

func TestBatchGetProfiles(t *testing.T) {
	svc, _, _ := newTestService()
	profiles, err := svc.BatchGetProfiles(context.Background(), []int64{8, 7, 7, 9, 404, 0})
	require.NoError(t, err)
	var ids []int64
	for _, p := range profiles {
		ids = append(ids, p.ID)
		assert.Empty(t, p.Email, "email tidak pernah ikut")
	}
	slices.Sort(ids)
	assert.Equal(t, []int64{7, 8}, ids, "id dobel, tidak ada, dan diblokir dilewati")

	tooMany := make([]int64, 101)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	_, err = svc.BatchGetProfiles(context.Background(), tooMany)
	var verr *ValidationError
	assert.ErrorAs(t, err, &verr)
}

func TestUpdateProfile(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	name, bio := "  Budi Santoso  ", "  Suka video kucing.  "

	p, err := svc.UpdateProfile(ctx, 7, ProfileUpdate{DisplayName: &name, Bio: &bio})
	require.NoError(t, err)
	assert.Equal(t, "Budi Santoso", p.DisplayName)
	assert.Equal(t, "Suka video kucing.", p.Bio)

	// Hanya bio yang dikirim (kosong = hapus bio); nama tidak berubah.
	empty := ""
	p, err = svc.UpdateProfile(ctx, 7, ProfileUpdate{Bio: &empty})
	require.NoError(t, err)
	assert.Equal(t, "Budi Santoso", p.DisplayName)
	assert.Empty(t, p.Bio)

	var verr *ValidationError
	blank, long := "   ", strings.Repeat("a", 51)
	longBio := strings.Repeat("é", 501)
	for _, u := range []ProfileUpdate{{DisplayName: &blank}, {DisplayName: &long}, {Bio: &longBio}} {
		_, err = svc.UpdateProfile(ctx, 7, u)
		assert.ErrorAs(t, err, &verr)
	}
}

func TestUploadAndRemoveAvatar(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()

	p, err := svc.UploadAvatar(ctx, 7, makePNG(t, 300, 300))
	require.NoError(t, err)
	first := repo.users[7].AvatarKey
	assert.Regexp(t, `^avatars/7/[0-9a-f]{16}\.jpg$`, first)
	assert.Equal(t, "budi@example.com", p.Email, "jawaban untuk pemilik")
	assert.Equal(t, []string{first}, store.keys())

	// Ganti foto: file lama dihapus, nama file baru berbeda.
	_, err = svc.UploadAvatar(ctx, 7, makePNG(t, 300, 300))
	require.NoError(t, err)
	second := repo.users[7].AvatarKey
	assert.NotEqual(t, first, second)
	assert.Equal(t, []string{second}, store.keys())

	_, err = svc.RemoveAvatar(ctx, 7)
	require.NoError(t, err)
	assert.Empty(t, repo.users[7].AvatarKey)
	assert.Empty(t, store.keys())

	// Gambar tidak valid: tidak ada yang disimpan.
	var verr *ValidationError
	_, err = svc.UploadAvatar(ctx, 7, []byte("bukan gambar"))
	assert.ErrorAs(t, err, &verr)
	assert.Empty(t, store.keys())
}

func TestUploadAvatarDatabaseFails(t *testing.T) {
	svc, repo, store := newTestService()
	repo.setAvatarErr = errors.New("ORA-03113")
	_, err := svc.UploadAvatar(context.Background(), 7, makePNG(t, 300, 300))
	require.Error(t, err)
	assert.Empty(t, store.keys(), "file baru dihapus lagi supaya tidak jadi sampah")
}

func TestAvatarURL(t *testing.T) {
	svc, _, _ := newTestService()
	assert.Equal(t, "http://media.test/images/avatars/7/ab.jpg", svc.AvatarURL("avatars/7/ab.jpg"))
	assert.Empty(t, svc.AvatarURL(""))
	assert.Empty(t, svc.AvatarURL("lain/7/ab.jpg"))
}
