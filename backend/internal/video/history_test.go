package video

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Versi palsu method riwayat di Repository. Perilakunya meniru query
// Oracle di history_repository.go.

func (r *fakeRepo) SaveProgress(_ context.Context, userID, videoID, positionMs int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.history[userID] == nil {
		r.history[userID] = map[int64]HistoryEntry{}
	}
	r.history[userID][videoID] = HistoryEntry{Video: Video{ID: videoID}, PositionMs: positionMs, WatchedAt: now}
	return nil
}

func (r *fakeRepo) GetProgress(_ context.Context, userID, videoID int64) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.history[userID][videoID]
	return e.PositionMs, ok, nil
}

func (r *fakeRepo) ListHistory(_ context.Context, userID int64, after *HistoryCursor, limit int) ([]HistoryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []HistoryEntry
	for _, e := range r.history[userID] {
		v, ok := r.videos[e.Video.ID]
		if !ok || v.Status != StatusReady {
			continue
		}
		if after != nil && !(e.WatchedAt.Before(after.WatchedAt) ||
			(e.WatchedAt.Equal(after.WatchedAt) && e.Video.ID < after.VideoID)) {
			continue
		}
		e.Video = v
		out = append(out, e)
	}
	// ORDER BY watched_at DESC, video_id DESC
	slices.SortFunc(out, func(a, b HistoryEntry) int {
		if c := b.WatchedAt.Compare(a.WatchedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.Video.ID, a.Video.ID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeRepo) DeleteHistory(_ context.Context, userID, videoID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if videoID == 0 {
		n := int64(len(r.history[userID]))
		delete(r.history, userID)
		return n, nil
	}
	if _, ok := r.history[userID][videoID]; !ok {
		return 0, nil
	}
	delete(r.history[userID], videoID)
	return 1, nil
}

// readyVideoWithDuration membuat video READY berdurasi durationMs.
func readyVideoWithDuration(repo *fakeRepo, durationMs int64) Video {
	v := readyVideo(repo, 1, time.Now())
	repo.mu.Lock()
	defer repo.mu.Unlock()
	v.DurationMs = durationMs
	repo.videos[v.ID] = v
	return v
}

// clockAt membuat jam palsu yang maju 1 detik setiap kali dibaca, supaya
// setiap SaveWatchProgress punya watched_at yang berbeda.
func clockAt(start time.Time) func() time.Time {
	t := start
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

func historyIDs(entries []HistoryEntry) []int64 {
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.Video.ID
	}
	return out
}

func TestResumePosition(t *testing.T) {
	const minute = 60_000
	cases := []struct {
		name          string
		pos, dur, out int64
	}{
		{"belum 5 detik", 4_999, 10 * minute, 0},
		{"tepat 5 detik", 5_000, 10 * minute, 5_000},
		{"di tengah", 4 * minute, 10 * minute, 4 * minute},
		{"sisa tepat 10 detik masih dilanjutkan", 10*minute - 10_000, 10 * minute, 10*minute - 10_000},
		{"sisa kurang dari 10 detik dianggap selesai", 10*minute - 9_999, 10 * minute, 0},
		{"sudah sampai akhir", 10 * minute, 10 * minute, 0},
		{"durasi belum diketahui", 30_000, 0, 0},
		{"video pendek", 6_000, 12_000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.out, ResumePosition(tc.pos, tc.dur))
		})
	}
}

func TestSaveWatchProgress(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	start := time.Date(2026, 11, 10, 8, 0, 0, 123456789, time.UTC)
	svc.now = clockAt(start)
	v := readyVideoWithDuration(repo, 120_000)

	p, err := svc.SaveWatchProgress(ctx, 8, v.ID, 30_000)
	require.NoError(t, err)
	assert.Equal(t, int64(30_000), p.PositionMs)
	assert.Equal(t, start.Add(time.Second).Truncate(time.Microsecond), p.WatchedAt,
		"dibulatkan ke mikrodetik seperti TIMESTAMP Oracle")

	// GetVideo memberi posisi untuk "lanjut menonton", hanya ke user itu.
	d, err := svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(30_000), d.ResumePositionMs)
	d, err = svc.GetVideo(ctx, 9, v.ID)
	require.NoError(t, err)
	assert.Zero(t, d.ResumePositionMs, "user lain belum pernah menonton")
	d, err = svc.GetVideo(ctx, 0, v.ID)
	require.NoError(t, err)
	assert.Zero(t, d.ResumePositionMs, "tanpa login")

	// Menonton lagi memperbarui entri yang sama, tidak menambah entri.
	_, err = svc.SaveWatchProgress(ctx, 8, v.ID, 60_000)
	require.NoError(t, err)
	assert.Len(t, repo.history[8], 1)
	assert.Equal(t, int64(60_000), repo.history[8][v.ID].PositionMs)

	// Posisi yang melewati durasi dipotong ke durasi, dan video yang sudah
	// selesai ditonton dibuka lagi dari awal.
	p, err = svc.SaveWatchProgress(ctx, 8, v.ID, 121_500)
	require.NoError(t, err)
	assert.Equal(t, int64(120_000), p.PositionMs)
	d, err = svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Zero(t, d.ResumePositionMs)

	t.Run("posisi negatif", func(t *testing.T) {
		_, err := svc.SaveWatchProgress(ctx, 8, v.ID, -1)
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, "position_ms", verr.Field)
	})
	t.Run("video belum READY", func(t *testing.T) {
		up := uploadedVideo(t, svc, store)
		_, err := svc.SaveWatchProgress(ctx, 7, up.ID, 1_000)
		assert.ErrorIs(t, err, ErrNotFound, "pemiliknya pun tidak bisa")
	})
	t.Run("video tidak ada", func(t *testing.T) {
		_, err := svc.SaveWatchProgress(ctx, 8, 999, 1_000)
		assert.ErrorIs(t, err, ErrNotFound)
		_, err = svc.SaveWatchProgress(ctx, 8, 0, 1_000)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("tanpa requester", func(t *testing.T) {
		_, err := svc.SaveWatchProgress(ctx, 0, v.ID, 1_000)
		assert.Error(t, err)
	})
}

func TestListWatchHistory(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	svc.now = clockAt(time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC))
	a := readyVideoWithDuration(repo, 60_000)
	b := readyVideoWithDuration(repo, 60_000)
	c := readyVideoWithDuration(repo, 60_000)

	// Urutan menonton: a, b, c, lalu a lagi. Yang terakhir ditonton di atas.
	for _, id := range []int64{a.ID, b.ID, c.ID, a.ID} {
		_, err := svc.SaveWatchProgress(ctx, 8, id, 10_000)
		require.NoError(t, err)
	}
	// Riwayat user lain tidak ikut.
	_, err := svc.SaveWatchProgress(ctx, 9, b.ID, 10_000)
	require.NoError(t, err)

	page1, next, err := svc.ListWatchHistory(ctx, 8, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID, c.ID}, historyIDs(page1))
	assert.Equal(t, int64(10_000), page1[0].PositionMs)
	assert.Equal(t, a.Title, page1[0].Video.Title, "berisi data video lengkap")
	require.NotEmpty(t, next)

	page2, next, err := svc.ListWatchHistory(ctx, 8, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID}, historyIDs(page2))
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
		all, _, err := svc.ListWatchHistory(ctx, 8, 0, "")
		require.NoError(t, err)
		assert.Equal(t, []int64{a.ID, c.ID}, historyIDs(all))
	})

	t.Run("hapus satu dan hapus semua", func(t *testing.T) {
		require.NoError(t, svc.DeleteWatchHistory(ctx, 8, c.ID))
		require.NoError(t, svc.DeleteWatchHistory(ctx, 8, c.ID), "aman dihapus dua kali")
		all, _, err := svc.ListWatchHistory(ctx, 8, 0, "")
		require.NoError(t, err)
		assert.Equal(t, []int64{a.ID, b.ID}, historyIDs(all))

		n, err := svc.ClearWatchHistory(ctx, 8)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		all, _, err = svc.ListWatchHistory(ctx, 8, 0, "")
		require.NoError(t, err)
		assert.Empty(t, all)

		other, _, err := svc.ListWatchHistory(ctx, 9, 0, "")
		require.NoError(t, err)
		assert.Len(t, other, 1, "riwayat user lain tidak ikut terhapus")
	})
}

