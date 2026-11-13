package danmaku

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
)

// NATSPublisher menyiarkan danmaku lewat core NATS (bukan JetStream):
// cepat, tidak disimpan, dan tidak menunggu konfirmasi. Padanannya:
// Redis Pub/Sub (PUBLISH) di Node.js, atau SimpMessagingTemplate di Spring.
type NATSPublisher struct {
	nc *nats.Conn
}

// NewNATSPublisher membuat NATSPublisher.
func NewNATSPublisher(nc *nats.Conn) *NATSPublisher {
	return &NATSPublisher{nc: nc}
}

var _ Publisher = (*NATSPublisher)(nil)

// PublishDanmaku mengirim event ke subject danmaku.live.<video_id>. Pesan
// masuk ke buffer koneksi dan dikirim di background; error hanya muncul
// jika koneksi sudah ditutup atau buffer saat reconnect penuh.
func (p *NATSPublisher) PublishDanmaku(_ context.Context, d Danmaku) error {
	data, err := json.Marshal(events.DanmakuSent{
		ID:         d.ID,
		VideoID:    d.VideoID,
		PositionMs: d.PositionMs,
		Body:       d.Body,
		Color:      d.Color,
		Mode:       string(d.Mode),
		CreatedAt:  d.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("encode danmaku: %w", err)
	}
	if err := p.nc.Publish(events.SubjectDanmakuLive(d.VideoID), data); err != nil {
		return fmt.Errorf("publish danmaku: %w", err)
	}
	return nil
}
