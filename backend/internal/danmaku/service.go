package danmaku

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Repository adalah semua akses database yang dibutuhkan Service.
// Implementasi Oracle ada di oracle_repository.go; test memakai versi palsu.
type Repository interface {
	// VideoDuration mengembalikan durasi video READY dalam milidetik.
	// ErrVideoNotFound jika video tidak ada, sudah dihapus, atau belum READY.
	VideoDuration(ctx context.Context, videoID int64) (int64, error)
	// CountSince menghitung danmaku yang dikirim userID sesudah waktu since.
	CountSince(ctx context.Context, userID int64, since time.Time) (int64, error)
	// Create menyimpan danmaku baru dan mengisi ID serta CreatedAt.
	Create(ctx context.Context, d Danmaku) (Danmaku, error)
	// List mengambil danmaku videoID dengan posisi fromMs sampai toMs
	// (termasuk kedua ujung), urut (position_ms, id), hanya yang sesudah
	// cursor after.
	List(ctx context.Context, videoID, fromMs, toMs int64, after Cursor, limit int) ([]Danmaku, error)
}

// Publisher menyiarkan danmaku yang baru tersimpan ke penonton lain.
// Implementasi NATS ada di nats.go.
type Publisher interface {
	PublishDanmaku(ctx context.Context, d Danmaku) error
}

const (
	maxBodyRunes = 100
	// Rentang terpanjang satu kali ListDanmaku: 6 menit, sama seperti
	// potongan (segment) danmaku di Bilibili. Player mengambil potongan
	// berikutnya saat video hampir sampai di sana.
	maxWindowMs     = 6 * 60 * 1000
	defaultPageSize = 500
	maxPageSize     = 1000
	// Batas spam: maksimal 3 danmaku per 10 detik per user.
	rateLimitCount  = 3
	rateLimitWindow = 10 * time.Second
	// Toleransi posisi di akhir video: waktu player dan durasi hasil
	// transcoding bisa beda sedikit.
	positionSlackMs = 1000
)

