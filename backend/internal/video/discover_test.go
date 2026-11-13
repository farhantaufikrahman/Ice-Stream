package video

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Versi palsu method langkah 24a di Repository. Perilakunya meniru query
// di oracle_repository.go.

func (r *fakeRepo) FindListedByIDs(_ context.Context, ids []int64) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Video
	for _, id := range ids {
		if v, ok := r.videos[id]; ok && v.IsListed() {
			out = append(out, v)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListPopularByOwner(_ context.Context, ownerID int64, offset, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []Video
	for _, v := range r.videos {
		if v.OwnerID == ownerID && v.IsListed() {
			all = append(all, v)
		}
	}
	slices.SortFunc(all, func(a, b Video) int {
		return slices.Compare([]int64{b.ViewCount, b.ID}, []int64{a.ViewCount, a.ID})
	})
	if offset >= len(all) {
		return nil, nil
	}
	all = all[offset:]
	return all[:min(limit, len(all))], nil
}

func (r *fakeRepo) TrendingVideos(_ context.Context, fromDay, category string, limit int) ([]TrendingHit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var hits []TrendingHit
	for id, days := range r.daily {
		v, ok := r.videos[id]
		if !ok || !v.IsListed() || (category != "" && v.Category != category) {
			continue
		}
		var sum int64
		for day, n := range days {
			// "2006-01-02" bisa dibandingkan sebagai teks.
			if day >= fromDay {
				sum += n
			}
		}
		if sum > 0 {
			hits = append(hits, TrendingHit{VideoID: id, Views: sum})
		}
	}
	slices.SortFunc(hits, func(a, b TrendingHit) int {
		return slices.Compare([]int64{b.Views, b.VideoID}, []int64{a.Views, a.VideoID})
	})
	return hits[:min(limit, len(hits))], nil
}

// setVideo mengubah satu video di repo palsu.
func setVideo(repo *fakeRepo, id int64, change func(*Video)) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	v := repo.videos[id]
	change(&v)
	repo.videos[id] = v
}

func TestSearchSortAndDuration(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Now()
	// Tiga video berjudul sama, jadi skornya sama.
	short := readyVideo(repo, 7, now.Add(-3*time.Hour))
	medium := readyVideo(repo, 7, now.Add(-2*time.Hour))
	long := readyVideo(repo, 7, now.Add(-time.Hour))
	set := func(id int64, durMin, views, danmaku int64) {
		setVideo(repo, id, func(v *Video) {
			v.DurationMs, v.ViewCount, v.DanmakuCount = durMin*60*1000, views, danmaku
		})
		repo.terms[id] = BuildSearchTerms("Belajar golang", "", nil)
	}
	set(short.ID, 9, 50, 1)
	set(medium.ID, 30, 10, 99) // 30:00 tepat masih "10-30 menit"
	set(long.ID, 31, 20, 5)

	ids := func(opts SearchOptions) []int64 {
		got, _, err := svc.SearchVideos(ctx, "golang", opts, 0, "")
		require.NoError(t, err)
		out := make([]int64, len(got))
		for i, v := range got {
			out[i] = v.ID
		}
		return out
	}
	assert.Equal(t, []int64{short.ID, long.ID, medium.ID}, ids(SearchOptions{}), "relevan: skor sama, lalu tayangan")
	assert.Equal(t, []int64{long.ID, medium.ID, short.ID}, ids(SearchOptions{Sort: SortNewest}))
	assert.Equal(t, []int64{short.ID, long.ID, medium.ID}, ids(SearchOptions{Sort: SortMostViewed}))
	assert.Equal(t, []int64{medium.ID, long.ID, short.ID}, ids(SearchOptions{Sort: SortMostDanmaku}))

	assert.Equal(t, []int64{short.ID}, ids(SearchOptions{Duration: DurationShort}))
	assert.Equal(t, []int64{medium.ID}, ids(SearchOptions{Duration: DurationMedium}))
	assert.Equal(t, []int64{long.ID}, ids(SearchOptions{Duration: DurationLong, Sort: SortNewest}))

	var verr *ValidationError
	_, _, err := svc.SearchVideos(ctx, "golang", SearchOptions{Sort: 9}, 0, "")
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "sort", verr.Field)
	_, _, err = svc.SearchVideos(ctx, "golang", SearchOptions{Duration: -1}, 0, "")
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "duration", verr.Field)
}

