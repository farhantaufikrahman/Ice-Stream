// Package user berisi logika UserService: profil publik (channel), ubah
// profil, dan foto profil. Datanya ada di tabel USERS milik AuthService
// (schema AUTH_SVC); package ini hanya membaca dan mengubah kolom profil,
// tidak pernah menyentuh password atau token.
package user

import (
	"errors"
	"time"
)

// Error yang bisa dikembalikan Service. Lapisan gRPC menerjemahkannya
// menjadi kode status gRPC (lihat grpc.go).
var (
	// ErrNotFound juga dipakai untuk akun yang diblokir, supaya tidak
	// terlihat bedanya dari akun yang memang tidak ada.
	ErrNotFound = errors.New("user tidak ditemukan")
	// ErrTooManySubscriptions: requester sudah mengikuti maxSubscriptions
	// channel.
	ErrTooManySubscriptions = errors.New("kamu sudah mengikuti 1000 channel, berhenti mengikuti channel lain dulu")
)

// statusBanned sama dengan nilai di CHECK constraint users_status_ck.
const statusBanned = "BANNED"

// Profile adalah data profil user. Email hanya diisi untuk pemiliknya.
type Profile struct {
	ID          int64
	Username    string
	DisplayName string
	Bio         string
	// AvatarKey adalah lokasi foto profil di bucket, misalnya
	// "avatars/7/3f9c0a1b.jpg". Kosong jika belum ada.
	AvatarKey string
	Email     string
	Status    string
	CreatedAt time.Time
	// SubscriberCount adalah jumlah subscriber channel ini.
	SubscriberCount int64
}

// Subscription adalah satu baris tabel subscriptions: requester mengikuti
// ChannelID. ID dipakai untuk urutan dan pagination.
type Subscription struct {
	ID        int64
	ChannelID int64
}

// ProfileUpdate berisi field yang ingin diubah. nil = jangan diubah.
type ProfileUpdate struct {
	DisplayName *string
	Bio         *string
}

// ValidationError menandai input dari user yang tidak valid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
