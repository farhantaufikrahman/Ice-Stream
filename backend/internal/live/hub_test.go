package live

import (
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBroker menggantikan NATS: publish langsung memanggil handler.
type fakeBroker struct {
	mu       sync.Mutex
	handlers map[string]func([]byte)
	// subscribes menghitung berapa kali Subscribe dipanggil per subject.
	subscribes map[string]int
	err        error
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{handlers: map[string]func([]byte){}, subscribes: map[string]int{}}
}

func (b *fakeBroker) Subscribe(subject string, handler func([]byte)) (func() error, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	b.handlers[subject] = handler
	b.subscribes[subject]++
	return func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.handlers, subject)
		return nil
	}, nil
}

func (b *fakeBroker) publish(subject, msg string) {
	b.mu.Lock()
	h := b.handlers[subject]
	b.mu.Unlock()
	if h != nil {
		h([]byte(msg))
	}
}

func (b *fakeBroker) subscribed(subject string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.handlers[subject]
	return ok
}

func newTestHub(b *fakeBroker, opts Options) *Hub {
	opts.Subject = func(id int64) string { return "danmaku.live." + strconv.FormatInt(id, 10) }
	return NewHub(b, opts, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHubFanOut(t *testing.T) {
	b := newFakeBroker()
	h := newTestHub(b, Options{Transform: func(d []byte) ([]byte, error) { return append([]byte("x:"), d...), nil }})

	a, err := h.Join(42)
	require.NoError(t, err)
	c, err := h.Join(42)
	require.NoError(t, err)
	other, err := h.Join(7)
	require.NoError(t, err)
	assert.Equal(t, 1, b.subscribes["danmaku.live.42"], "satu langganan NATS per video")

	b.publish("danmaku.live.42", "halo")
	assert.Equal(t, "x:halo", string(<-a.Messages()))
	assert.Equal(t, "x:halo", string(<-c.Messages()))
	assert.Empty(t, other.Messages(), "penonton video lain tidak menerima")

	clients, rooms := h.Stats()
	assert.Equal(t, 3, clients)
	assert.Equal(t, 2, rooms)

	// Penonton terakhir pergi: langganan dihentikan.
	h.Leave(a)
	assert.True(t, b.subscribed("danmaku.live.42"))
	h.Leave(c)
	h.Leave(c) // aman dipanggil dua kali
	assert.False(t, b.subscribed("danmaku.live.42"))
	clients, rooms = h.Stats()
	assert.Equal(t, 1, clients)
	assert.Equal(t, 1, rooms)

	// Penonton baru: berlangganan lagi.
	_, err = h.Join(42)
	require.NoError(t, err)
	assert.Equal(t, 2, b.subscribes["danmaku.live.42"])
}

func TestHubSlowClient(t *testing.T) {
	b := newFakeBroker()
	h := newTestHub(b, Options{Buffer: 2})
	slow, _ := h.Join(42)
	for i := 0; i < 5; i++ {
		b.publish("danmaku.live.42", strconv.Itoa(i))
	}
	// Dua pesan pertama masuk antrean, sisanya dibuang tanpa menahan broker.
	assert.Equal(t, "0", string(<-slow.Messages()))
	assert.Equal(t, "1", string(<-slow.Messages()))
	assert.Equal(t, int64(3), slow.Dropped())
}

func TestHubTransformError(t *testing.T) {
	b := newFakeBroker()
	h := newTestHub(b, Options{Transform: func([]byte) ([]byte, error) { return nil, errors.New("json rusak") }})
	c, _ := h.Join(42)
	b.publish("danmaku.live.42", "{")
	assert.Empty(t, c.Messages())
}

func TestHubLimits(t *testing.T) {
	b := newFakeBroker()
	h := newTestHub(b, Options{MaxClients: 2})
	a, err := h.Join(1)
	require.NoError(t, err)
	_, err = h.Join(2)
	require.NoError(t, err)
	_, err = h.Join(3)
	assert.ErrorIs(t, err, ErrFull)
	h.Leave(a)
	_, err = h.Join(3)
	assert.NoError(t, err, "slot terbuka lagi setelah ada yang pergi")

	b.err = errors.New("nats: connection closed")
	h2 := newTestHub(b, Options{})
	_, err = h2.Join(9)
	assert.Error(t, err)
	clients, rooms := h2.Stats()
	assert.Zero(t, clients)
	assert.Zero(t, rooms)
}

func TestHubClose(t *testing.T) {
	b := newFakeBroker()
	h := newTestHub(b, Options{})
	a, _ := h.Join(42)
	c, _ := h.Join(7)

	h.Close()
	h.Close() // aman dipanggil dua kali
	<-a.Done()
	<-c.Done()
	assert.False(t, b.subscribed("danmaku.live.42"))
	assert.False(t, b.subscribed("danmaku.live.7"))
	h.Leave(a) // handler tetap memanggil Leave lewat defer: tidak boleh panic

	_, err := h.Join(42)
	assert.ErrorIs(t, err, ErrClosed)
}

func TestHubConcurrent(t *testing.T) {
	b := newFakeBroker()
	h := newTestHub(b, Options{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := h.Join(int64(i % 3))
			if err != nil {
				return
			}
			b.publish("danmaku.live."+strconv.Itoa(i%3), "m")
			h.Leave(c)
		}(i)
	}
	wg.Wait()
	clients, rooms := h.Stats()
	assert.Zero(t, clients)
	assert.Zero(t, rooms)
}