func TestListWatchHistoryValidation(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService()

	bad := map[string]string{
		"bukan base64":   "%%%",
		"token feed":     encodeFeedCursor(FeedCursor{PublishedAt: time.Now(), ID: 1}),
		"token cari":     encodeSearchPage(2),
		"tanpa titik":    base64.RawURLEncoding.EncodeToString([]byte("h123")),
		"waktu negatif":  base64.RawURLEncoding.EncodeToString([]byte("h-5.1")),
		"id bukan angka": base64.RawURLEncoding.EncodeToString([]byte("h123.x")),
	}
	for name, token := range bad {
		t.Run(name, func(t *testing.T) {
			_, _, err := svc.ListWatchHistory(ctx, 8, 0, token)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr), "err = %v", err)
			assert.Equal(t, "page_token", verr.Field)
		})
	}

	_, _, err := svc.ListWatchHistory(ctx, 8, maxPageSize+1, "")
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "page_size", verr.Field)

	c := HistoryCursor{WatchedAt: time.Date(2026, 11, 10, 8, 0, 0, 123456000, time.UTC), VideoID: 42}
	got, err := decodeHistoryCursor(encodeHistoryCursor(c))
	require.NoError(t, err)
	assert.Equal(t, c, got, "encode lalu decode kembali sama")
}
