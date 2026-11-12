package comment

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Repository adalah semua akses database yang dibutuhkan Service.
// Implementasi Oracle ada di oracle_repository.go; test memakai versi palsu.
type Repository interface {
	// CheckVideo mengembalikan ErrVideoNotFound jika video tidak ada,
	// sudah dihapus, atau belum READY.
	CheckVideo(ctx context.Context, videoID int64) error
	// Create menyimpan komentar baru dan menambah comment_count video (dan
	// reply_count komentar utama untuk balasan) dalam satu transaksi.
	// ErrVideoNotFound jika video tidak READY, ErrNotFound jika komentar
	// yang dibalas tidak ada.
	Create(ctx context.Context, c NewComment) (Comment, error)
	// FindRoot mencari komentar utama milik videoID. ErrNotFound jika tidak
	// ada, sudah dihapus, atau ternyata sebuah balasan.
	FindRoot(ctx context.Context, videoID, commentID int64) (Comment, error)
	// ListRoots mengambil komentar utama, id terbesar lebih dulu, hanya
	// yang id-nya lebih kecil dari beforeID (0 = dari yang terbaru).
	ListRoots(ctx context.Context, videoID, beforeID int64, limit int) ([]Comment, error)
	// ListReplies mengambil balasan untuk rootID, id terkecil lebih dulu,
	// hanya yang id-nya lebih besar dari afterID (0 = dari awal).
	ListReplies(ctx context.Context, videoID, rootID, afterID int64, limit int) ([]Comment, error)
	// Delete menghapus komentar (beserta balasannya jika komentar utama)
	// dan mengurangi penghitungnya, dalam satu transaksi. Sebelum menghapus,
	// authorize dipanggil dengan komentar dan id pemilik video; jika
	// authorize mengembalikan error, tidak ada yang dihapus.
	Delete(ctx context.Context, videoID, commentID int64, now time.Time, authorize func(c Comment, videoOwnerID int64) error) error
	// ListByChannel mengambil komentar dan balasan di video milik ownerID
	// (hanya videoID jika > 0) yang belum dihapus, id terbesar lebih dulu,
	// hanya yang id-nya lebih kecil dari beforeID (0 = dari yang terbaru).
	ListByChannel(ctx context.Context, ownerID, videoID, beforeID int64, limit int) ([]ChannelComment, error)
}

const (
	maxBodyRunes    = 1000
	defaultPageSize = 20
	maxPageSize     = 50
)

// Service berisi aturan bisnis CommentService, terpisah dari gRPC dan
// Oracle supaya mudah diuji.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// CreateComment menulis komentar atas nama requesterID. parentID 0 berarti
// komentar utama; selain itu balasan.
func (s *Service) CreateComment(ctx context.Context, requesterID, videoID, parentID int64, body string) (Comment, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return Comment{}, errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return Comment{}, ErrVideoNotFound
	}
	if parentID < 0 {
		return Comment{}, ErrNotFound
	}
	clean, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	return s.repo.Create(ctx, NewComment{VideoID: videoID, AuthorID: requesterID, ParentID: parentID, Body: clean})
}

// normalizeBody merapikan isi komentar: baris baru Windows (\r\n) menjadi
// \n, spasi di awal dan akhir dibuang. Karakter kontrol tak terlihat
// (selain baris baru dan tab) ditolak, karena bisa dipakai untuk merusak
// tampilan, misalnya karakter pembalik arah teks U+202E.
func normalizeBody(body string) (string, error) {
	if !utf8.ValidString(body) {
		return "", &ValidationError{"body", "bukan teks UTF-8 yang valid"}
	}
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	n := utf8.RuneCountInString(body)
	if n == 0 || n > maxBodyRunes {
		return "", &ValidationError{"body", fmt.Sprintf("wajib diisi, maksimal %d karakter", maxBodyRunes)}
	}
	for _, r := range body {
		if r == '\n' || r == '\t' {
			continue
		}
		// Cc = karakter kontrol (misalnya \x00), Cf = karakter format tak
		// terlihat (misalnya U+202E). U+200D (zero width joiner) tetap
		// boleh karena dipakai untuk menyusun emoji seperti 👨‍👩‍👧.
		if unicode.Is(unicode.Cc, r) || (unicode.Is(unicode.Cf, r) && r != '‍') {
			return "", &ValidationError{"body", "berisi karakter yang tidak diizinkan"}
		}
	}
	return body, nil
}