// Service berisi aturan bisnis DanmakuService, terpisah dari gRPC, Oracle,
// dan NATS supaya mudah diuji.
type Service struct {
	repo      Repository
	publisher Publisher
	logger    *slog.Logger
	now       func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository, publisher Publisher, logger *slog.Logger) *Service {
	return &Service{repo: repo, publisher: publisher, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Send memeriksa dan menyimpan danmaku, lalu menyiarkannya. Siaran bersifat
// "best effort": kalau NATS sedang mati, danmaku tetap tersimpan (penonton
// lain akan melihatnya saat memuat ulang video), dan Send tetap berhasil.
func (s *Service) Send(ctx context.Context, in SendInput) (Danmaku, error) {
	if in.RequesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return Danmaku{}, errors.New("requester id kosong")
	}
	if in.VideoID <= 0 {
		return Danmaku{}, ErrVideoNotFound
	}
	d, err := validateInput(in)
	if err != nil {
		return Danmaku{}, err
	}

	duration, err := s.repo.VideoDuration(ctx, in.VideoID)
	if err != nil {
		return Danmaku{}, err
	}
	// duration 0 berarti durasinya tidak diketahui; posisinya tidak dicek.
	if duration > 0 && d.PositionMs > duration+positionSlackMs {
		return Danmaku{}, &ValidationError{"position_ms", fmt.Sprintf("melewati durasi video (%d ms)", duration)}
	}

	// Dua request yang datang bersamaan bisa sama-sama lolos pemeriksaan
	// ini, jadi kadang user bisa mengirim satu atau dua lebih banyak. Untuk
	// mencegah spam itu cukup; batas yang tepat butuh kunci (lock) per user.
	recent, err := s.repo.CountSince(ctx, in.RequesterID, s.now().Add(-rateLimitWindow))
	if err != nil {
		return Danmaku{}, err
	}
	if recent >= rateLimitCount {
		return Danmaku{}, ErrRateLimited
	}

	saved, err := s.repo.Create(ctx, d)
	if err != nil {
		return Danmaku{}, err
	}
	if err := s.publisher.PublishDanmaku(ctx, saved); err != nil {
		s.logger.WarnContext(ctx, "danmaku tersimpan tapi gagal disiarkan",
			slog.Int64("danmaku_id", saved.ID), slog.Any("error", err))
	}
	return saved, nil
}

// validateInput memeriksa isi danmaku dan mengisi nilai default.
func validateInput(in SendInput) (Danmaku, error) {
	body, err := normalizeBody(in.Body)
	if err != nil {
		return Danmaku{}, err
	}
	if in.PositionMs < 0 {
		return Danmaku{}, &ValidationError{"position_ms", "tidak boleh negatif"}
	}
	color := ColorWhite
	if in.Color != nil {
		if *in.Color > ColorWhite {
			return Danmaku{}, &ValidationError{"color", "harus warna RGB 0x000000 sampai 0xFFFFFF"}
		}
		color = *in.Color
	}
	mode := in.Mode
	if mode == "" {
		mode = ModeScroll
	}
	if !mode.Valid() {
		return Danmaku{}, &ValidationError{"mode", "harus SCROLL, TOP, atau BOTTOM"}
	}
	return Danmaku{
		VideoID:    in.VideoID,
		UserID:     in.RequesterID,
		PositionMs: in.PositionMs,
		Body:       body,
		Color:      color,
		Mode:       mode,
	}, nil
}

// normalizeBody membuang spasi di awal dan akhir, lalu memeriksa isi
// danmaku: 1 sampai 100 huruf, satu baris, tanpa karakter kontrol tak
// terlihat (aturan yang sama dengan komentar, tapi baris baru dan tab juga
// ditolak karena danmaku selalu tampil satu baris).
func normalizeBody(body string) (string, error) {
	if !utf8.ValidString(body) {
		return "", &ValidationError{"body", "bukan teks UTF-8 yang valid"}
	}
	body = strings.TrimSpace(body)
	n := utf8.RuneCountInString(body)
	if n == 0 || n > maxBodyRunes {
		return "", &ValidationError{"body", fmt.Sprintf("wajib diisi, maksimal %d karakter", maxBodyRunes)}
	}
	for _, r := range body {
		if r == '\n' || r == '\r' || r == '\t' {
			return "", &ValidationError{"body", "harus satu baris"}
		}
		// Cc = karakter kontrol, Cf = karakter format tak terlihat (misalnya
		// U+202E pembalik arah teks). U+200D (zero width joiner) tetap boleh
		// karena dipakai untuk menyusun emoji.
		if unicode.Is(unicode.Cc, r) || (unicode.Is(unicode.Cf, r) && r != '‍') {
			return "", &ValidationError{"body", "berisi karakter yang tidak diizinkan"}
		}
	}
	return body, nil
}

// List mengambil danmaku dalam rentang posisi fromMs sampai toMs.
func (s *Service) List(ctx context.Context, videoID, fromMs, toMs int64, pageSize int, pageToken string) ([]Danmaku, string, error) {
	if fromMs < 0 || toMs < fromMs {
		return nil, "", &ValidationError{"from_ms", "harus 0 atau lebih, dan tidak boleh lebih besar dari to_ms"}
	}
	if toMs-fromMs > maxWindowMs {
		return nil, "", &ValidationError{"to_ms", fmt.Sprintf("rentang maksimal %d ms (6 menit)", maxWindowMs)}
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	var after Cursor
	if pageToken != "" {
		c, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		after = c
	}
	if videoID <= 0 {
		return nil, "", ErrVideoNotFound
	}
	if _, err := s.repo.VideoDuration(ctx, videoID); err != nil {
		return nil, "", err
	}

	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	items, err := s.repo.List(ctx, videoID, fromMs, toMs, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	if len(items) <= pageSize {
		return items, "", nil
	}
	items = items[:pageSize]
	last := items[pageSize-1]
	return items, encodeCursor(Cursor{PositionMs: last.PositionMs, ID: last.ID}), nil
}

// encodeCursor mengubah cursor menjadi token opaque, misalnya
// base64("15000.42"). Client cukup mengirimnya kembali.
func encodeCursor(c Cursor) string {
	raw := strconv.FormatInt(c.PositionMs, 10) + "." + strconv.FormatInt(c.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(token string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, err
	}
	posStr, idStr, ok := strings.Cut(string(raw), ".")
	if !ok {
		return Cursor{}, errors.New("format cursor salah")
	}
	pos, err1 := strconv.ParseInt(posStr, 10, 64)
	id, err2 := strconv.ParseInt(idStr, 10, 64)
	if err1 != nil || err2 != nil || pos < 0 || id <= 0 {
		return Cursor{}, errors.New("isi cursor salah")
	}
	return Cursor{PositionMs: pos, ID: id}, nil
}
