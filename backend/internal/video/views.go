package video

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// ViewBucket adalah nama bucket NATS Key-Value untuk mencatat siapa yang
// baru saja menonton video apa.
const ViewBucket = "video_views"

// NATSViewDeduper mencegah satu penonton menambah views berkali-kali
// dalam waktu singkat (misalnya karena menekan refresh terus).
//
// Setiap view disimpan sebagai key "<video_id>.<viewer_key>" di bucket
// Key-Value NATS JetStream dengan umur (TTL) tertentu. Selama key masih
// ada, view berikutnya dari penonton yang sama tidak dihitung. Setelah
// umurnya habis, NATS menghapus key itu sendiri, jadi tidak perlu job
// pembersih.
//
// Padanannya di Node.js atau Java biasanya Redis:
// SET key 1 NX EX 1800 (ioredis / Spring Data Redis setIfAbsent).
type NATSViewDeduper struct {
	kv jetstream.KeyValue
}

var _ ViewDeduper = (*NATSViewDeduper)(nil)

// NewNATSViewDeduper membuat (atau memperbarui) bucket ViewBucket dengan
// umur key = window. Aman dipanggil setiap kali service start.
func NewNATSViewDeduper(ctx context.Context, js jetstream.JetStream, window time.Duration) (*NATSViewDeduper, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      ViewBucket,
		Description: "Penanda view per penonton, supaya refresh berulang tidak menambah views",
		TTL:         window,
		// Satu key cukup menyimpan nilai terakhir.
		History: 1,
		Storage: jetstream.FileStorage,
		// Satu key sekitar 100 byte. 256 MiB cukup untuk jutaan view dalam
		// satu window. Kalau penuh, Claim gagal dan view tidak dihitung
		// (lebih baik kurang menghitung daripada menghitung dobel).
		MaxBytes: 256 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("membuat bucket KV %s: %w", ViewBucket, err)
	}
	return &NATSViewDeduper{kv: kv}, nil
}

// Claim mencatat bahwa viewerKey menonton videoID. Hasilnya true jika ini
// view pertama dalam window, false jika penonton yang sama sudah tercatat.
//
// kv.Create hanya berhasil jika key belum ada, dan pemeriksaan ini
// dilakukan server NATS secara atomik. Jadi walau ada beberapa pod
// VideoService, dua request bersamaan dari penonton yang sama tetap hanya
// dihitung sekali.
func (d *NATSViewDeduper) Claim(ctx context.Context, videoID int64, viewerKey string) (bool, error) {
	_, err := d.kv.Create(ctx, viewKey(videoID, viewerKey), []byte("1"))
	if errors.Is(err, jetstream.ErrKeyExists) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Release menghapus tanda dari Claim. Dipanggil jika menambah views di
// Oracle gagal, supaya penonton bisa dihitung lagi saat mencoba ulang.
func (d *NATSViewDeduper) Release(ctx context.Context, videoID int64, viewerKey string) error {
	return d.kv.Delete(ctx, viewKey(videoID, viewerKey))
}

// viewKey membuat key KV, misalnya "42.u-7". Titik adalah pemisah yang
// umum di NATS, dan viewerKey sudah dipastikan hanya berisi huruf, angka,
// "-", dan "_" (lihat validViewerKey).
func viewKey(videoID int64, viewerKey string) string {
	return strconv.FormatInt(videoID, 10) + "." + viewerKey
}
