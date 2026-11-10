package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
)

const (
	// MaxAttempts adalah berapa kali satu video dicoba sebelum dinyatakan
	// FAILED.
	MaxAttempts = 3
	// MaxDeliver dipasang di konfigurasi consumer: batas NATS mengirim ulang
	// satu pesan. Dibuat 2 lebih banyak dari MaxAttempts, karena pesan yang
	// dikembalikan saat pod dimatikan (restart/deploy) juga ikut terhitung.
	// Tanpa cadangan ini, video bisa tersangkut di PROCESSING selamanya.
	MaxDeliver = MaxAttempts + 2
	// AckWait: jika worker diam selama ini tanpa kabar, NATS menganggapnya
	// mati dan mengirim tugasnya ke worker lain. Selama ffmpeg berjalan,
	// worker mengirim "masih dikerjakan" setiap heartbeatInterval.
	AckWait           = time.Minute
	heartbeatInterval = 15 * time.Second
	// retryDelay adalah jeda sebelum video yang gagal dicoba lagi.
	retryDelay = 30 * time.Second
)

// Media adalah pekerjaan ffmpeg yang dibutuhkan worker. Implementasinya
// FFmpeg; test memakai versi palsu supaya tidak perlu ffmpeg sungguhan.
type Media interface {
	Probe(ctx context.Context, path string) (ProbeResult, error)
	TranscodeHLS(ctx context.Context, input, outDir string, renditions []Rendition, hasAudio bool) error
	Thumbnail(ctx context.Context, input, output string, durationMs int64) error
}

// Publisher mengirim event hasil kerja transcoder.
type Publisher interface {
	Publish(ctx context.Context, subject, msgID string, v any) error
}

// JetStreamPublisher mengirim event lewat NATS JetStream.
type JetStreamPublisher struct{ JS jetstream.JetStream }

func (p JetStreamPublisher) Publish(ctx context.Context, subject, msgID string, v any) error {
	return events.Publish(ctx, p.JS, subject, msgID, v)
}

// Worker memproses event videos.uploaded satu per satu.
type Worker struct {
	media      Media
	storage    Storage
	publisher  Publisher
	workDir    string
	jobTimeout time.Duration
	logger     *slog.Logger
}

// NewWorker membuat Worker.
func NewWorker(media Media, storage Storage, publisher Publisher, workDir string, jobTimeout time.Duration, logger *slog.Logger) *Worker {
	return &Worker{media: media, storage: storage, publisher: publisher, workDir: workDir, jobTimeout: jobTimeout, logger: logger}
}

// permanentError menandai kegagalan yang tidak akan sembuh walau dicoba
// lagi, misalnya file yang bukan video. Video langsung dinyatakan FAILED.
// reason adalah kalimat singkat untuk user (disimpan di failure_reason);
// detail teknisnya (err) hanya masuk log.
type permanentError struct {
	reason string
	err    error
}

