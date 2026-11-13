package video

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Langkah 24a: cara lain menemukan video.
//   - Urutan dan filter durasi di pencarian (desain Search: "Urutkan" dan
//     "Durasi").
//   - Tab "Terpopuler" di halaman channel (desain Channel).
//   - Trending di beranda (desain Home): tayangan 7 hari terakhir.

// SearchSort adalah pilihan "Urutkan" di halaman pencarian.
type SearchSort int

const (
	// SortRelevance: skor kecocokan kata (judul dan tag lebih tinggi dari
	// deskripsi), lalu tayangan. Bawaan.
	SortRelevance SearchSort = iota
	// SortNewest: yang paling baru tayang lebih dulu.
	SortNewest
	// SortMostViewed: tayangan sepanjang masa terbanyak lebih dulu.
	SortMostViewed
	// SortMostDanmaku: jumlah danmaku terbanyak lebih dulu.
	SortMostDanmaku
)

// DurationFilter adalah pilihan "Durasi" di halaman pencarian.
type DurationFilter int

const (
	DurationAny DurationFilter = iota
	// DurationShort: kurang dari 10 menit.
	DurationShort
	// DurationMedium: 10 sampai 30 menit.
	DurationMedium
	// DurationLong: lebih dari 30 menit.
	DurationLong
)

// Batas durasi dalam milidetik, sama dengan label di desain.
const (
	shortMaxMs  = 10 * 60 * 1000
	mediumMaxMs = 30 * 60 * 1000
)

// durationRange mengubah filter menjadi batas [min, max) dalam milidetik.
// max 0 berarti tanpa batas atas.
func (d DurationFilter) durationRange() (minMs, maxMs int64) {
	switch d {
	case DurationShort:
		return 0, shortMaxMs
	case DurationMedium:
		return shortMaxMs, mediumMaxMs + 1 // "10-30 menit": 30:00 tepat masih termasuk
	case DurationLong:
		return mediumMaxMs + 1, 0
	default:
		return 0, 0
	}
}

// SearchOptions adalah urutan dan filter pencarian video.
type SearchOptions struct {
	Sort     SearchSort
	Duration DurationFilter
}

func (o SearchOptions) validate() error {
	if o.Sort < SortRelevance || o.Sort > SortMostDanmaku {
		return &ValidationError{"sort", "tidak dikenal"}
	}
	if o.Duration < DurationAny || o.Duration > DurationLong {
		return &ValidationError{"duration", "tidak dikenal"}
	}
	return nil
}

// ---------- Terpopuler di halaman channel ----------

// popularMaxPages membatasi halaman tab Terpopuler. Halamannya memakai
// offset (lihat ListPopular), jadi dibatasi seperti pencarian, tapi lebih
// longgar: 50 x 20 = 1000 video per channel.
const popularMaxPages = 50

// ListPopular mengambil video READY milik ownerID, tayangan terbanyak lebih
// dulu (tab "Terpopuler" di halaman channel).
//
// Halaman memakai offset, bukan cursor seperti feed: tayangan berubah
// setiap saat, jadi cursor (view_count, id) dari halaman sebelumnya bisa
// sudah tidak berlaku. Offset paling buruk membuat satu video tampil di dua
// halaman, tanpa ada yang hilang secara permanen.
func (s *Service) ListPopular(ctx context.Context, ownerID int64, pageSize int, pageToken string) ([]Video, string, error) {
	if ownerID <= 0 {
		return nil, "", &ValidationError{"owner_id", "wajib diisi untuk urutan terpopuler"}
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	page := 0
	if pageToken != "" {
		p, err := decodeOffsetPage(pageToken, "p", popularMaxPages)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		page = p
	}
	videos, err := s.repo.ListPopularByOwner(ctx, ownerID, page*pageSize, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	if len(videos) <= pageSize || page+1 >= popularMaxPages {
		if len(videos) > pageSize {
			videos = videos[:pageSize]
		}
		return videos, "", nil
	}
	return videos[:pageSize], encodeOffsetPage("p", page+1), nil
}

// encodeOffsetPage membuat token halaman seperti base64("p2"). Huruf di
// depan membedakan jenis daftar, jadi token tab Terpopuler tidak bisa
// dipakai di feed biasa atau pencarian.
func encodeOffsetPage(prefix string, page int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(prefix + strconv.Itoa(page)))
}

func decodeOffsetPage(token, prefix string, maxPages int) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	numStr, ok := strings.CutPrefix(string(raw), prefix)
	if !ok {
		return 0, errors.New("jenis token salah")
	}
	page, err := strconv.Atoi(numStr)
	if err != nil || page < 1 || page >= maxPages {
		return 0, errors.New("nomor halaman salah")
	}
	return page, nil
}

// ---------- Trending ----------

const (
	// trendingDays: Trending = tayangan 7 hari terakhir, termasuk hari ini
	// (tanggal WIB, sama dengan dasbor Studio).
	trendingDays = 7
	// Beranda menampilkan 5, halaman Trending sampai 50.
	defaultTrendingLimit = 10
	maxTrendingLimit     = 50
)

// TrendingVideo adalah satu video Trending.
type TrendingVideo struct {
	Video Video
	// RecentViews adalah tayangan 7 hari terakhir, angka yang dipakai
	// untuk mengurutkan.
	RecentViews int64
}

// TrendingHit adalah hasil query Trending sebelum data video diambil.
type TrendingHit struct {
	VideoID int64
	Views   int64
}

// ListTrending mengambil video READY dengan tayangan terbanyak dalam 7 hari
// terakhir. category kosong = semua kategori.
//
// YouTube dan Bilibili memakai banyak sinyal (kecepatan naik, like,
// komentar) dan tim moderasi. Ice-Stream memakai satu sinyal yang jelas
// dan bisa dicek kreator di dasbornya sendiri: tayangan 7 hari.
func (s *Service) ListTrending(ctx context.Context, category string, limit int) ([]TrendingVideo, error) {
	if limit <= 0 {
		limit = defaultTrendingLimit
	}
	if limit > maxTrendingLimit {
		return nil, &ValidationError{"limit", fmt.Sprintf("maksimal %d", maxTrendingLimit)}
	}
	category, err := cleanCategory(category)
	if err != nil {
		return nil, err
	}
	// Hari ini ikut dihitung: 7 tanggal = hari ini dan 6 hari sebelumnya.
	from := statsDay(s.now().Add(-(trendingDays - 1) * 24 * time.Hour))
	hits, err := s.repo.TrendingVideos(ctx, from, category, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.VideoID
	}
	videos, err := s.repo.FindListedByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Video, len(videos))
	for _, v := range videos {
		byID[v.ID] = v
	}
	out := make([]TrendingVideo, 0, len(hits))
	for _, h := range hits {
		// Video yang dihapus di antara dua query dilewati saja.
		if v, ok := byID[h.VideoID]; ok {
			out = append(out, TrendingVideo{Video: v, RecentViews: h.Views})
		}
	}
	return out, nil
}
