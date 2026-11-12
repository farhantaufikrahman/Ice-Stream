// Package livestream berisi logika LiveService: siaran langsung (langkah
// 25a). Datanya ada di schema VIDEO_SVC (tabel live_channels dan
// live_streams), dan service ini berjalan di proses yang sama dengan
// VideoService.
//
// Pembagian tugasnya:
//
//	OBS --RTMP--> MediaMTX --HLS--> penonton
//	                 |  ^
//	   tanya "boleh  |  | baca daftar stream
//	   siaran?"      v  | setiap 5 detik
//	              VideoService (paket ini) --> Oracle
//
// Video tidak pernah melewati Go. MediaMTX (server media open source yang
// juga ditulis dengan Go) yang menerima dan membagikan videonya. Paket ini
// hanya memeriksa stream key dan mencatat siapa yang sedang live.
//
// Nama paketnya "livestream", bukan "live", karena internal/live sudah
// dipakai untuk siaran danmaku ke browser (SSE).
package livestream

import (
	"errors"
	"time"
)

// Error yang bisa dikembalikan Service. Lapisan gRPC menerjemahkannya
// menjadi kode status gRPC (lihat grpc.go).
var (
	// ErrNotFound: siaran tidak ada.
	ErrNotFound = errors.New("siaran tidak ditemukan")
	// ErrForbidden: stream key salah atau path tidak dikenal. Dipakai
	// hook MediaMTX, tidak pernah sampai ke gateway.
	ErrForbidden = errors.New("tidak boleh siaran")
	// ErrStreamEnded: chat dikirim ke siaran yang sudah selesai.
	ErrStreamEnded = errors.New("siaran sudah selesai")
	// ErrRateLimited: user mengirim chat lebih cepat dari mode lambat.
	ErrRateLimited = errors.New("tunggu sebentar sebelum mengirim pesan lagi")
)

// Status siaran, sama dengan CHECK constraint live_streams_status_ck.
type Status string

const (
	StatusLive  Status = "LIVE"
	StatusEnded Status = "ENDED"
)

// DefaultTitle adalah judul siaran sebelum kreator mengubahnya.
const DefaultTitle = "Siaran langsung"

// Channel adalah baris di tabel live_channels.
type Channel struct {
	OwnerID  int64
	Title    string
	Category string
	// StreamKeyHash kosong jika kreator belum pernah membuat stream key.
	StreamKeyHash      string
	StreamKeyCreatedAt time.Time
	// PublishTokenHash adalah hash token siaran browser (langkah 25b).
	// Kosong jika belum pernah dibuat.
	PublishTokenHash      string
	PublishTokenExpiresAt time.Time
}

// Stream adalah satu kali siaran (tabel live_streams).
type Stream struct {
	ID          int64
	OwnerID     int64
	Title       string
	Category    string
	Status      Status
	StartedAt   time.Time
	EndedAt     time.Time
	ViewerCount int64
	PeakViewers int64
}

// Online adalah stream yang sedang diterima MediaMTX untuk satu kreator.
type Online struct {
	OwnerID int64
	// ReadySince adalah waktu MediaMTX mulai menerima video.
	ReadySince time.Time
	// Viewers adalah jumlah penonton HLS yang sedang memutar.
	Viewers int64
}

// ListFilter membatasi ListLive. Field kosong = tidak dibatasi.
type ListFilter struct {
	Category string
	OwnerIDs []int64
	// Query mencari teks di judul (huruf besar/kecil dianggap sama).
	Query string
	Limit int
}

// ChatMessage adalah satu pesan chat siaran (tabel live_chat_messages).
type ChatMessage struct {
	ID        int64
	StreamID  int64
	UserID    int64
	Body      string
	Color     uint32
	CreatedAt time.Time
}

// ColorWhite adalah warna chat default.
const ColorWhite uint32 = 0xFFFFFF

// PublishRequest adalah isi permintaan "boleh siaran?" dari MediaMTX.
type PublishRequest struct {
	// Path adalah nama stream, misalnya "live/7".
	Path string
	// Query adalah sisa URL RTMP dari OBS, misalnya "key=isk_...".
	Query string
	// Token dari header "Authorization: Bearer ..." saat siaran dari
	// browser (WHIP).
	Token string
}

// ValidationError menandai input dari user yang tidak valid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
