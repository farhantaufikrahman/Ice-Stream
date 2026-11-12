package video

import (
	"cmp"
	"context"
	"encoding/binary"
	"hash/fnv"
	"math"
	"slices"
	"time"
)

// Langkah 27: "Rekomendasi untukmu" di Beranda (desain Home.dc.html).
//
// Cara kerjanya dua tahap, sama seperti sistem rekomendasi besar (YouTube
// menyebutnya "candidate generation" lalu "ranking"), tetapi jauh lebih
// sederhana dan bisa dijelaskan ke kreator:
//
//  1. Kandidat: video publik terbaru dan video publik paling banyak
//     ditonton (masing-masing recommendCandidates video).
//  2. Skor setiap kandidat dari empat sinyal yang jelas:
//     - populer: tayangan sepanjang masa (skala log, jadi video sejuta
//     tayangan tidak menenggelamkan semua video lain);
//     - baru: makin baru makin tinggi, separuhnya hilang setelah 14 hari;
//     - kategori: seberapa sering penonton menonton kategori itu
//     (dari 100 riwayat tonton terakhir);
//     - mengikuti: video dari kanal yang penonton ikuti.
//     Ditambah sedikit acak (seed) supaya tombol "Muat ulang" memberi
//     susunan lain, dan maksimal 2 video per kanal supaya beragam.
//
// Video yang sudah ditonton sampai hampir habis (90%) dan video milik
// penonton sendiri tidak direkomendasikan.

const (
	// recommendCandidates: jumlah video terbaru dan jumlah video terpopuler
	// yang diambil sebagai kandidat.
	recommendCandidates = 200
	// recommendHistory: riwayat tonton terakhir yang dipakai untuk menebak
	// kategori kesukaan.
	recommendHistory = 100
	// Beranda menampilkan 12 (desain), maksimal 24.
	defaultRecommendLimit = 12
	maxRecommendLimit     = 24
	// maxPerOwner: paling banyak 2 video dari kanal yang sama.
	maxPerOwner = 2
	// finishedRatio: dianggap sudah selesai ditonton jika posisinya sudah
	// 90% dari durasi.
	finishedRatio = 0.9
	// freshHalfLife: skor "baru" tinggal separuh setelah 14 hari.
	freshHalfLife = 14 * 24 * time.Hour
)

// Bobot setiap sinyal. Kategori dan kanal yang diikuti sedikit lebih kuat
// dari populer dan baru, karena itu yang paling pribadi.
const (
	weightPopular  = 1.0
	weightFresh    = 1.0
	weightCategory = 1.5
	weightFollow   = 1.5
	weightRandom   = 0.8
)

// RecommendInput adalah permintaan rekomendasi.
type RecommendInput struct {
	// ViewerID 0 = tamu (hanya populer, baru, dan acak).
	ViewerID int64
	// FollowedOwnerIDs adalah kanal yang diikuti penonton. Diisi gateway
	// dari UserService, karena data langganan ada di schema AUTH_SVC.
	FollowedOwnerIDs []int64
	// Category kosong = semua kategori.
	Category string
	// Seed mengubah bagian acak. Seed yang sama = urutan yang sama, jadi
	// halaman yang dimuat ulang browser tidak berubah-ubah sendiri.
	Seed  uint64
	Limit int
}

// WatchSignal adalah satu baris riwayat tonton beserta data videonya,
// bahan untuk menebak selera penonton.
type WatchSignal struct {
	VideoID    int64
	Category   string
	PositionMs int64
	DurationMs int64
}

// finished mengembalikan true jika video sudah ditonton hampir habis.
func (w WatchSignal) finished() bool {
	return w.DurationMs > 0 && float64(w.PositionMs) >= finishedRatio*float64(w.DurationMs)
}

