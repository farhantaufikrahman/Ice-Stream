package video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
)

// NATSPublisher mengirim event VideoService ke NATS JetStream.
// Padanannya: KafkaTemplate di Spring (Java), producer kafkajs di Node.js.
type NATSPublisher struct {
	js jetstream.JetStream
}

// NewNATSPublisher membuat NATSPublisher.
func NewNATSPublisher(js jetstream.JetStream) *NATSPublisher {
	return &NATSPublisher{js: js}
}

var _ Publisher = (*NATSPublisher)(nil)

// PublishUploaded mengirim event videos.uploaded. Msg-Id-nya selalu sama
// untuk video yang sama, jadi kalau terkirim dua kali, JetStream hanya
// menyimpan satu.
func (p *NATSPublisher) PublishUploaded(ctx context.Context, v Video) error {
	return events.Publish(ctx, p.js, events.SubjectUploaded, events.MsgID(events.SubjectUploaded, v.ID), events.Uploaded{
		VideoID:     v.ID,
		ObjectKey:   v.ObjectKey,
		ContentType: v.ContentType,
	})
}

// ConsumeTranscodeEvents membaca event videos.transcode.* dari transcoder
// dan memperbarui status video. Berjalan di background sampai Stop()
// dipanggil pada ConsumeContext yang dikembalikan.
func ConsumeTranscodeEvents(ctx context.Context, js jetstream.JetStream, svc *Service, logger *slog.Logger) (jetstream.ConsumeContext, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, events.StreamName, jetstream.ConsumerConfig{
		Durable:       events.ConsumerVideoStatus,
		FilterSubject: events.SubjectTranscodeAll,
		// Setiap pesan harus di-Ack. Kalau tidak, dikirim ulang setelah AckWait.
		AckPolicy: jetstream.AckExplicitPolicy,
		AckWait:   30 * time.Second,
		// Update status cepat dan idempotent, jadi boleh dicoba sampai 20 kali
		// (misalnya saat Oracle sedang restart).
		MaxDeliver: 20,
	})
	if err != nil {
		return nil, fmt.Errorf("membuat consumer %s: %w", events.ConsumerVideoStatus, err)
	}
	return cons.Consume(func(msg jetstream.Msg) {
		handleTranscodeEvent(ctx, svc, msg, logger)
	})
}

func handleTranscodeEvent(ctx context.Context, svc *Service, msg jetstream.Msg, logger *slog.Logger) {
	err := applyTranscodeEvent(ctx, svc, msg.Subject(), msg.Data())
	log := logger.With(slog.String("subject", msg.Subject()))

	var verr *ValidationError
	switch {
	case err == nil:
		log.Info("event transcode diproses")
		_ = msg.Ack()
	case errors.Is(err, ErrNotFound), errors.As(err, &verr), errors.Is(err, errBadEvent):
		// Mencoba ulang tidak akan membantu: buang pesannya.
		log.Warn("event transcode dibuang", slog.Any("error", err))
		_ = msg.TermWithReason(err.Error())
	default:
		// Misalnya Oracle sedang tidak bisa dihubungi: coba lagi nanti.
		log.Error("event transcode gagal, dicoba lagi", slog.Any("error", err))
		_ = msg.NakWithDelay(5 * time.Second)
	}
}

// errBadEvent menandai pesan yang isinya tidak bisa dibaca.
var errBadEvent = errors.New("event tidak valid")

func decode(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %w", errBadEvent, err)
	}
	return nil
}

// applyTranscodeEvent memilih aksi berdasarkan subject pesan.
func applyTranscodeEvent(ctx context.Context, svc *Service, subject string, data []byte) error {
	switch subject {
	case events.SubjectTranscodeStarted:
		var e events.TranscodeStarted
		if err := decode(data, &e); err != nil {
			return err
		}
		return svc.MarkProcessing(ctx, e.VideoID)
	case events.SubjectTranscodeSucceeded:
		var e events.TranscodeSucceeded
		if err := decode(data, &e); err != nil {
			return err
		}
		return svc.MarkReady(ctx, e.VideoID, TranscodeResult{
			PlaylistKey:  e.PlaylistKey,
			ThumbnailKey: e.ThumbnailKey,
			DurationMs:   e.DurationMs,
			Renditions:   e.Renditions,
		})
	case events.SubjectTranscodeFailed:
		var e events.TranscodeFailed
		if err := decode(data, &e); err != nil {
			return err
		}
		return svc.MarkFailed(ctx, e.VideoID, e.Reason)
	}
	return fmt.Errorf("%w: subject %s tidak dikenal", errBadEvent, subject)
}