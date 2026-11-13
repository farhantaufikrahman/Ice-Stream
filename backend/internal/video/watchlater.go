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

// Tonton nanti: daftar video yang disimpan user untuk ditonton kemudian.
// Bentuknya mirip riwayat tonton (history.go), tapi diisi user sendiri
// lewat tombol Simpan, bukan otomatis oleh player.

// WatchLaterEntry adalah satu video di daftar Tonton nanti.
type WatchLaterEntry struct {
	Video Video
	// AddedAt adalah waktu video disimpan.
	AddedAt time.Time
}

// WatchLaterCursor menandai entri terakhir di halaman sebelumnya. Daftar
// diurutkan added_at lalu video_id dari besar ke kecil.
type WatchLaterCursor struct {
	AddedAt time.Time
	VideoID int64
}

// AddToWatchLater menyimpan videoID ke Tonton nanti milik requesterID dan
// mengembalikan waktu video itu disimpan. Hanya untuk video READY.
//
// Aman dipanggil berulang (idempotent): jika video sudah tersimpan,
// barisnya tidak diubah dan waktu yang dikembalikan tetap waktu pertama
// kali disimpan. Jadi posisinya di daftar tidak melompat ke atas hanya
// karena tombolnya ditekan dua kali.
func (s *Service) AddToWatchLater(ctx context.Context, requesterID, videoID int64) (time.Time, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return time.Time{}, errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return time.Time{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return time.Time{}, err
	}
	if !v.CanView(requesterID) {
		return time.Time{}, ErrNotFound
	}
	// Dibulatkan ke mikrodetik, ketelitian TIMESTAMP bawaan Oracle, supaya
	// nilai di cursor halaman sama persis dengan isi database.
	now := s.now().Truncate(time.Microsecond)
	return s.repo.AddWatchLater(ctx, requesterID, videoID, now)
}

// RemoveFromWatchLater mengeluarkan videoID dari Tonton nanti milik
// requesterID. Aman dipanggil walau video itu tidak tersimpan.
func (s *Service) RemoveFromWatchLater(ctx context.Context, requesterID, videoID int64) error {
	if requesterID <= 0 {
		return errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return nil
	}
	return s.repo.RemoveWatchLater(ctx, requesterID, videoID)
}

// ListWatchLater mengambil Tonton nanti milik requesterID, yang terakhir
// disimpan lebih dulu. Video yang sudah tidak READY (misalnya dihapus
// pemiliknya) tidak ikut ditampilkan.
func (s *Service) ListWatchLater(ctx context.Context, requesterID int64, pageSize int, pageToken string) ([]WatchLaterEntry, string, error) {
	if requesterID <= 0 {
		return nil, "", errors.New("requester id kosong")
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	var after *WatchLaterCursor
	if pageToken != "" {
		c, err := decodeWatchLaterCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		after = &c
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	entries, err := s.repo.ListWatchLater(ctx, requesterID, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(entries) > pageSize {
		entries = entries[:pageSize]
		last := entries[pageSize-1]
		next = encodeWatchLaterCursor(WatchLaterCursor{AddedAt: last.AddedAt, VideoID: last.Video.ID})
	}
	return entries, next, nil
}

// encodeWatchLaterCursor mengubah cursor menjadi base64 dari
// "w<added_at dalam mikrodetik>.<video_id>". Huruf "w" membedakannya dari
// token riwayat ("h") dan token feed.
func encodeWatchLaterCursor(c WatchLaterCursor) string {
	raw := "w" + strconv.FormatInt(c.AddedAt.UnixMicro(), 10) + "." + strconv.FormatInt(c.VideoID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeWatchLaterCursor(token string) (WatchLaterCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return WatchLaterCursor{}, err
	}
	body, ok := strings.CutPrefix(string(raw), "w")
	if !ok {
		return WatchLaterCursor{}, errors.New("bukan token tonton nanti")
	}
	micros, id, ok := strings.Cut(body, ".")
	if !ok {
		return WatchLaterCursor{}, errors.New("format cursor salah")
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil || us <= 0 {
		return WatchLaterCursor{}, errors.New("waktu cursor salah")
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return WatchLaterCursor{}, errors.New("id cursor salah")
	}
	return WatchLaterCursor{AddedAt: time.UnixMicro(us).UTC(), VideoID: n}, nil
}
