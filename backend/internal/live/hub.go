// Package live membagikan pesan real-time dari NATS ke banyak penonton yang
// tersambung ke satu pod API gateway (dipakai untuk danmaku).
//
// Alurnya:
//
//	VideoService --NATS danmaku.live.42--> Hub (di setiap pod gateway)
//	                                        |-> penonton A (SSE)
//	                                        |-> penonton B (SSE)
//
// Hub hanya berlangganan (subscribe) subject video yang sedang ditonton di
// pod ini. Penonton pertama sebuah video membuat langganan baru, penonton
// terakhir yang pergi menghapusnya. Jadi kalau ada 1000 orang menonton video
// yang sama, pod tetap hanya punya satu langganan NATS untuk video itu.
//
// Padanannya: Socket.IO "rooms" + Redis adapter di Node.js, atau
// SimpleBroker/STOMP relay di Spring WebSocket (Java).
package live

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
)

var (
	// ErrFull: jumlah penonton di pod ini sudah mencapai batas.
	ErrFull = errors.New("terlalu banyak penonton tersambung")
	// ErrClosed: Hub sudah ditutup (pod sedang berhenti).
	ErrClosed = errors.New("hub sudah ditutup")
)

// Broker adalah sumber pesan, yaitu NATS (lihat nats.go). Test memakai
// versi palsu.
type Broker interface {
	// Subscribe mulai menerima pesan dari subject. handler dipanggil dari
	// goroutine milik broker. Fungsi yang dikembalikan menghentikan langganan.
	Subscribe(subject string, handler func(data []byte)) (unsubscribe func() error, err error)
}

// Options mengatur Hub.
type Options struct {
	// Subject mengubah id video menjadi subject NATS.
	Subject func(videoID int64) string
	// Transform mengubah pesan NATS menjadi data yang dikirim ke browser.
	// Dipanggil sekali per pesan (bukan per penonton). Jika error, pesan
	// dibuang.
	Transform func(data []byte) ([]byte, error)
	// MaxClients adalah batas penonton tersambung di satu pod.
	MaxClients int
	// Buffer adalah jumlah pesan yang boleh mengantre per penonton. Jika
	// penonton terlalu lambat dan antreannya penuh, pesan baru untuknya
	// dibuang (danmaku yang terlambat tidak ada gunanya).
	Buffer int
}

// Hub menyimpan daftar penonton per video.
type Hub struct {
	broker Broker
	opts   Options
	logger *slog.Logger

	mu      sync.Mutex
	rooms   map[int64]*room
	clients int
	closed  bool
}

// room berisi penonton satu video di pod ini.
type room struct {
	clients     map[*Client]struct{}
	unsubscribe func() error
}

// Client adalah satu penonton yang tersambung.
type Client struct {
	videoID  int64
	messages chan []byte
	done     chan struct{}
	dropped  atomic.Int64
}

// Messages mengirim pesan untuk penonton ini.
func (c *Client) Messages() <-chan []byte { return c.messages }

// Done tertutup saat Hub ditutup. Handler harus berhenti mengirim dan
// mengakhiri response.
func (c *Client) Done() <-chan struct{} { return c.done }

// Dropped adalah jumlah pesan yang dibuang karena penonton terlalu lambat.
func (c *Client) Dropped() int64 { return c.dropped.Load() }

// NewHub membuat Hub.
func NewHub(broker Broker, opts Options, logger *slog.Logger) *Hub {
	if opts.Buffer <= 0 {
		opts.Buffer = 64
	}
	if opts.Transform == nil {
		opts.Transform = func(b []byte) ([]byte, error) { return b, nil }
	}
	return &Hub{broker: broker, opts: opts, logger: logger, rooms: make(map[int64]*room)}
}

// Join mendaftarkan penonton baru untuk videoID. Panggil Leave setelah
// selesai (biasanya dengan defer).
func (h *Hub) Join(videoID int64) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	if h.opts.MaxClients > 0 && h.clients >= h.opts.MaxClients {
		return nil, ErrFull
	}

	r, ok := h.rooms[videoID]
	if !ok {
		r = &room{clients: make(map[*Client]struct{})}
		unsub, err := h.broker.Subscribe(h.opts.Subject(videoID), func(data []byte) {
			h.dispatch(r, data)
		})
		if err != nil {
			return nil, err
		}
		r.unsubscribe = unsub
		h.rooms[videoID] = r
	}

	c := &Client{videoID: videoID, messages: make(chan []byte, h.opts.Buffer), done: make(chan struct{})}
	r.clients[c] = struct{}{}
	h.clients++
	return c, nil
}

// Leave menghapus penonton. Jika ia penonton terakhir video itu, langganan
// NATS-nya dihentikan. Aman dipanggil lebih dari sekali, juga setelah Close.
func (h *Hub) Leave(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[c.videoID]
	if !ok {
		return
	}
	if _, ok := r.clients[c]; !ok {
		return
	}
	delete(r.clients, c)
	h.clients--
	if len(r.clients) == 0 {
		delete(h.rooms, c.videoID)
		h.unsubscribe(r)
	}
}

// dispatch membagikan satu pesan ke semua penonton di room. Pengiriman ke
// channel tidak pernah menunggu: satu penonton yang lambat tidak boleh
// menahan penonton lain.
func (h *Hub) dispatch(r *room, data []byte) {
	out, err := h.opts.Transform(data)
	if err != nil {
		h.logger.Warn("pesan live dibuang", slog.Any("error", err))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range r.clients {
		select {
		case c.messages <- out:
		default:
			c.dropped.Add(1)
		}
	}
}

// Stats mengembalikan jumlah penonton dan jumlah video yang sedang
// ditonton di pod ini.
func (h *Hub) Stats() (clients, rooms int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clients, len(h.rooms)
}

// Close memutus semua penonton dan menghentikan semua langganan. Dipanggil
// saat pod berhenti: koneksi SSE tidak pernah selesai sendiri, jadi tanpa
// ini http.Server.Shutdown akan menunggu sampai batas waktunya habis.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, r := range h.rooms {
		for c := range r.clients {
			close(c.done)
		}
		h.unsubscribe(r)
		delete(h.rooms, id)
	}
	h.clients = 0
}

func (h *Hub) unsubscribe(r *room) {
	if err := r.unsubscribe(); err != nil {
		h.logger.Warn("gagal berhenti berlangganan NATS", slog.Any("error", err))
	}
}
