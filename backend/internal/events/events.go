// Package events berisi "kontrak" pesan antar service yang lewat NATS
// JetStream: nama subject, isi pesan (JSON), dan konfigurasi stream.
//
// Seperti file .proto untuk gRPC, paket ini dipakai bersama oleh pengirim
// dan penerima, supaya keduanya selalu sepakat tentang bentuk pesannya.
//
// Alurnya:
//
//	VideoService --videos.uploaded--> transcoder
//	transcoder   --videos.transcode.started/succeeded/failed--> VideoService
//
// Transcoder sengaja tidak menyentuh database. Hanya VideoService yang
// boleh mengubah tabel videos (setiap service memiliki datanya sendiri).
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// StreamName adalah nama stream JetStream yang menampung semua event video.
const StreamName = "VIDEOS"

// Subject (alamat) setiap jenis event. Mirip nama topic di Kafka.
const (
	SubjectUploaded           = "videos.uploaded"
	SubjectTranscodeStarted   = "videos.transcode.started"
	SubjectTranscodeSucceeded = "videos.transcode.succeeded"
	SubjectTranscodeFailed    = "videos.transcode.failed"
	// SubjectTranscodeAll dipakai VideoService untuk menerima ketiga event
	// transcode sekaligus (tanda > berarti "apa saja sesudahnya").
	SubjectTranscodeAll = "videos.transcode.>"
)

// Nama consumer (pembaca) yang tahan lama (durable): posisi bacanya disimpan
// di server, jadi pesan tidak hilang walau pod sedang restart.
const (
	ConsumerTranscoder  = "transcoder"
	ConsumerVideoStatus = "video-status"
)

// Uploaded dikirim VideoService setelah file mentah selesai diunggah.
type Uploaded struct {
	VideoID     int64  `json:"video_id"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
}

// TranscodeStarted dikirim transcoder saat mulai mengerjakan satu video.
type TranscodeStarted struct {
	VideoID int64 `json:"video_id"`
}

// TranscodeSucceeded dikirim transcoder setelah semua file HLS dan
// thumbnail tersimpan di object storage.
type TranscodeSucceeded struct {
	VideoID int64 `json:"video_id"`
	// PlaylistKey adalah lokasi master playlist, misalnya "hls/42/master.m3u8".
	PlaylistKey string `json:"playlist_key"`
	// ThumbnailKey adalah lokasi gambar thumbnail, misalnya "hls/42/thumbnail.jpg".
	ThumbnailKey string `json:"thumbnail_key"`
	DurationMs   int64  `json:"duration_ms"`
	// Renditions berisi resolusi yang dibuat, misalnya ["360p", "720p"].
	Renditions []string `json:"renditions"`
}

// TranscodeFailed dikirim transcoder jika video tidak bisa diproses, baik
// karena filenya rusak maupun karena sudah gagal berkali-kali.
type TranscodeFailed struct {
	VideoID int64  `json:"video_id"`
	Reason  string `json:"reason"`
}

// Connect membuka koneksi ke NATS. Koneksi otomatis tersambung lagi jika
// server NATS restart, jadi service tidak perlu ikut restart. extra berisi
// pengaturan tambahan, misalnya nats.RetryOnFailedConnect(true).
func Connect(url, clientName string, extra ...nats.Option) (*nats.Conn, error) {
	opts := append([]nats.Option{
		nats.Name(clientName),
		nats.MaxReconnects(-1), // coba terus, jangan pernah menyerah
		nats.ReconnectWait(2 * time.Second),
	}, extra...)
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke NATS %s: %w", url, err)
	}
	return nc, nil
}

// EnsureStream membuat stream VIDEOS, atau memperbaruinya jika sudah ada.
// Dipanggil VideoService dan transcoder saat start, jadi urutan start
// keduanya tidak penting.
func EnsureStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{"videos.>"},
		// Disimpan di disk (PersistentVolume), bukan hanya di RAM.
		Storage: jetstream.FileStorage,
		// Event disimpan 7 hari, cukup untuk memproses ulang jika ada masalah.
		Retention: jetstream.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
		// Pesan dengan Msg-Id yang sama dalam 10 menit dianggap duplikat dan
		// dibuang. Jadi aman kalau VideoService mengirim event yang sama dua kali.
		Duplicates: 10 * time.Minute,
		Replicas:   1,
	})
	if err != nil {
		return fmt.Errorf("membuat stream %s: %w", StreamName, err)
	}
	return nil
}

// Publish mengirim v (diubah ke JSON) ke subject, lalu menunggu konfirmasi
// bahwa JetStream sudah menyimpannya. msgID dipakai untuk membuang duplikat.
func Publish(ctx context.Context, js jetstream.JetStream, subject, msgID string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode event %s: %w", subject, err)
	}
	if _, err := js.Publish(ctx, subject, data, jetstream.WithMsgID(msgID)); err != nil {
		return fmt.Errorf("publish event %s: %w", subject, err)
	}
	return nil
}

// MsgID membuat Msg-Id yang stabil, misalnya "videos.uploaded/42".
func MsgID(subject string, videoID int64) string {
	return subject + "/" + strconv.FormatInt(videoID, 10)
}

// Danmaku yang baru terkirim disiarkan lewat NATS biasa (core NATS), BUKAN
// JetStream: pesannya tidak disimpan di NATS. Kalau tidak ada gateway yang
// sedang mendengarkan, pesannya hilang, dan itu tidak masalah karena
// danmaku sudah tersimpan di Oracle dan bisa diambil lewat ListDanmaku.
//
//	VideoService --danmaku.live.<video_id>--> semua pod API gateway yang
//	                                         punya penonton video itu
//
// Subject ini sengaja tidak diawali "videos.", supaya tidak ikut tersimpan
// di stream VIDEOS.
const subjectDanmakuLivePrefix = "danmaku.live."

// SubjectDanmakuLive mengembalikan subject siaran danmaku untuk satu video,
// misalnya "danmaku.live.42".
func SubjectDanmakuLive(videoID int64) string {
	return subjectDanmakuLivePrefix + strconv.FormatInt(videoID, 10)
}

// DanmakuSent dikirim VideoService setelah satu danmaku tersimpan.
type DanmakuSent struct {
	ID         int64  `json:"id"`
	VideoID    int64  `json:"video_id"`
	PositionMs int64  `json:"position_ms"`
	Body       string `json:"body"`
	// Color adalah warna RGB, 0x000000 sampai 0xFFFFFF.
	Color uint32 `json:"color"`
	// Mode bernilai "SCROLL", "TOP", atau "BOTTOM".
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
}
