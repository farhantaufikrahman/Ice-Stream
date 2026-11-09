package video

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo menyimpan video di memori, menggantikan Oracle saat test.
type fakeRepo struct {
	mu     sync.Mutex
	nextID int64
	videos map[int64]Video
}

func newFakeRepo() *fakeRepo { return &fakeRepo{videos: map[int64]Video{}} }

func (r *fakeRepo) Create(_ context.Context, nv NewVideo) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	v := Video{
		ID: r.nextID, OwnerID: nv.OwnerID, Title: nv.Title, Description: nv.Description,
		Status: StatusPendingUpload, ObjectKey: nv.ObjectKey, ContentType: nv.ContentType,
		SizeBytes: nv.SizeBytes, CreatedAt: time.Now().UTC(),
	}
	r.videos[v.ID] = v
	return v, nil
}

func (r *fakeRepo) FindByID(_ context.Context, id int64) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok {
		return Video{}, ErrNotFound
	}
	return v, nil
}

func (r *fakeRepo) MarkUploaded(_ context.Context, id int64, now time.Time) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok || v.Status != StatusPendingUpload {
		return Video{}, ErrNotUploading
	}
	v.Status, v.UploadedAt = StatusUploaded, &now
	r.videos[id] = v
	return v, nil
}

func (r *fakeRepo) ListByOwner(_ context.Context, ownerID, beforeID int64, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Video
	for id := r.nextID; id > 0 && len(out) < limit; id-- {
		v, ok := r.videos[id]
		if ok && v.OwnerID == ownerID && (beforeID == 0 || id < beforeID) {
			out = append(out, v)
		}
	}
	return out, nil
}

// fakeStore pura-pura menjadi object storage. objects diisi test untuk
// mensimulasikan file yang sudah diunggah browser.
type fakeStore struct {
	objects map[string]ObjectInfo
}

func (s *fakeStore) PresignPut(_ context.Context, key, contentType string, _ int64, ttl time.Duration) (PresignedRequest, error) {
	return PresignedRequest{URL: "http://storage.test/videos/" + key + "?sig=put", Headers: map[string]string{"Content-Type": contentType}, ExpiresAt: time.Now().Add(ttl)}, nil
}

func (s *fakeStore) PresignGet(_ context.Context, key string, ttl time.Duration) (PresignedRequest, error) {
	return PresignedRequest{URL: "http://storage.test/videos/" + key + "?sig=get", ExpiresAt: time.Now().Add(ttl)}, nil
}

func (s *fakeStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	info, ok := s.objects[key]
	if !ok {
		return ObjectInfo{}, ErrObjectNotFound
	}
	return info, nil
}

func newTestService() (*Service, *fakeRepo, *fakeStore) {
	repo, store := newFakeRepo(), &fakeStore{objects: map[string]ObjectInfo{}}
	svc := NewService(repo, store, Options{
		MaxUploadBytes: 10 << 20,
		UploadURLTTL:   15 * time.Minute,
		PlaybackURLTTL: time.Hour,
	})
	return svc, repo, store
}

func validInput() CreateUploadInput {
	return CreateUploadInput{OwnerID: 7, Title: "  Video pertama  ", Description: "halo", ContentType: "Video/MP4", SizeBytes: 1234}
}

func TestCreateUpload(t *testing.T) {
	svc, _, _ := newTestService()

	up, err := svc.CreateUpload(context.Background(), validInput())
	require.NoError(t, err)

	assert.Equal(t, "Video pertama", up.Video.Title, "judul dirapikan")
	assert.Equal(t, "video/mp4", up.Video.ContentType, "tipe dijadikan huruf kecil")
	assert.Equal(t, StatusPendingUpload, up.Video.Status)
	assert.Regexp(t, `^raw/[0-9a-f]{32}/source$`, up.Video.ObjectKey)
	assert.Contains(t, up.Request.URL, up.Video.ObjectKey)
	assert.Equal(t, "video/mp4", up.Request.Headers["Content-Type"])
}

