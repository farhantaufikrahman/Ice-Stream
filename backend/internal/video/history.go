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

// Riwayat tonton: setiap user yang login punya daftar video yang pernah
// ia tonton, beserta detik terakhir yang ditonton. Player mengirim posisi
// secara berkala (SaveWatchProgress); saat video dibuka lagi, GetVideo
// memberi tahu dari detik berapa video sebaiknya dilanjutkan.

const (
	// Posisi di bawah 5 detik tidak perlu dilanjutkan: lebih nyaman mulai
	// dari awal saja.
	resumeMinMs = 5_000
	// Jika sisa video kurang dari 10 detik, video dianggap sudah selesai
	// ditonton, jadi dibuka lagi dari awal (seperti YouTube).
	resumeEndMarginMs = 10_000
)

// HistoryEntry adalah satu video di riwayat tonton.
type HistoryEntry struct {
	Video Video
	// PositionMs adalah detik terakhir yang ditonton, dalam milidetik.
	PositionMs int64
	// WatchedAt adalah waktu posisi itu terakhir dikirim.
	WatchedAt time.Time
}

// HistoryCursor menandai entri terakhir di halaman sebelumnya. Riwayat
// diurutkan watched_at lalu video_id dari besar ke kecil (sama seperti
// FeedCursor, tapi kolomnya berbeda).
type HistoryCursor struct {
	WatchedAt time.Time
	VideoID   int64
}

// WatchProgress adalah hasil SaveWatchProgress.
type WatchProgress struct {
	// PositionMs adalah posisi yang disimpan (bisa lebih kecil dari yang
	// dikirim jika melewati durasi video).
	PositionMs int64
	WatchedAt  time.Time
}

// SaveWatchProgress menyimpan sampai detik berapa requesterID menonton
// videoID. Dipanggil player setiap beberapa detik dan saat video dijeda
// atau tab ditutup. Hanya untuk video READY.
//
// Aman dipanggil berulang (idempotent): menonton video yang sama lagi
// hanya memperbarui posisi dan waktunya, tidak menambah entri baru.
func (s *Service) SaveWatchProgress(ctx context.Context, requesterID, videoID, positionMs int64) (WatchProgress, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return WatchProgress{}, errors.New("requester id kosong")
	}
	if positionMs < 0 {
		return WatchProgress{}, &ValidationError{"position_ms", "tidak boleh negatif"}
	}
	if videoID <= 0 {
		return WatchProgress{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return WatchProgress{}, err
	}
	if !v.CanView(requesterID) {
		return WatchProgress{}, ErrNotFound
	}
	// Player kadang melaporkan posisi sedikit melewati durasi (pembulatan
	// di akhir video). Daripada ditolak, posisinya dipotong ke durasi.
	if v.DurationMs > 0 && positionMs > v.DurationMs {
		positionMs = v.DurationMs
	}
	// Dibulatkan ke mikrodetik, ketelitian TIMESTAMP bawaan Oracle. Dengan
	// begitu nilai di cursor halaman riwayat sama persis dengan isi database.
	now := s.now().Truncate(time.Microsecond)
	if err := s.repo.SaveProgress(ctx, requesterID, videoID, positionMs, now); err != nil {
		return WatchProgress{}, err
	}
	return WatchProgress{PositionMs: positionMs, WatchedAt: now}, nil
}

// ListWatchHistory mengambil riwayat tonton requesterID, yang terakhir
// ditonton lebih dulu. Video yang sudah tidak READY (misalnya dihapus
// nanti) tidak ikut ditampilkan.
func (s *Service) ListWatchHistory(ctx context.Context, requesterID int64, pageSize int, pageToken string) ([]HistoryEntry, string, error) {
	if requesterID <= 0 {
		return nil, "", errors.New("requester id kosong")
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	var after *HistoryCursor
	if pageToken != "" {
		c, err := decodeHistoryCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		after = &c
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	entries, err := s.repo.ListHistory(ctx, requesterID, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(entries) > pageSize {
		entries = entries[:pageSize]
		last := entries[pageSize-1]
		next = encodeHistoryCursor(HistoryCursor{WatchedAt: last.WatchedAt, VideoID: last.Video.ID})
	}
	return entries, next, nil
}

// DeleteWatchHistory menghapus satu video dari riwayat requesterID. Aman
// dipanggil walau video itu tidak ada di riwayat.
func (s *Service) DeleteWatchHistory(ctx context.Context, requesterID, videoID int64) error {
	if requesterID <= 0 {
		return errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return nil
	}
	_, err := s.repo.DeleteHistory(ctx, requesterID, videoID)
	return err
}

// ClearWatchHistory menghapus seluruh riwayat requesterID dan mengembalikan
// jumlah entri yang dihapus.
func (s *Service) ClearWatchHistory(ctx context.Context, requesterID int64) (int64, error) {
	if requesterID <= 0 {
		return 0, errors.New("requester id kosong")
	}
	return s.repo.DeleteHistory(ctx, requesterID, 0)
}

// ResumePosition menentukan dari detik berapa video dilanjutkan, dari
// posisi terakhir yang ditonton. 0 = mulai dari awal, yaitu jika posisinya
// masih di 5 detik pertama atau video sudah hampir selesai ditonton.
func ResumePosition(positionMs, durationMs int64) int64 {
	if positionMs < resumeMinMs || durationMs <= 0 {
		return 0
	}
	if durationMs-positionMs < resumeEndMarginMs {
		return 0
	}
	return positionMs
}

// encodeHistoryCursor mengubah cursor menjadi base64 dari
// "h<watched_at dalam mikrodetik>.<video_id>". Huruf "h" membedakannya dari
// token feed dan token pencarian.
func encodeHistoryCursor(c HistoryCursor) string {
	raw := "h" + strconv.FormatInt(c.WatchedAt.UnixMicro(), 10) + "." + strconv.FormatInt(c.VideoID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeHistoryCursor(token string) (HistoryCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return HistoryCursor{}, err
	}
	body, ok := strings.CutPrefix(string(raw), "h")
	if !ok {
		return HistoryCursor{}, errors.New("bukan token riwayat")
	}
	micros, id, ok := strings.Cut(body, ".")
	if !ok {
		return HistoryCursor{}, errors.New("format cursor salah")
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil || us <= 0 {
		return HistoryCursor{}, errors.New("waktu cursor salah")
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return HistoryCursor{}, errors.New("id cursor salah")
	}
	return HistoryCursor{WatchedAt: time.UnixMicro(us).UTC(), VideoID: n}, nil
}