func TestDurationRange(t *testing.T) {
	for f, want := range map[DurationFilter][2]int64{
		DurationAny:    {0, 0},
		DurationShort:  {0, 600_000},
		DurationMedium: {600_000, 1_800_001},
		DurationLong:   {1_800_001, 0},
	} {
		minMs, maxMs := f.durationRange()
		assert.Equal(t, want, [2]int64{minMs, maxMs}, "filter %d", f)
	}
}

func TestListPopular(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	now := time.Now()
	var mine []Video
	for i, views := range []int64{5, 40, 40, 1} {
		v := readyVideo(repo, 7, now.Add(time.Duration(i)*time.Minute))
		setVideo(repo, v.ID, func(v *Video) { v.ViewCount = views })
		mine = append(mine, v)
	}
	other := readyVideo(repo, 8, now)
	setVideo(repo, other.ID, func(v *Video) { v.ViewCount = 1000 })

	page1, next, err := svc.ListPopular(ctx, 7, 3, "")
	require.NoError(t, err)
	require.Len(t, page1, 3)
	// Tayangan sama (40): id lebih besar lebih dulu.
	assert.Equal(t, []int64{mine[2].ID, mine[1].ID, mine[0].ID}, []int64{page1[0].ID, page1[1].ID, page1[2].ID})
	require.NotEmpty(t, next)
	page2, next, err := svc.ListPopular(ctx, 7, 3, next)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, mine[3].ID, page2[0].ID)
	assert.Empty(t, next)

	var verr *ValidationError
	_, _, err = svc.ListPopular(ctx, 0, 3, "")
	require.ErrorAs(t, err, &verr)
	// Token pencarian atau feed tidak berlaku di sini.
	for _, token := range []string{encodeSearchPage(1), "rusak", encodeOffsetPage("p", popularMaxPages)} {
		_, _, err = svc.ListPopular(ctx, 7, 3, token)
		require.ErrorAs(t, err, &verr, token)
	}
}

func TestListTrending(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	// 11 Nov 2026 pukul 10:00 WIB: 7 hari = 5 sampai 11 Nov.
	svc.now = func() time.Time { return time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC) }
	now := time.Now()
	a := readyVideo(repo, 7, now)
	b := readyVideo(repo, 8, now)
	c := readyVideo(repo, 9, now)
	old := readyVideo(repo, 9, now)
	gone := readyVideo(repo, 9, now)
	setVideo(repo, b.ID, func(v *Video) { v.Category = "game" })
	setVideo(repo, c.ID, func(v *Video) { v.Category = "game" })
	repo.daily = map[int64]map[string]int64{
		a.ID:    {"2026-11-05": 3, "2026-11-11": 2},   // 5
		b.ID:    {"2026-11-10": 9},                    // 9
		c.ID:    {"2026-11-04": 100, "2026-11-06": 1}, // 1: 4 Nov di luar 7 hari
		old.ID:  {"2026-10-01": 500},                  // tidak ada tayangan 7 hari terakhir
		gone.ID: {"2026-11-11": 50},
	}
	// Video yang sudah dihapus tidak ikut.
	repo.trash[gone.ID] = repo.videos[gone.ID]
	delete(repo.videos, gone.ID)

	got, err := svc.ListTrending(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []int64{b.ID, a.ID, c.ID}, []int64{got[0].Video.ID, got[1].Video.ID, got[2].Video.ID})
	assert.Equal(t, []int64{9, 5, 1}, []int64{got[0].RecentViews, got[1].RecentViews, got[2].RecentViews})

	got, err = svc.ListTrending(ctx, "Game", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, b.ID, got[0].Video.ID)

	var verr *ValidationError
	_, err = svc.ListTrending(ctx, "", 51)
	require.ErrorAs(t, err, &verr)
	_, err = svc.ListTrending(ctx, "film", 5)
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "category", verr.Field)
}