func TestCreateUploadValidation(t *testing.T) {
	tests := map[string]func(*CreateUploadInput){
		"judul kosong":          func(in *CreateUploadInput) { in.Title = "   " },
		"judul terlalu panjang": func(in *CreateUploadInput) { in.Title = strings.Repeat("a", 101) },
		"deskripsi kepanjangan": func(in *CreateUploadInput) { in.Description = strings.Repeat("é", 1001) },
		"tipe bukan video":      func(in *CreateUploadInput) { in.ContentType = "image/png" },
		"ukuran nol":            func(in *CreateUploadInput) { in.SizeBytes = 0 },
		"ukuran melebihi batas": func(in *CreateUploadInput) { in.SizeBytes = 10<<20 + 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _, _ := newTestService()
			in := validInput()
			mutate(&in)
			_, err := svc.CreateUpload(context.Background(), in)
			var verr *ValidationError
			assert.ErrorAs(t, err, &verr)
		})
	}
}

func TestCompleteUpload(t *testing.T) {
	ctx := context.Background()
	svc, _, store := newTestService()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	id := up.Video.ID

	// File belum diunggah.
	_, err = svc.CompleteUpload(ctx, 7, id)
	assert.ErrorIs(t, err, ErrUploadIncomplete)

	// Ukuran file tidak sama dengan yang didaftarkan.
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 99, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 7, id)
	assert.ErrorIs(t, err, ErrUploadIncomplete)

	// User lain tidak boleh menyelesaikan upload milik orang lain.
	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 8, id)
	assert.ErrorIs(t, err, ErrNotFound)

	v, err := svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)
	assert.Equal(t, StatusUploaded, v.Status)
	require.NotNil(t, v.UploadedAt)

	// Memanggil ulang tetap berhasil (idempotent).
	again, err := svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)
	assert.Equal(t, v.UploadedAt, again.UploadedAt)

	_, err = svc.CompleteUpload(ctx, 7, 999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetVideoVisibility(t *testing.T) {
	ctx := context.Background()
	svc, _, store := newTestService()
	up, err := svc.CreateUpload(ctx, validInput())
	require.NoError(t, err)
	id := up.Video.ID

	// Selama PENDING_UPLOAD: hanya pemilik yang bisa melihat, tanpa URL putar.
	p, err := svc.GetVideo(ctx, 7, id)
	require.NoError(t, err)
	assert.Empty(t, p.Request.URL)
	_, err = svc.GetVideo(ctx, 8, id)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.GetVideo(ctx, 0, id)
	assert.ErrorIs(t, err, ErrNotFound)

	store.objects[up.Video.ObjectKey] = ObjectInfo{Size: 1234, ContentType: "video/mp4"}
	_, err = svc.CompleteUpload(ctx, 7, id)
	require.NoError(t, err)

	// Setelah UPLOADED: siapa pun bisa melihat, termasuk yang belum login.
	p, err = svc.GetVideo(ctx, 0, id)
	require.NoError(t, err)
	assert.Contains(t, p.Request.URL, "sig=get")
}

func TestListMyVideosPagination(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()
	for range 5 {
		_, err := svc.CreateUpload(ctx, validInput())
		require.NoError(t, err)
	}
	other := validInput()
	other.OwnerID = 8
	_, err := svc.CreateUpload(ctx, other)
	require.NoError(t, err)

	page1, next, err := svc.ListMyVideos(ctx, 7, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{5, 4}, ids(page1))
	assert.Equal(t, "4", next)

	page2, next, err := svc.ListMyVideos(ctx, 7, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, ids(page2))

	page3, next, err := svc.ListMyVideos(ctx, 7, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, ids(page3))
	assert.Empty(t, next, "halaman terakhir")

	_, _, err = svc.ListMyVideos(ctx, 7, 2, "bukan-angka")
	var verr *ValidationError
	assert.ErrorAs(t, err, &verr)
	_, _, err = svc.ListMyVideos(ctx, 7, 51, "")
	assert.ErrorAs(t, err, &verr)
}

func ids(videos []Video) []int64 {
	out := make([]int64, len(videos))
	for i, v := range videos {
		out[i] = v.ID
	}
	return out
}
