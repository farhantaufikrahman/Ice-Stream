package video

import (
	"cmp"
	"context"
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Versi palsu method Tonton nanti di Repository. Perilakunya meniru query
// Oracle di watchlater_repository.go.

func (r *fakeRepo) AddWatchLater(_ context.Context, userID, videoID int64, now time.Time) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.later[userID] == nil {
		r.later[userID] = map[int64]time.Time{}
	}
	if at, ok := r.later[userID][videoID]; ok {
		return at, nil // sudah tersimpan: tidak diubah
	}
	r.later[userID][videoID] = now
	return now, nil
}

func (r *fakeRepo) RemoveWatchLater(_ context.Context, userID, videoID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.later[userID], videoID)
	return nil
}

func (r *fakeRepo) IsInWatchLater(_ context.Context, userID, videoID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.later[userID][videoID]
	return ok, nil
}

func (r *fakeRepo) ListWatchLater(_ context.Context, userID int64, after *WatchLaterCursor, limit int) ([]WatchLaterEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []WatchLaterEntry
	for id, at := range r.later[userID] {
		v, ok := r.videos[id]
		if !ok || v.Status != StatusReady {
			continue
		}
		if after != nil && !(at.Before(after.AddedAt) || (at.Equal(after.AddedAt) && id < after.VideoID)) {
			continue
		}
		out = append(out, WatchLaterEntry{Video: v, AddedAt: at})
	}
	// ORDER BY added_at DESC, video_id DESC
	slices.SortFunc(out, func(a, b WatchLaterEntry) int {
		if c := b.AddedAt.Compare(a.AddedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.Video.ID, a.Video.ID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func watchLaterIDs(entries []WatchLaterEntry) []int64 {
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.Video.ID
	}
	return out
}

func TestAddToWatchLater(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	start := time.Date(2026, 11, 11, 8, 0, 0, 123456789, time.UTC)
	svc.now = clockAt(start)
	v := readyVideoWithDuration(repo, 60_000)

	at, err := svc.AddToWatchLater(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, start.Add(time.Second).Truncate(time.Microsecond), at, "dibulatkan ke mikrodetik")

	// Menyimpan lagi tidak mengubah waktu, jadi urutannya tidak melompat.
	again, err := svc.AddToWatchLater(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, at, again)
	assert.Len(t, repo.later[8], 1)

	// GetVideo memberi tahu tombol Simpan, hanya untuk user itu.
	d, err := svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.True(t, d.ViewerSaved)
	d, err = svc.GetVideo(ctx, 9, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerSaved, "user lain belum menyimpan")
	d, err = svc.GetVideo(ctx, 0, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerSaved, "tanpa login")

	// Dikeluarkan dua kali tetap aman.
	require.NoError(t, svc.RemoveFromWatchLater(ctx, 8, v.ID))
	require.NoError(t, svc.RemoveFromWatchLater(ctx, 8, v.ID))
	d, err = svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerSaved)

	t.Run("video belum READY", func(t *testing.T) {
		up := uploadedVideo(t, svc, store)
		_, err := svc.AddToWatchLater(ctx, 7, up.ID)
		assert.ErrorIs(t, err, ErrNotFound, "pemiliknya pun tidak bisa")
	})
	t.Run("video tidak ada", func(t *testing.T) {
		_, err := svc.AddToWatchLater(ctx, 8, 999)
		assert.ErrorIs(t, err, ErrNotFound)
		_, err = svc.AddToWatchLater(ctx, 8, 0)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("tanpa requester", func(t *testing.T) {
		_, err := svc.AddToWatchLater(ctx, 0, v.ID)
		assert.Error(t, err)
		assert.Error(t, svc.RemoveFromWatchLater(ctx, 0, v.ID))
	})
}

func TestListWatchLater(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	svc.now = clockAt(time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC))
	a := readyVideoWithDuration(repo, 60_000)
	b := readyVideoWithDuration(repo, 60_000)
	c := readyVideoWithDuration(repo, 60_000)

	// Disimpan berurutan a, b, c; menyimpan a lagi tidak memindahkannya.
	for _, id := range []int64{a.ID, b.ID, c.ID, a.ID} {
		_, err := svc.AddToWatchLater(ctx, 8, id)
		require.NoError(t, err)
	}
	_, err := svc.AddToWatchLater(ctx, 9, b.ID)
	require.NoError(t, err)

	page1, next, err := svc.ListWatchLater(ctx, 8, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{c.ID, b.ID}, watchLaterIDs(page1))
	assert.Equal(t, c.Title, page1[0].Video.Title, "berisi data video lengkap")
	require.NotEmpty(t, next)

	page2, next, err := svc.ListWatchLater(ctx, 8, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID}, watchLaterIDs(page2))
	assert.Empty(t, next, "halaman terakhir")

	t.Run("video yang tidak READY lagi tidak tampil", func(t *testing.T) {
		repo.mu.Lock()
		vb := repo.videos[b.ID]
		vb.Status = StatusFailed
		repo.videos[b.ID] = vb
		repo.mu.Unlock()
		t.Cleanup(func() {
			repo.mu.Lock()
			vb.Status = StatusReady
			repo.videos[b.ID] = vb
			repo.mu.Unlock()
		})
		all, _, err := svc.ListWatchLater(ctx, 8, 0, "")
		require.NoError(t, err)
		assert.Equal(t, []int64{c.ID, a.ID}, watchLaterIDs(all))
	})

	t.Run("daftar user lain terpisah", func(t *testing.T) {
		other, _, err := svc.ListWatchLater(ctx, 9, 0, "")
		require.NoError(t, err)
		assert.Equal(t, []int64{b.ID}, watchLaterIDs(other))
	})
}

func TestListWatchLaterValidation(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	_, _, err := svc.ListWatchLater(ctx, 8, maxPageSize+1, "")
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "page_size", verr.Field)

	historyToken := encodeHistoryCursor(HistoryCursor{WatchedAt: time.Now(), VideoID: 1})
	for name, token := range map[string]string{
		"bukan base64":   "%%%",
		"token riwayat":  historyToken,
		"tanpa titik":    base64.RawURLEncoding.EncodeToString([]byte("w123")),
		"id bukan angka": base64.RawURLEncoding.EncodeToString([]byte("w123.abc")),
		"waktu nol":      base64.RawURLEncoding.EncodeToString([]byte("w0.5")),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := svc.ListWatchLater(ctx, 8, 0, token)
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, "page_token", verr.Field)
		})
	}

	_, _, err = svc.ListWatchLater(ctx, 0, 0, "")
	assert.Error(t, err, "tanpa requester")
}

func TestWatchLaterCursorRoundTrip(t *testing.T) {
	c := WatchLaterCursor{AddedAt: time.Date(2026, 11, 11, 8, 0, 0, 123456000, time.UTC), VideoID: 42}
	got, err := decodeWatchLaterCursor(encodeWatchLaterCursor(c))
	require.NoError(t, err)
	assert.Equal(t, c, got)
}
