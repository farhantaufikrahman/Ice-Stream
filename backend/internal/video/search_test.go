package video

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Search versi memori: aturan yang sama dengan query SQL di
// OracleRepository.Search (semua kata harus cocok di awal kata, skor =
// jumlah bobot, lalu views, lalu id).
func (r *fakeRepo) Search(_ context.Context, terms []string, opts SearchOptions, offset, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	type hit struct {
		v     Video
		score int
	}
	var hits []hit
	for id, vt := range r.terms {
		v := r.videos[id]
		if v.Status != StatusReady {
			continue
		}
		if minMs, maxMs := opts.Duration.durationRange(); v.DurationMs < minMs || (maxMs > 0 && v.DurationMs >= maxMs) {
			continue
		}
		score, all := 0, true
		for _, q := range terms {
			best := 0
			for _, t := range vt {
				if strings.HasPrefix(t.Term, q) && t.Weight > best {
					best = t.Weight
				}
			}
			if best == 0 {
				all = false
				break
			}
			score += best
		}
		if all {
			hits = append(hits, hit{v, score})
		}
	}
	// Sama dengan searchOrder di oracle_repository.go: setiap urutan
	// adalah daftar nilai yang dibandingkan dari kiri, id paling akhir.
	key := func(h hit) []int64 {
		switch opts.Sort {
		case SortNewest:
			return []int64{h.v.ProcessedAt.UnixMicro(), h.v.ID}
		case SortMostViewed:
			return []int64{h.v.ViewCount, h.v.ID}
		case SortMostDanmaku:
			return []int64{h.v.DanmakuCount, h.v.ViewCount, h.v.ID}
		default:
			return []int64{int64(h.score), h.v.ViewCount, h.v.ID}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		return slices.Compare(key(hits[i]), key(hits[j])) > 0
	})
	var out []Video
	for i := offset; i < len(hits) && len(out) < limit; i++ {
		out = append(out, hits[i].v)
	}
	return out, nil
}

func (r *fakeRepo) ListUnindexed(_ context.Context, limit int) ([]Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Video
	for id := int64(1); id <= r.nextID && len(out) < limit; id++ {
		if v, ok := r.videos[id]; ok {
			if _, indexed := r.terms[id]; !indexed {
				out = append(out, v)
			}
		}
	}
	return out, nil
}

func (r *fakeRepo) SaveSearchTerms(_ context.Context, videoID int64, terms []SearchTerm, _ time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, indexed := r.terms[videoID]; indexed {
		return false, nil
	}
	r.terms[videoID] = terms
	return true, nil
}

func TestTokenize(t *testing.T) {
	assert.Equal(t, []string{"belajar", "golang", "dari", "nol", "2026"},
		tokenize("Belajar GOLANG dari-nol (2026)!"))
	assert.Equal(t, []string{"cafe", "creme", "brulee"}, tokenize("Café Crème Brûlée"), "aksen dibuang")
	assert.Equal(t, []string{"ok"}, tokenize("a ok b ?"), "kata 1 huruf dibuang")
	assert.Equal(t, []string{strings.Repeat("x", 30)}, tokenize(strings.Repeat("x", 40)), "dipotong 30 huruf")
	assert.Equal(t, []string{"東京タワー"}, tokenize("東京タワー"), "tanpa spasi = satu kata")
	assert.Empty(t, tokenize("  !!! "))
}

func TestBuildSearchTerms(t *testing.T) {
	terms := BuildSearchTerms("Belajar Golang", "Tutorial golang untuk pemula, belajar dari nol", nil)
	assert.Equal(t, []SearchTerm{
		{"belajar", WeightTitle}, {"golang", WeightTitle},
		{"tutorial", WeightDescription}, {"untuk", WeightDescription},
		{"pemula", WeightDescription}, {"dari", WeightDescription}, {"nol", WeightDescription},
	}, terms, "kata di judul dan deskripsi tetap berbobot judul, tanpa dobel")

	// 400 kata berbeda: kata1, kata2, ... hanya 300 yang disimpan.
	var long strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&long, "kata%d ", i)
	}
	assert.Len(t, BuildSearchTerms("judul", long.String(), nil), maxTermsPerVideo)
}

func TestParseSearchQuery(t *testing.T) {
	terms, err := ParseSearchQuery("  Golang golang  PEMULA ")
	require.NoError(t, err)
	assert.Equal(t, []string{"golang", "pemula"}, terms)

	terms, err = ParseSearchQuery("satu dua tiga empat lima enam tujuh")
	require.NoError(t, err)
	assert.Len(t, terms, maxQueryTerms)

	var verr *ValidationError
	for _, q := range []string{"", "   ", "a b c", "!!!", strings.Repeat("a", 101), "\xff\xfe"} {
		_, err := ParseSearchQuery(q)
		assert.ErrorAs(t, err, &verr, "query %q", q)
	}
}