func (e permanentError) Error() string { return e.reason + ": " + e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Handle memproses satu pesan dari NATS. Selalu diakhiri dengan salah satu:
//   - Ack:  berhasil, pesan selesai.
//   - Nak:  gagal sementara, NATS mengirim ulang nanti.
//   - Term: gagal permanen atau sudah dicoba MaxDeliver kali, pesan dibuang
//     setelah event videos.transcode.failed dikirim.
func (w *Worker) Handle(ctx context.Context, msg jetstream.Msg) {
	var ev events.Uploaded
	if err := json.Unmarshal(msg.Data(), &ev); err != nil || ev.VideoID <= 0 || ev.ObjectKey == "" {
		w.logger.Error("pesan videos.uploaded tidak valid, dibuang", slog.String("data", string(msg.Data())))
		_ = msg.TermWithReason("payload tidak valid")
		return
	}
	attempt := uint64(1)
	if md, err := msg.Metadata(); err == nil {
		attempt = md.NumDelivered
	}
	log := w.logger.With(slog.Int64("video_id", ev.VideoID), slog.Uint64("percobaan", attempt))

	if err := w.publisher.Publish(ctx, events.SubjectTranscodeStarted, events.MsgID(events.SubjectTranscodeStarted, ev.VideoID), events.TranscodeStarted{VideoID: ev.VideoID}); err != nil {
		log.Error("gagal mengirim event started", slog.Any("error", err))
		_ = msg.NakWithDelay(retryDelay)
		return
	}

	// Kirim "masih dikerjakan" berkala selama transcoding berjalan.
	stopHeartbeat := heartbeat(msg)
	start := time.Now()
	log.Info("transcoding dimulai", slog.String("object_key", ev.ObjectKey))
	result, err := w.Process(ctx, ev)
	stopHeartbeat()

	if err == nil {
		err = w.publisher.Publish(ctx, events.SubjectTranscodeSucceeded, events.MsgID(events.SubjectTranscodeSucceeded, ev.VideoID), result)
		if err == nil {
			log.Info("transcoding selesai", slog.Duration("lama", time.Since(start)), slog.Any("resolusi", result.Renditions))
			_ = msg.Ack()
			return
		}
	}

	var perm permanentError
	switch {
	case ctx.Err() != nil:
		// Pod sedang dimatikan: kembalikan tugas supaya dikerjakan lagi nanti.
		log.Warn("transcoding dihentikan karena pod berhenti, akan diulang")
		_ = msg.Nak()
	case errors.As(err, &perm) || attempt >= MaxAttempts:
		log.Error("transcoding gagal, video ditandai FAILED", slog.Any("error", err))
		reason := fmt.Sprintf("transcoding gagal setelah %d kali percobaan", attempt)
		if errors.As(err, &perm) {
			reason = perm.reason
		}
		fail := events.TranscodeFailed{VideoID: ev.VideoID, Reason: reason}
		if pubErr := w.publisher.Publish(ctx, events.SubjectTranscodeFailed, events.MsgID(events.SubjectTranscodeFailed, ev.VideoID), fail); pubErr != nil {
			log.Error("gagal mengirim event failed", slog.Any("error", pubErr))
			_ = msg.NakWithDelay(retryDelay)
			return
		}
		_ = msg.TermWithReason("transcoding gagal")
	default:
		log.Warn("transcoding gagal, akan dicoba lagi", slog.Any("error", err), slog.Duration("jeda", retryDelay))
		_ = msg.NakWithDelay(retryDelay)
	}
}

// heartbeat memanggil msg.InProgress() setiap heartbeatInterval sampai
// fungsi yang dikembalikan dipanggil.
func heartbeat(msg jetstream.Msg) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = msg.InProgress()
			}
		}
	}()
	return func() { close(done) }
}

// Process mengerjakan satu video dari awal sampai akhir:
// unduh file mentah -> ffprobe -> ffmpeg HLS + thumbnail -> unggah hasil.
func (w *Worker) Process(ctx context.Context, ev events.Uploaded) (events.TranscodeSucceeded, error) {
	ctx, cancel := context.WithTimeout(ctx, w.jobTimeout)
	defer cancel()

	// Folder kerja khusus video ini, dihapus setelah selesai (berhasil
	// maupun gagal) supaya disk tidak penuh.
	dir, err := os.MkdirTemp(w.workDir, "video-"+strconv.FormatInt(ev.VideoID, 10)+"-")
	if err != nil {
		return events.TranscodeSucceeded{}, fmt.Errorf("membuat folder kerja: %w", err)
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "source")
	if err := w.storage.Download(ctx, ev.ObjectKey, source); err != nil {
		if errors.Is(err, ErrSourceMissing) {
			return events.TranscodeSucceeded{}, permanentError{"file video tidak ditemukan di storage", err}
		}
		return events.TranscodeSucceeded{}, err
	}

	probe, err := w.media.Probe(ctx, source)
	if err != nil {
		// File yang tidak bisa dibaca ffprobe tidak akan membaik jika diulang.
		if ctx.Err() == nil {
			return events.TranscodeSucceeded{}, permanentError{"file tidak bisa dibaca sebagai video (format tidak didukung atau file rusak)", err}
		}
		return events.TranscodeSucceeded{}, err
	}

	renditions := Renditions(probe.Height)
	out := filepath.Join(dir, "hls")
	if err := w.media.TranscodeHLS(ctx, source, out, renditions, probe.HasAudio); err != nil {
		return events.TranscodeSucceeded{}, err
	}
	if err := w.media.Thumbnail(ctx, source, filepath.Join(out, "thumbnail.jpg"), probe.DurationMs); err != nil {
		return events.TranscodeSucceeded{}, err
	}

	prefix := "hls/" + strconv.FormatInt(ev.VideoID, 10) + "/"
	if err := w.storage.UploadDir(ctx, out, prefix); err != nil {
		return events.TranscodeSucceeded{}, err
	}

	names := make([]string, len(renditions))
	for i, r := range renditions {
		names[i] = r.Name
	}
	return events.TranscodeSucceeded{
		VideoID:      ev.VideoID,
		PlaylistKey:  prefix + "master.m3u8",
		ThumbnailKey: prefix + "thumbnail.jpg",
		DurationMs:   probe.DurationMs,
		Renditions:   names,
	}, nil
}
