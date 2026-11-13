package livestream

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
)

// NATSPublisher menyiarkan chat lewat core NATS (bukan JetStream), sama
// seperti danmaku: cepat, tidak disimpan ulang, tidak menunggu konfirmasi.
// Pesan yang sudah lewat tetap bisa dibaca dari Oracle (ListChatMessages).
type NATSPublisher struct {
	nc *nats.Conn
}

// NewNATSPublisher membuat NATSPublisher.
func NewNATSPublisher(nc *nats.Conn) *NATSPublisher {
	return &NATSPublisher{nc: nc}
}

var _ ChatPublisher = (*NATSPublisher)(nil)

// PublishChat mengirim event ke subject live.chat.<stream_id>.
func (p *NATSPublisher) PublishChat(_ context.Context, m ChatMessage) error {
	data, err := json.Marshal(events.LiveChatSent{
		ID:        m.ID,
		StreamID:  m.StreamID,
		UserID:    m.UserID,
		Body:      m.Body,
		Color:     m.Color,
		CreatedAt: m.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("encode chat: %w", err)
	}
	if err := p.nc.Publish(events.SubjectLiveChat(m.StreamID), data); err != nil {
		return fmt.Errorf("publish chat: %w", err)
	}
	return nil
}