// ListComments mengambil komentar utama sebuah video, terbaru lebih dulu.
func (s *Service) ListComments(ctx context.Context, videoID int64, pageSize int, pageToken string) ([]Comment, string, error) {
	limit, cursor, err := parsePage(pageSize, pageToken)
	if err != nil {
		return nil, "", err
	}
	if err := s.checkVideo(ctx, videoID); err != nil {
		return nil, "", err
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	comments, err := s.repo.ListRoots(ctx, videoID, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	comments, next := cutPage(comments, limit)
	return comments, next, nil
}

// ListReplies mengambil balasan untuk satu komentar utama, terlama lebih dulu.
func (s *Service) ListReplies(ctx context.Context, videoID, commentID int64, pageSize int, pageToken string) ([]Comment, string, error) {
	limit, cursor, err := parsePage(pageSize, pageToken)
	if err != nil {
		return nil, "", err
	}
	if err := s.checkVideo(ctx, videoID); err != nil {
		return nil, "", err
	}
	if commentID <= 0 {
		return nil, "", ErrNotFound
	}
	if _, err := s.repo.FindRoot(ctx, videoID, commentID); err != nil {
		return nil, "", err
	}
	replies, err := s.repo.ListReplies(ctx, videoID, commentID, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	replies, next := cutPage(replies, limit)
	return replies, next, nil
}

// DeleteComment menghapus komentar. Penulis boleh menghapus komentarnya
// sendiri, dan pemilik video boleh menghapus komentar apa pun di videonya
// (seperti YouTube Studio atau Bilibili UP主).
func (s *Service) DeleteComment(ctx context.Context, requesterID, videoID, commentID int64) error {
	if requesterID <= 0 {
		return errors.New("requester id kosong")
	}
	if videoID <= 0 || commentID <= 0 {
		return ErrNotFound
	}
	return s.repo.Delete(ctx, videoID, commentID, s.now(), func(c Comment, videoOwnerID int64) error {
		if requesterID == c.AuthorID || requesterID == videoOwnerID {
			return nil
		}
		return ErrForbidden
	})
}

// ListChannelComments mengambil komentar di video milik requesterID untuk
// Creator Studio. videoID 0 = semua video. Video milik orang lain
// menghasilkan daftar kosong (bukan error), karena syarat pemilik ada di
// query.
func (s *Service) ListChannelComments(ctx context.Context, requesterID, videoID int64, pageSize int, pageToken string) ([]ChannelComment, string, error) {
	if requesterID <= 0 {
		return nil, "", errors.New("requester id kosong")
	}
	if videoID < 0 {
		return nil, "", ErrVideoNotFound
	}
	limit, cursor, err := parsePage(pageSize, pageToken)
	if err != nil {
		return nil, "", err
	}
	comments, err := s.repo.ListByChannel(ctx, requesterID, videoID, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	if len(comments) <= limit {
		return comments, "", nil
	}
	comments = comments[:limit]
	return comments, encodeCursor(comments[limit-1].ID), nil
}

func (s *Service) checkVideo(ctx context.Context, videoID int64) error {
	if videoID <= 0 {
		return ErrVideoNotFound
	}
	return s.repo.CheckVideo(ctx, videoID)
}

// parsePage memeriksa page_size dan membaca page_token (id komentar
// terakhir di halaman sebelumnya, 0 untuk halaman pertama).
func parsePage(pageSize int, pageToken string) (int, int64, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return 0, 0, &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	if pageToken == "" {
		return pageSize, 0, nil
	}
	id, err := decodeCursor(pageToken)
	if err != nil {
		return 0, 0, &ValidationError{"page_token", "tidak valid"}
	}
	return pageSize, id, nil
}

// cutPage membuang data ke-(limit+1) dan membuat token halaman berikutnya
// dari id komentar terakhir. Token kosong = sudah halaman terakhir.
func cutPage(comments []Comment, limit int) ([]Comment, string) {
	if len(comments) <= limit {
		return comments, ""
	}
	comments = comments[:limit]
	return comments, encodeCursor(comments[limit-1].ID)
}

// encodeCursor mengubah id menjadi token opaque (base64), sama seperti
// feed video: client cukup mengirimnya kembali tanpa perlu memahami isinya.
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeCursor(token string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("id cursor salah")
	}
	return id, nil
}
