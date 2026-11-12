package danmaku

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Versi palsu method langkah 26 di Repository. Perilakunya meniru query di
// oracle_repository.go.

func (f *fakeRepo) ListByChannel(_ context.Context, ownerID, videoID, beforeID int64, limit int) ([]ChannelDanmaku, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ChannelDanmaku
	for _, d := range f.items {
		if f.owners[d.VideoID] != ownerID || (videoID > 0 && d.VideoID != videoID) || (beforeID > 0 && d.ID >= beforeID) {
			continue
		}
		out = append(out, ChannelDanmaku{Danmaku: d, VideoTitle: "judul"})
	}
	slices.SortFunc(out, func(a, b ChannelDanmaku) int { return int(b.ID - a.ID) })
	return out[:min(limit, len(out))], nil
}

func (f *fakeRepo) Delete(_ context.Context, danmakuID int64, authorize func(Danmaku, int64) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.items, func(d Danmaku) bool { return d.ID == danmakuID })
	if i < 0 {
		return ErrNotFound
	}
	if err := authorize(f.items[i], f.owners[f.items[i].VideoID]); err != nil {
		return err
	}
	f.items = slices.Delete(f.items, i, i+1)
	return nil
}

func TestListChannelDanmaku(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newTestService()
	repo.videos[43] = 60_000
	repo.videos[50] = 60_000
	repo.owners = map[int64]int64{42: 7, 43: 7, 50: 8}
	// Pengirim berbeda supaya tidak terkena batas kecepatan.
	for i, videoID := range []int64{42, 43, 42, 50} {
		_, err := svc.Send(ctx, SendInput{RequesterID: int64(100 + i), VideoID: videoID, PositionMs: 1000, Body: "halo"})
		require.NoError(t, err)
	}

	page1, next, err := svc.ListChannelDanmaku(ctx, 7, 0, 2, "")
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, []int64{3, 2}, []int64{page1[0].ID, page1[1].ID}, "terbaru lebih dulu")
	require.NotEmpty(t, next)
	page2, next, err := svc.ListChannelDanmaku(ctx, 7, 0, 2, next)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, int64(1), page2[0].ID)
	assert.Empty(t, next)

	only, _, err := svc.ListChannelDanmaku(ctx, 7, 43, 0, "")
	require.NoError(t, err)
	require.Len(t, only, 1)
	assert.Equal(t, int64(43), only[0].VideoID)

	var verr *ValidationError
	_, _, err = svc.ListChannelDanmaku(ctx, 7, 0, 101, "")
	require.ErrorAs(t, err, &verr)
	_, _, err = svc.ListChannelDanmaku(ctx, 7, 0, 0, "rusak!")
	require.ErrorAs(t, err, &verr)
}

func TestDeleteDanmaku(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newTestService()
	repo.owners = map[int64]int64{42: 7}
	send := func(userID int64) int64 {
		d, err := svc.Send(ctx, SendInput{RequesterID: userID, VideoID: 42, PositionMs: 1000, Body: "halo"})
		require.NoError(t, err)
		return d.ID
	}
	a, b := send(99), send(98)

	assert.ErrorIs(t, svc.DeleteDanmaku(ctx, 98, a), ErrForbidden, "penonton lain")
	assert.NoError(t, svc.DeleteDanmaku(ctx, 99, a), "pengirimnya sendiri")
	assert.NoError(t, svc.DeleteDanmaku(ctx, 7, b), "pemilik video")
	assert.ErrorIs(t, svc.DeleteDanmaku(ctx, 7, b), ErrNotFound, "sudah dihapus")
	assert.ErrorIs(t, svc.DeleteDanmaku(ctx, 7, 0), ErrNotFound)
}