// readyWith membuat video READY dengan judul dan deskripsi tertentu lewat
// CreateUpload, supaya kata pencariannya dibuat oleh Service sendiri.
func readyWith(t *testing.T, svc *Service, repo *fakeRepo, title, desc string, views int64) Video {
	t.Helper()
	in := validInput()
	in.Title, in.Description = title, desc
	up, err := svc.CreateUpload(context.Background(), in)
	require.NoError(t, err)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	v := repo.videos[up.Video.ID]
	v.Status, v.ViewCount = StatusReady, views
	repo.videos[v.ID] = v
	return v
}

func TestSearchVideos(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	a := readyWith(t, svc, repo, "Belajar Golang dari Nol", "", 5)
	b := readyWith(t, svc, repo, "Masak nasi goreng", "resep golang? bukan, resep nasi", 100)
	c := readyWith(t, svc, repo, "Golang lanjutan", "goroutine dan channel", 1)
	// Video yang belum READY tidak boleh muncul.
	pending, err := svc.CreateUpload(ctx, CreateUploadInput{OwnerID: 7, Title: "Golang rahasia", ContentType: "video/mp4", SizeBytes: 1})
	require.NoError(t, err)

	got, next, err := svc.SearchVideos(ctx, "golang", SearchOptions{}, 0, "")
	require.NoError(t, err)
	// a dan c cocok di judul (skor 2), b hanya di deskripsi (skor 1). Skor
	// sama diurutkan views terbanyak.
	assert.Equal(t, []int64{a.ID, c.ID, b.ID}, ids(got))
	assert.NotContains(t, ids(got), pending.Video.ID)
	assert.Empty(t, next)

	got, _, err = svc.SearchVideos(ctx, "Go", SearchOptions{}, 0, "")
	require.NoError(t, err)
	assert.Len(t, got, 3, "awalan kata: go cocok dengan golang, goreng, goroutine")

	got, _, err = svc.SearchVideos(ctx, "golang nol", SearchOptions{}, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID}, ids(got), "semua kata harus cocok")

	got, _, err = svc.SearchVideos(ctx, "tidakada", SearchOptions{}, 0, "")
	require.NoError(t, err)
	assert.Empty(t, got)

	// Halaman berikutnya.
	page1, next, err := svc.SearchVideos(ctx, "golang", SearchOptions{}, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID, c.ID}, ids(page1))
	require.NotEmpty(t, next)
	page2, next, err := svc.SearchVideos(ctx, "golang", SearchOptions{}, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID}, ids(page2))
	assert.Empty(t, next)
}

func TestSearchVideosPageLimit(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	for i := 0; i < 12; i++ {
		readyWith(t, svc, repo, "Golang part", "", int64(i))
	}
	token, pages := "", 0
	for {
		got, next, err := svc.SearchVideos(ctx, "golang", SearchOptions{}, 1, token)
		require.NoError(t, err)
		require.Len(t, got, 1)
		pages++
		if next == "" {
			break
		}
		token = next
	}
	assert.Equal(t, searchMaxPages, pages, "berhenti di halaman 10 walau hasilnya masih ada")
}

func TestSearchVideosValidation(t *testing.T) {
	svc, _, _ := newTestService()
	var verr *ValidationError
	_, _, err := svc.SearchVideos(context.Background(), "", SearchOptions{}, 0, "")
	assert.ErrorAs(t, err, &verr)
	_, _, err = svc.SearchVideos(context.Background(), "golang", SearchOptions{}, 51, "")
	assert.ErrorAs(t, err, &verr)
	for _, token := range []string{"!!!", encodeRaw("1"), encodeRaw("s0"), encodeRaw("s10"), encodeRaw("sx"), encodeFeedCursor(FeedCursor{ProcessedAt: time.Now(), ID: 1})} {
		_, _, err = svc.SearchVideos(context.Background(), "golang", SearchOptions{}, 0, token)
		assert.ErrorAs(t, err, &verr, "token %q", token)
	}
}

func TestIndexPending(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	// 250 video lama tanpa kata pencarian (dibuat sebelum fitur ini ada).
	for i := 0; i < 250; i++ {
		v := readyVideo(repo, 7, time.Now())
		v.Title = "Video lama golang"
		repo.videos[v.ID] = v
	}
	n, err := svc.IndexPending(ctx)
	require.NoError(t, err)
	assert.Equal(t, 250, n)

	got, _, err := svc.SearchVideos(ctx, "lama", SearchOptions{}, 50, "")
	require.NoError(t, err)
	assert.Len(t, got, 50)

	n, err = svc.IndexPending(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "dijalankan ulang: tidak ada yang perlu diindeks")
}
