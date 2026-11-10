package live

import "github.com/nats-io/nats.go"

// NATSBroker menerima pesan dari core NATS.
type NATSBroker struct {
	nc *nats.Conn
}

// NewNATSBroker membuat NATSBroker dari koneksi yang sudah dibuka.
func NewNATSBroker(nc *nats.Conn) *NATSBroker {
	return &NATSBroker{nc: nc}
}

var _ Broker = (*NATSBroker)(nil)

// Subscribe berlangganan subject. Setiap langganan punya goroutine sendiri
// di nats.go, jadi pesan satu video tidak menunggu pesan video lain.
// Langganan yang dibuat saat NATS sedang terputus tetap berlaku: nats.go
// mendaftarkannya ulang begitu tersambung lagi.
func (b *NATSBroker) Subscribe(subject string, handler func([]byte)) (func() error, error) {
	sub, err := b.nc.Subscribe(subject, func(m *nats.Msg) { handler(m.Data) })
	if err != nil {
		return nil, err
	}
	return sub.Unsubscribe, nil
}
