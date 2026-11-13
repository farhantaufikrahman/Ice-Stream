package video

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLikeVideo(t *testing.T) {
	ctx := context.Background()
	svc, repo, store := newTestService()
	v := readyVideo(repo, 7, time.Now())

	r, err := svc.LikeVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, LikeResult{Liked: true, LikeCount: 1}, r)

	// Like kedua dari user yang sama tidak menambah jumlah (idempotent).
	r, err = svc.LikeVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), r.LikeCount)

	r, err = svc.LikeVideo(ctx, 9, v.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), r.LikeCount)

	// GetVideo memberi tahu apakah viewer sudah like.
	d, err := svc.GetVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.True(t, d.ViewerLiked)
	assert.Equal(t, int64(2), d.Video.LikeCount)
	d, err = svc.GetVideo(ctx, 10, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerLiked)
	d, err = svc.GetVideo(ctx, 0, v.ID)
	require.NoError(t, err)
	assert.False(t, d.ViewerLiked, "tanpa login")

	r, err = svc.UnlikeVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, LikeResult{Liked: false, LikeCount: 1}, r)
	r, err = svc.UnlikeVideo(ctx, 8, v.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), r.LikeCount, "unlike kedua tidak mengurangi lagi")

	t.Run("video belum READY", func(t *testing.T) {
		up := uploadedVideo(t, svc, store)
		_, err := svc.LikeVideo(ctx, 8, up.ID)
		assert.ErrorIs(t, err, ErrNotFound)
		_, err = svc.UnlikeVideo(ctx, 8, up.ID)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("video tidak ada", func(t *testing.T) {
		_, err := svc.LikeVideo(ctx, 8, 999)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("requester kosong adalah bug gateway", func(t *testing.T) {
		_, err := svc.LikeVideo(ctx, 0, v.ID)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrNotFound)
	})
}
