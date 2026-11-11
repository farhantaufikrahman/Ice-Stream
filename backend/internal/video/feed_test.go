package video

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readyVideo menyimpan video READY milik ownerID yang selesai diproses
// pada processedAt, langsung ke fakeRepo.
func readyVideo(repo *fakeRepo, ownerID int64, processedAt time.Time) Video {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.nextID++
	v := Video{
		ID: repo.nextID, OwnerID: ownerID, Title: "Video", Status: StatusReady,
		PlaylistKey: "hls/x/master.m3u8", ProcessedAt: &processedAt,
	}
	repo.videos[v.ID] = v
	return v
}

func TestListVideosFeed(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	base := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

	// id 1..3 READY. id 2 dan 3 selesai di waktu yang sama, jadi urutannya
	// ditentukan id. id 1 diunggah paling dulu tapi selesai paling akhir,
	// jadi tampil paling atas.
	readyVideo(repo, 7, base.Add(2*time.Minute))
	readyVideo(repo, 8, base)
	readyVideo(repo, 7, base)
	// Video yang belum READY tidak boleh muncul di feed.
	uploadedVideo(t, svc, store)

	page1, next, err := svc.ListVideos(ctx, FeedFilter{}, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3}, ids(page1))
	require.NotEmpty(t, next)

	page2, next, err := svc.ListVideos(ctx, FeedFilter{}, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{2}, ids(page2))
	assert.Empty(t, next, "halaman terakhir")

	all, _, err := svc.ListVideos(ctx, FeedFilter{}, 0, "")
	require.NoError(t, err)
	assert.Len(t, all, 3, "page_size 0 = default 20")

	// Halaman channel user 7: hanya video miliknya.
	mine, next, err := svc.ListVideos(ctx, FeedFilter{OwnerIDs: []int64{7}}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3}, ids(mine))
	assert.Empty(t, next)

	// Feed subscription: video milik user 7 atau 8. Id dobel dan id <= 0
	// dibuang, user 99 tidak punya video.
	subs, _, err := svc.ListVideos(ctx, FeedFilter{OwnerIDs: []int64{8, 7, 7, 0, 99}}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3, 2}, ids(subs))
}

func TestListVideosValidation(t *testing.T) {
	svc, _, _ := newTestService()
	var verr *ValidationError
	for _, token := range []string{"42", "!!!", encodeRaw("abc"), encodeRaw("123"), encodeRaw("123.-1")} {
		_, _, err := svc.ListVideos(context.Background(), FeedFilter{}, 10, token)
		assert.ErrorAs(t, err, &verr, "token %q", token)
	}
	_, _, err := svc.ListVideos(context.Background(), FeedFilter{}, 51, "")
	assert.ErrorAs(t, err, &verr)

	tooMany := make([]int64, maxFeedOwners+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	_, _, err = svc.ListVideos(context.Background(), FeedFilter{OwnerIDs: tooMany}, 10, "")
	assert.ErrorAs(t, err, &verr, "lebih dari 1000 channel")
}

func TestFeedCursorRoundTrip(t *testing.T) {
	c := FeedCursor{ProcessedAt: time.Date(2026, 11, 10, 8, 0, 0, 123456000, time.UTC), ID: 42}
	got, err := decodeFeedCursor(encodeFeedCursor(c))
	require.NoError(t, err)
	assert.True(t, c.ProcessedAt.Equal(got.ProcessedAt))
	assert.Equal(t, c.ID, got.ID)
}

func TestRecordView(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	v := readyVideo(repo, 7, time.Now())

	r, err := svc.RecordView(ctx, v.ID, "u-8")
	require.NoError(t, err)
	assert.Equal(t, ViewResult{Counted: true, ViewCount: 1}, r)

	// Penonton yang sama dalam window: tidak dihitung lagi.
	r, err = svc.RecordView(ctx, v.ID, "u-8")
	require.NoError(t, err)
	assert.Equal(t, ViewResult{Counted: false, ViewCount: 1}, r)

	// Penonton lain: dihitung.
	r, err = svc.RecordView(ctx, v.ID, "a-0123abcd")
	require.NoError(t, err)
	assert.Equal(t, ViewResult{Counted: true, ViewCount: 2}, r)

	// Video yang belum READY dan video yang tidak ada: NOT_FOUND.
	pending := uploadedVideo(t, svc, store)
	_, err = svc.RecordView(ctx, pending.ID, "u-8")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.RecordView(ctx, 999, "u-8")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRecordViewInvalidKey(t *testing.T) {
	svc, repo, _ := newTestService()
	v := readyVideo(repo, 7, time.Now())
	var verr *ValidationError
	for _, key := range []string{"", "u.8", "a*", "spasi tidak boleh", string(make([]byte, 65))} {
		_, err := svc.RecordView(context.Background(), v.ID, key)
		assert.ErrorAs(t, err, &verr, "key %q", key)
	}
}

func TestRecordViewFailures(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	v := readyVideo(repo, 7, time.Now())
	views := svc.views.(*fakeViews)

	// NATS mati: view tidak dihitung, error bisa dikenali.
	views.err = errors.New("nats: no responders")
	_, err := svc.RecordView(ctx, v.ID, "u-8")
	assert.ErrorIs(t, err, ErrViewsUnavailable)
	views.err = nil

	// Oracle gagal: tanda di NATS dihapus lagi, jadi percobaan ulang dihitung.
	repo.incrementErr = errors.New("ORA-03113")
	_, err = svc.RecordView(ctx, v.ID, "u-8")
	require.Error(t, err)
	repo.incrementErr = nil
	r, err := svc.RecordView(ctx, v.ID, "u-8")
	require.NoError(t, err)
	assert.Equal(t, ViewResult{Counted: true, ViewCount: 1}, r)
}

// encodeRaw membuat token base64 dengan isi bebas, untuk menguji token rusak.
func encodeRaw(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
