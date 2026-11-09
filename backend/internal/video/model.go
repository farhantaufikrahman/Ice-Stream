// Package video berisi logika VideoService: mencatat video baru, membuat
// URL upload/putar, dan memeriksa file di object storage.
package video

import (
	"errors"
	"time"
)

// Status video, sama dengan CHECK constraint di tabel videos.
const (
	StatusPendingUpload = "PENDING_UPLOAD"
	StatusUploaded      = "UPLOADED"
)

// Error yang bisa dikembalikan Service. Lapisan gRPC menerjemahkannya
// menjadi kode status gRPC (lihat grpc.go).
var (
	// ErrNotFound juga dipakai jika video ada tapi bukan milik requester,
	// supaya orang lain tidak bisa menebak id video yang belum dipublikasi.
	ErrNotFound = errors.New("video tidak ditemukan")
	// ErrUploadIncomplete: file belum ada di storage, atau isinya tidak sesuai.
	ErrUploadIncomplete = errors.New("file video belum selesai diunggah")
	// ErrNotUploading: CompleteUpload dipanggil untuk video yang tidak sedang
	// menunggu upload.
	ErrNotUploading = errors.New("video tidak sedang menunggu upload")
)

// Video adalah satu baris di tabel videos.
type Video struct {
	ID          int64
	OwnerID     int64
	Title       string
	Description string
	Status      string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
	// UploadedAt bernilai nil selama status masih PENDING_UPLOAD.
	UploadedAt *time.Time
}

// NewVideo adalah data untuk mencatat video baru.
type NewVideo struct {
	OwnerID     int64
	Title       string
	Description string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
}

// ValidationError menandai input dari user yang tidak valid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
