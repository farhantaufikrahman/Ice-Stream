package comment

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ListByChannel versi palsu (langkah 26), meniru query di
// oracle_repository.go.
func (r *fakeRepo) ListByChannel(_ context.Context, ownerID, videoID, beforeID int64, limit int) ([]ChannelComment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ChannelComment
	for id, c := range r.comments {
		v, ok := r.videos[c.VideoID]
		if !ok || v.ownerID != ownerID || r.deleted[id] || (videoID > 0 && c.VideoID != videoID) || (beforeID > 0 && id >= beforeID) {
			continue
		}
		out = append(out, ChannelComment{Comment: *c, VideoTitle: "judul"})
	}
	slices.SortFunc(out, func(a, b ChannelComment) int { return int(b.ID - a.ID) })
	return out[:min(limit, len(out))], nil
}

func TestListChannelComments(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestService()
	repo.videos[9] = &fakeVideo{ownerID: 200, ready: true}
	create := func(videoID, parentID int64) int64 {
		c, err := svc.CreateComment(ctx, 5, videoID, parentID, "halo")
		require.NoError(t, err)
		return c.ID
	}
	a := create(1, 0)
	b := create(2, 0)
	reply := create(1, a)
	create(9, 0) // channel lain
	gone := create(2, 0)
	require.NoError(t, svc.DeleteComment(ctx, 100, 2, gone))

	page1, next, err := svc.ListChannelComments(ctx, 100, 0, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{reply, b}, []int64{page1[0].ID, page1[1].ID}, "terbaru lebih dulu, balasan ikut, yang dihapus tidak")
	require.NotEmpty(t, next)
	page2, next, err := svc.ListChannelComments(ctx, 100, 0, 2, next)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, a, page2[0].ID)
	assert.Empty(t, next)

	only, _, err := svc.ListChannelComments(ctx, 100, 2, 0, "")
	require.NoError(t, err)
	require.Len(t, only, 1)
	assert.Equal(t, b, only[0].ID)

	// Video milik orang lain: kosong, bukan error.
	none, _, err := svc.ListChannelComments(ctx, 100, 9, 0, "")
	require.NoError(t, err)
	assert.Empty(t, none)
}
