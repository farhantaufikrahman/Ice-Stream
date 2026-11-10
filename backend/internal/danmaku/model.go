// Package danmaku berisi logika DanmakuService: komentar melayang yang
// menempel pada detik tertentu di video, khas Bilibili. Datanya ada di
// schema VIDEO_SVC (tabel danmaku), dan service ini berjalan di proses yang
// sama dengan VideoService.
package danmaku

import (
	"errors"
	"time"
)

// Error yang bisa dikembalikan Service. Lapisan gRPC menerjemahkannya
// menjadi kode status gRPC (lihat grpc.go).
var (
	// ErrVideoNotFound: video tidak ada, sudah dihapus, atau belum READY.
	ErrVideoNotFound = errors.New("video tidak ditemukan")
	// ErrRateLimited: user mengirim danmaku terlalu cepat.
	ErrRateLimited = errors.New("terlalu banyak danmaku, tunggu beberapa detik")
)

// Mode menentukan cara danmaku tampil di layar.
type Mode string

const (
	// ModeScroll: melayang dari kanan ke kiri (default).
	ModeScroll Mode = "SCROLL"
	// ModeTop: diam di bagian atas layar.
	ModeTop Mode = "TOP"
	// ModeBottom: diam di bagian bawah layar.
	ModeBottom Mode = "BOTTOM"
)

// Valid mengembalikan true untuk ketiga mode yang dikenal.
func (m Mode) Valid() bool {
	return m == ModeScroll || m == ModeTop || m == ModeBottom
}

// ColorWhite adalah warna default, #FFFFFF.
const ColorWhite uint32 = 0xFFFFFF

// Danmaku adalah satu baris di tabel danmaku.
type Danmaku struct {
	ID      int64
	VideoID int64
	// UserID adalah pengirim. Tidak ditampilkan ke penonton lain.
	UserID int64
	// PositionMs adalah milidetik dari awal video.
	PositionMs int64
	Body       string
	// Color adalah warna RGB, 0x000000 sampai 0xFFFFFF.
	Color     uint32
	Mode      Mode
	CreatedAt time.Time
}

// SendInput adalah isi permintaan mengirim danmaku.
type SendInput struct {
	RequesterID int64
	VideoID     int64
	PositionMs  int64
	Body        string
	// Color nil = tidak dikirim, memakai ColorWhite.
	Color *uint32
	// Mode kosong = ModeScroll.
	Mode Mode
}

// Cursor menandai danmaku terakhir di halaman sebelumnya. Urutan danmaku
// adalah (PositionMs, ID), jadi cursor butuh keduanya: banyak danmaku bisa
// punya posisi yang sama persis.
type Cursor struct {
	PositionMs int64
	ID         int64
}

// IsZero mengembalikan true untuk halaman pertama.
func (c Cursor) IsZero() bool { return c.ID == 0 }

// ValidationError menandai input dari user yang tidak valid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
