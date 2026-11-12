package video

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Versi palsu method edit/hapus di Repository dan ObjectStore. Perilakunya
// meniru manage_repository.go dan S3Store.DeletePrefix.

func (r *fakeRepo) UpdateDetails(_ context.Context, id, ownerID int64, d Details, terms []SearchTerm, now time.Time) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok || v.OwnerID != ownerID {
		return Video{}, ErrNotFound
	}
	v.Title, v.Description, v.Category, v.Tags = d.Title, d.Description, d.Category, d.Tags
	v.Visibility, v.Draft, v.Language = d.Visibility, d.Draft, d.Language
	if !v.Draft && v.Status == StatusReady && v.PublishedAt == nil {
		v.PublishedAt = &now
	}
	r.videos[id] = v
	r.terms[id] = terms
	return v, nil
}

func (r *fakeRepo) SoftDelete(_ context.Context, id, ownerID int64, allowed []string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok || v.OwnerID != ownerID {
		return ErrNotFound
	}
	if !slices.Contains(allowed, v.Status) {
		return ErrVideoBusy
	}
	delete(r.videos, id)
	delete(r.terms, id)
	r.trash[id] = v
	return nil
}

func (r *fakeRepo) ListUnpurged(_ context.Context, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Video
	for id, v := range r.trash {
		if !r.purged[id] {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b Video) int { return int(a.ID - b.ID) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeRepo) MarkPurged(_ context.Context, id int64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.trash[id]; !ok {
		return errors.New("video belum dihapus")
	}
	r.purged[id] = true
	return nil
}

func (s *fakeStore) DeletePrefix(_ context.Context, prefix string) (int, error) {
	if s.deleteErr != nil {
		return 0, s.deleteErr
	}
	n := 0
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			delete(s.objects, k)
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) SetThumbnailKey(_ context.Context, id, ownerID int64, key string, _ time.Time) (Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.videos[id]
	if !ok || v.OwnerID != ownerID {
		return Video{}, ErrNotFound
	}
	v.ThumbnailKey = key
	r.videos[id] = v
	return v, nil
}

func (s *fakeStore) Put(_ context.Context, key, _ string, data []byte) error {
	if s.objects == nil {
		s.objects = map[string]ObjectInfo{}
	}
	s.objects[key] = ObjectInfo{Size: int64(len(data)), ContentType: "image/jpeg"}
	return nil
}

func (s *fakeStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func ptr(s string) *string { return &s }

func TestUpdateVideo(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	up, err := svc.CreateUpload(ctx, validInput()) // milik user 7
	require.NoError(t, err)
	id := up.Video.ID

	// Judul saja: deskripsi tetap, kata pencarian dibuat ulang.
	v, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Title: ptr("  Belajar Golang  ")})
	require.NoError(t, err)
	assert.Equal(t, "Belajar Golang", v.Title, "judul dirapikan")
	assert.Equal(t, "halo", v.Description, "deskripsi tidak berubah")
	assert.Equal(t, []SearchTerm{{"belajar", WeightTitle}, {"golang", WeightTitle}, {"halo", WeightDescription}}, repo.terms[id])

	// Deskripsi saja, dan "" berarti menghapus deskripsi.
	v, err = svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: id, Description: ptr("")})
	require.NoError(t, err)
	assert.Equal(t, "Belajar Golang", v.Title)
	assert.Empty(t, v.Description)
	assert.Equal(t, []SearchTerm{{"belajar", WeightTitle}, {"golang", WeightTitle}}, repo.terms[id])

	cases := map[string]struct {
		in    UpdateInput
		field string
	}{
		"tanpa field":               {UpdateInput{RequesterID: 7, VideoID: id}, "title"},
		"judul kosong":              {UpdateInput{RequesterID: 7, VideoID: id, Title: ptr("   ")}, "title"},
		"judul terlalu panjang":     {UpdateInput{RequesterID: 7, VideoID: id, Title: ptr(strings.Repeat("a", 101))}, "title"},
		"deskripsi terlalu panjang": {UpdateInput{RequesterID: 7, VideoID: id, Description: ptr(strings.Repeat("é", 1001))}, "description"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.UpdateVideo(ctx, tc.in)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tc.field, verr.Field)
		})
	}

	t.Run("bukan pemilik", func(t *testing.T) {
		_, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 8, VideoID: id, Title: ptr("curang")})
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, "Belajar Golang", repo.videos[id].Title, "tidak berubah")
	})
	t.Run("video tidak ada", func(t *testing.T) {
		_, err := svc.UpdateVideo(ctx, UpdateInput{RequesterID: 7, VideoID: 999, Title: ptr("x")})
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDeleteVideo(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	v := readyVideo(repo, 7, time.Now())
	repo.mu.Lock()
	v.ObjectKey = "raw/0123456789abcdef0123456789abcdef/source"
	repo.videos[v.ID] = v
	repo.mu.Unlock()
	store.objects[v.ObjectKey] = ObjectInfo{}
	store.objects["hls/1/master.m3u8"] = ObjectInfo{}
	store.objects["hls/1/720p/seg0.ts"] = ObjectInfo{}
	// Video lain dengan id berawalan sama ("hls/10/") tidak boleh ikut terhapus.
	store.objects["hls/10/master.m3u8"] = ObjectInfo{}

	_, err := svc.DeleteVideo(ctx, 8, v.ID)
	assert.ErrorIs(t, err, ErrNotFound, "bukan pemilik")

	deleted, err := svc.DeleteVideo(ctx, 7, v.ID)
	require.NoError(t, err)
	assert.Equal(t, v.ID, deleted.ID)

	// Sudah hilang dari semua tempat.
	_, err = svc.GetVideo(ctx, 7, v.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.DeleteVideo(ctx, 7, v.ID)
	assert.ErrorIs(t, err, ErrNotFound, "hapus kedua kali = tidak ditemukan")
	feed, _, err := svc.ListVideos(ctx, FeedFilter{}, 0, "")
	require.NoError(t, err)
	assert.Empty(t, feed)

	// File belum dihapus sampai PurgeMedia dipanggil.
	assert.Len(t, store.objects, 4)
	n, err := svc.PurgeMedia(ctx, deleted)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, map[string]ObjectInfo{"hls/10/master.m3u8": {}}, store.objects)
	assert.True(t, repo.purged[v.ID])
}

func TestDeleteVideoStatus(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()

	t.Run("sedang diproses tidak boleh dihapus", func(t *testing.T) {
		v := uploadedVideo(t, svc, store)
		_, err := svc.DeleteVideo(ctx, 7, v.ID)
		assert.ErrorIs(t, err, ErrVideoBusy)
		_, err = repo.MarkProcessing(ctx, v.ID, time.Now())
		require.NoError(t, err)
		_, err = svc.DeleteVideo(ctx, 7, v.ID)
		assert.ErrorIs(t, err, ErrVideoBusy)

		// Setelah FAILED boleh dihapus.
		require.NoError(t, svc.MarkFailed(ctx, v.ID, "rusak"))
		_, err = svc.DeleteVideo(ctx, 7, v.ID)
		assert.NoError(t, err)
	})
	t.Run("belum diunggah boleh dihapus", func(t *testing.T) {
		up, err := svc.CreateUpload(ctx, validInput())
		require.NoError(t, err)
		_, err = svc.DeleteVideo(ctx, 7, up.Video.ID)
		assert.NoError(t, err)
	})
}

func TestPurgeDeleted(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	a := readyVideo(repo, 7, time.Now())
	b := readyVideo(repo, 7, time.Now())
	store.objects["hls/1/master.m3u8"] = ObjectInfo{}
	store.objects["hls/2/master.m3u8"] = ObjectInfo{}

	// Storage mati saat video dihapus: video tetap terhapus, file tertinggal.
	store.deleteErr = errors.New("storage mati")
	for _, v := range []Video{a, b} {
		deleted, err := svc.DeleteVideo(ctx, 7, v.ID)
		require.NoError(t, err)
		_, err = svc.PurgeMedia(ctx, deleted)
		require.Error(t, err)
	}
	assert.Len(t, store.objects, 2)
	assert.Empty(t, repo.purged)

	// Storage hidup lagi: PurgeDeleted (saat start) membersihkan semuanya.
	store.deleteErr = nil
	n, err := svc.PurgeDeleted(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Empty(t, store.objects)
	n, err = svc.PurgeDeleted(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "tidak ada yang tersisa")
}

func TestMediaPrefixes(t *testing.T) {
	assert.Equal(t, []string{"raw/abc/", "hls/42/"}, mediaPrefixes(Video{ID: 42, ObjectKey: "raw/abc/source"}))
	// Key yang aneh tidak pernah menghasilkan "raw/" atau "/" saja.
	assert.Equal(t, []string{"hls/42/"}, mediaPrefixes(Video{ID: 42, ObjectKey: "raw/source"}))
	assert.Equal(t, []string{"hls/42/"}, mediaPrefixes(Video{ID: 42, ObjectKey: ""}))
	assert.Equal(t, []string{"hls/42/"}, mediaPrefixes(Video{ID: 42, ObjectKey: "lain/abc/source"}))
}

func (s *fakeStore) PutStream(_ context.Context, key, contentType string, r io.Reader) (int64, error) {
	if s.putErr != nil {
		return 0, s.putErr
	}
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		return 0, err
	}
	if s.objects == nil {
		s.objects = map[string]ObjectInfo{}
	}
	s.objects[key] = ObjectInfo{Size: n, ContentType: contentType}
	return n, nil
}
