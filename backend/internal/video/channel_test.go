package video

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ChannelSummary palsu meniru query Oracle: hanya video publik yang sudah
// terbit (IsListed = listedSQL) yang dihitung.
func (r *fakeRepo) ChannelSummary(_ context.Context, ownerID int64) (ChannelCounts, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out ChannelCounts
	for _, v := range r.videos {
		if v.OwnerID == ownerID && v.IsListed() {
			out.VideoCount++
			out.TotalViews += v.ViewCount
			out.TotalLikes += v.LikeCount
		}
	}
	return out, r.featured[ownerID], nil
}

func (r *fakeRepo) SetFeaturedVideo(_ context.Context, ownerID, videoID int64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.featured == nil {
		r.featured = map[int64]int64{}
	}
	r.featured[ownerID] = videoID
	return nil
}

func TestChannelSummaryCountsListedOnly(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Now()
	addListed(repo, 1, 7, "game", 100, now)
	addListed(repo, 2, 7, "game", 50, now)
	v := repo.videos[1]
	v.LikeCount = 4
	repo.videos[1] = v
	// Tidak dihitung: draf, pribadi, milik kanal lain.
	addListed(repo, 3, 7, "game", 1000, now)
	v = repo.videos[3]
	v.Draft = true
	repo.videos[3] = v
	addListed(repo, 4, 7, "game", 1000, now)
	v = repo.videos[4]
	v.Visibility = VisibilityPrivate
	repo.videos[4] = v
	addListed(repo, 5, 8, "game", 1000, now)

	sum, err := svc.GetChannelSummary(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, ChannelCounts{VideoCount: 2, TotalViews: 150, TotalLikes: 4}, sum.Totals)
	assert.Nil(t, sum.Featured)

	var verr *ValidationError
	_, err = svc.GetChannelSummary(ctx, 0)
	assert.ErrorAs(t, err, &verr)
}

func TestSetFeaturedVideo(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Now()
	addListed(repo, 1, 7, "game", 0, now)
	addListed(repo, 2, 8, "game", 0, now)
	addListed(repo, 3, 7, "game", 0, now)
	v := repo.videos[3]
	v.Visibility = VisibilityUnlisted
	repo.videos[3] = v

	got, err := svc.SetFeaturedVideo(ctx, 7, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.ID)
	sum, err := svc.GetChannelSummary(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, sum.Featured)
	assert.Equal(t, int64(1), sum.Featured.ID)

	// Video milik orang lain terlihat seperti tidak ada.
	_, err = svc.SetFeaturedVideo(ctx, 7, 2)
	assert.ErrorIs(t, err, ErrNotFound)
	// Video tidak publik tidak bisa jadi unggulan.
	var verr *ValidationError
	_, err = svc.SetFeaturedVideo(ctx, 7, 3)
	assert.ErrorAs(t, err, &verr)
	_, err = svc.SetFeaturedVideo(ctx, 7, -1)
	assert.ErrorAs(t, err, &verr)

	// Jika video unggulan menjadi pribadi, ia tidak ditampilkan, dan
	// kembali tampil saat publik lagi.
	v = repo.videos[1]
	v.Visibility = VisibilityPrivate
	repo.videos[1] = v
	sum, err = svc.GetChannelSummary(ctx, 7)
	require.NoError(t, err)
	assert.Nil(t, sum.Featured)
	v.Visibility = VisibilityPublic
	repo.videos[1] = v
	sum, err = svc.GetChannelSummary(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, sum.Featured)

	// 0 = hapus pilihan.
	got, err = svc.SetFeaturedVideo(ctx, 7, 0)
	require.NoError(t, err)
	assert.Nil(t, got)
	sum, err = svc.GetChannelSummary(ctx, 7)
	require.NoError(t, err)
	assert.Nil(t, sum.Featured)
}