// Recommend menyusun "Rekomendasi untukmu".
func (s *Service) Recommend(ctx context.Context, in RecommendInput) ([]Video, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultRecommendLimit
	}
	if limit > maxRecommendLimit {
		return nil, &ValidationError{"limit", "maksimal 24"}
	}
	category, err := cleanCategory(in.Category)
	if err != nil {
		return nil, err
	}

	recent, err := s.repo.ListPublic(ctx, FeedFilter{Category: category}, nil, recommendCandidates)
	if err != nil {
		return nil, err
	}
	popular, err := s.repo.ListPopularPublic(ctx, category, recommendCandidates)
	if err != nil {
		return nil, err
	}

	var signals []WatchSignal
	if in.ViewerID > 0 {
		signals, err = s.repo.RecentWatchSignals(ctx, in.ViewerID, recommendHistory)
		if err != nil {
			return nil, err
		}
	}
	return rankRecommendations(append(recent, popular...), in, signals, s.now(), limit), nil
}

// scored adalah kandidat beserta skornya.
type scored struct {
	video Video
	score float64
}

// rankRecommendations memberi skor, mengurutkan, lalu memilih video. Fungsi
// murni (tanpa database) supaya mudah diuji.
func rankRecommendations(candidates []Video, in RecommendInput, signals []WatchSignal, now time.Time, limit int) []Video {
	finished := make(map[int64]bool)
	catCount := make(map[string]int)
	for _, w := range signals {
		if w.finished() {
			finished[w.VideoID] = true
		}
		if w.Category != "" {
			catCount[w.Category]++
		}
	}
	followed := make(map[int64]bool, len(in.FollowedOwnerIDs))
	for _, id := range in.FollowedOwnerIDs {
		followed[id] = true
	}

	// Kandidat terbaru dan terpopuler bisa berisi video yang sama.
	seen := make(map[int64]bool, len(candidates))
	var maxViews int64
	pool := make([]Video, 0, len(candidates))
	for _, v := range candidates {
		if seen[v.ID] || finished[v.ID] || (in.ViewerID > 0 && v.OwnerID == in.ViewerID) {
			continue
		}
		seen[v.ID] = true
		pool = append(pool, v)
		maxViews = max(maxViews, v.ViewCount)
	}

	list := make([]scored, len(pool))
	for i, v := range pool {
		score := weightPopular*popularity(v.ViewCount, maxViews) +
			weightFresh*freshness(v, now) +
			weightRandom*jitter(in.Seed, v.ID)
		if len(signals) > 0 {
			score += weightCategory * float64(catCount[v.Category]) / float64(len(signals))
		}
		if followed[v.OwnerID] {
			score += weightFollow
		}
		list[i] = scored{video: v, score: score}
	}
	slices.SortFunc(list, func(a, b scored) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		// Skor sama: id terbesar (lebih baru) dulu, supaya urutan selalu tetap.
		return cmp.Compare(b.video.ID, a.video.ID)
	})

	out := make([]Video, 0, limit)
	perOwner := make(map[int64]int)
	for _, c := range list {
		if len(out) == limit {
			break
		}
		if perOwner[c.video.OwnerID] >= maxPerOwner {
			continue
		}
		perOwner[c.video.OwnerID]++
		out = append(out, c.video)
	}
	return out
}

// popularity: 0 (tanpa tayangan) sampai 1 (kandidat paling banyak ditonton),
// skala log.
func popularity(views, maxViews int64) float64 {
	if maxViews <= 0 {
		return 0
	}
	return math.Log1p(float64(views)) / math.Log1p(float64(maxViews))
}

// freshness: 1 untuk video yang baru terbit, 0,5 setelah 14 hari, 0,25
// setelah 28 hari, dan seterusnya.
func freshness(v Video, now time.Time) float64 {
	published := v.CreatedAt
	if v.PublishedAt != nil {
		published = *v.PublishedAt
	}
	age := now.Sub(published)
	if age <= 0 {
		return 1
	}
	return math.Exp2(-float64(age) / float64(freshHalfLife))
}

// jitter: angka 0 sampai 1 yang tetap untuk pasangan (seed, videoID) yang
// sama, dari hash FNV-1a. Tidak perlu acak yang aman untuk kriptografi,
// cukup tersebar rata.
func jitter(seed uint64, videoID int64) float64 {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], seed)
	binary.LittleEndian.PutUint64(buf[8:], uint64(videoID))
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	return float64(h.Sum64()>>11) / float64(1<<53)
}
