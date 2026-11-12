package video

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (r *fakeRepo) ListPopularPublic(_ context.Context, category string, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []Video
	for _, v := range r.videos {
		if v.IsListed() && (category == "" || v.Category == category) {
			all = append(all, v)
		}
	}
	slices.SortFunc(all, func(a, b Video) int {
		return slices.Compare([]int64{b.ViewCount, b.ID}, []int64{a.ViewCount, a.ID})
	})
	return all[:min(limit, len(all))], nil
}

func (r *fakeRepo) RecentWatchSignals(_ context.Context, userID int64, limit int) ([]WatchSignal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var entries []HistoryEntry
	for _, e := range r.history[userID] {
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b HistoryEntry) int { return b.WatchedAt.Compare(a.WatchedAt) })
	var out []WatchSignal
	for _, e := range entries[:min(limit, len(entries))] {
		v, ok := r.videos[e.Video.ID]
		if !ok {
			continue
		}
		out = append(out, WatchSignal{VideoID: v.ID, Category: v.Category, PositionMs: e.PositionMs, DurationMs: v.DurationMs})
	}
	return out, nil
}

// addListed menyimpan video publik di fake repo.
func addListed(repo *fakeRepo, id, owner int64, category string, views int64, published time.Time) {
	p := published
	repo.videos[id] = Video{
		ID: id, OwnerID: owner, Title: "v", Status: StatusReady, Visibility: VisibilityPublic,
		Category: category, ViewCount: views, DurationMs: 60_000, PublishedAt: &p, CreatedAt: p,
	}
}

func TestRecommendGuestPrefersPopularAndFresh(t *testing.T) {
	svc, repo, _ := newTestService()
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	addListed(repo, 1, 10, "game", 1000, now.Add(-time.Hour))     // populer dan baru
	addListed(repo, 2, 11, "game", 0, now.Add(-200*24*time.Hour)) // lama dan sepi
	addListed(repo, 3, 12, "musik", 50, now.Add(-2*24*time.Hour)) // sedang
	repo.videos[4] = Video{ID: 4, OwnerID: 13, Status: StatusReady, Visibility: VisibilityUnlisted, ViewCount: 9999}

	got, err := svc.Recommend(context.Background(), RecommendInput{Seed: 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3, 2}, ids(got), "video tidak publik tidak ikut")
}

func TestRecommendPersonal(t *testing.T) {
	svc, repo, _ := newTestService()
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	day := now.Add(-24 * time.Hour)
	addListed(repo, 1, 10, "game", 500, day)
	addListed(repo, 2, 11, "musik", 500, day)
	addListed(repo, 3, 12, "kuliner", 500, day)
	addListed(repo, 4, 7, "musik", 5000, day) // milik penonton sendiri
	addListed(repo, 5, 13, "musik", 500, day) // sudah ditonton sampai habis
	addListed(repo, 6, 14, "musik", 500, day) // baru ditonton sedikit

	// Penonton 7 sering menonton musik.
	_ = repo.SaveProgress(context.Background(), 7, 5, 59_000, now)
	_ = repo.SaveProgress(context.Background(), 7, 6, 5_000, now)

	got, err := svc.Recommend(context.Background(), RecommendInput{ViewerID: 7, FollowedOwnerIDs: []int64{12}, Seed: 3})
	require.NoError(t, err)
	assert.NotContains(t, ids(got), int64(4), "video sendiri")
	assert.NotContains(t, ids(got), int64(5), "sudah selesai ditonton")
	assert.Contains(t, ids(got), int64(6), "baru sebagian: tetap boleh")
	// Kanal yang diikuti dan kategori kesukaan di atas kategori lain.
	assert.Equal(t, int64(1), got[len(got)-1].ID)
}

func TestRecommendSeedAndDiversity(t *testing.T) {
	svc, repo, _ := newTestService()
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	for i := int64(1); i <= 30; i++ {
		// Kanal 10 punya 10 video; sisanya satu video per kanal.
		owner := i
		if i <= 10 {
			owner = 1000
		}
		addListed(repo, i, owner, "game", 100, now.Add(-24*time.Hour))
	}
	a, err := svc.Recommend(context.Background(), RecommendInput{Seed: 1})
	require.NoError(t, err)
	again, _ := svc.Recommend(context.Background(), RecommendInput{Seed: 1})
	other, _ := svc.Recommend(context.Background(), RecommendInput{Seed: 2})
	assert.Len(t, a, defaultRecommendLimit)
	assert.Equal(t, ids(a), ids(again), "seed sama = urutan sama")
	assert.NotEqual(t, ids(a), ids(other), "Muat ulang (seed lain) = susunan lain")

	perOwner := map[int64]int{}
	for _, v := range a {
		perOwner[v.OwnerID]++
	}
	assert.LessOrEqual(t, perOwner[1000], maxPerOwner)
}

func TestRecommendValidation(t *testing.T) {
	svc, _, _ := newTestService()
	var verr *ValidationError
	_, err := svc.Recommend(context.Background(), RecommendInput{Limit: 25})
	assert.ErrorAs(t, err, &verr)
	_, err = svc.Recommend(context.Background(), RecommendInput{Category: "bukan"})
	assert.ErrorAs(t, err, &verr)
}

func TestJitterRange(t *testing.T) {
	for seed := uint64(0); seed < 50; seed++ {
		j := jitter(seed, int64(seed*7))
		assert.True(t, j >= 0 && j < 1)
	}
}
