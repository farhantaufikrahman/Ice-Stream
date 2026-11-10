// Package comment berisi logika CommentService: komentar dan balasan di
// bawah video. Datanya ada di schema VIDEO_SVC (tabel comments), dan
// service ini berjalan di proses yang sama dengan VideoService.
package comment

import (
	"errors"
	"time"
)

// Error yang bisa dikembalikan Service. Lapisan gRPC menerjemahkannya
// menjadi kode status gRPC (lihat grpc.go).
var (
	// ErrVideoNotFound: video tidak ada, sudah dihapus, atau belum READY.
	ErrVideoNotFound = errors.New("video tidak ditemukan")
	// ErrNotFound: komentar tidak ada, sudah dihapus, atau bukan milik
	// video yang disebut di request.
	ErrNotFound = errors.New("komentar tidak ditemukan")
	// ErrForbidden: requester bukan penulis komentar dan bukan pemilik video.
	ErrForbidden = errors.New("kamu tidak boleh menghapus komentar ini")
)

// Comment adalah satu baris di tabel comments.
type Comment struct {
	ID       int64
	VideoID  int64
	AuthorID int64
	// ParentID bernilai 0 untuk komentar utama.
	ParentID int64
	Body     string
	// ReplyCount adalah jumlah balasan yang belum dihapus (komentar utama).
	ReplyCount int64
	CreatedAt  time.Time
}

// IsRoot mengembalikan true untuk komentar utama (bukan balasan).
func (c Comment) IsRoot() bool { return c.ParentID == 0 }

// NewComment adalah data untuk menyimpan komentar baru.
type NewComment struct {
	VideoID  int64
	AuthorID int64
	// ParentID 0 = komentar utama. Jika diisi, boleh id balasan; repository
	// menyimpannya di bawah komentar utama dari balasan itu.
	ParentID int64
	Body     string
}

// ValidationError menandai input dari user yang tidak valid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
