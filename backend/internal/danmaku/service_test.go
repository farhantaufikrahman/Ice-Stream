package danmaku

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo menyimpan danmaku di memori, menggantikan Oracle di test.
type fakeRepo struct {
	mu sync.Mutex
	// videos: id video READY -> durasi (ms).
	videos map[int64]int64
	items  []Danmaku
	nextID int64
	now    func() time.Time
	err    error
}

func (f *fakeRepo) VideoDuration(_ context.Context, videoID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.videos[videoID]
	if !ok {
		return 0, ErrVideoNotFound
	}
	return d, nil
}

func (f *fakeRepo) CountSince(_ context.Context, userID int64, since time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, d := range f.items {
		if d.UserID == userID && d.CreatedAt.After(since) {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) Create(_ context.Context, d Danmaku) (Danmaku, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return Danmaku{}, f.err
	}
	f.nextID++
	d.ID = f.nextID
	d.CreatedAt = f.now()
	f.items = append(f.items, d)
	return d, nil
}

func (f *fakeRepo) List(_ context.Context, videoID, fromMs, toMs int64, after Cursor, limit int) ([]Danmaku, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Danmaku
	for _, d := range f.items {
		if d.VideoID != videoID || d.PositionMs < fromMs || d.PositionMs > toMs {
			continue
		}
		if !after.IsZero() && (d.PositionMs < after.PositionMs || (d.PositionMs == after.PositionMs && d.ID <= after.ID)) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PositionMs != out[j].PositionMs {
			return out[i].PositionMs < out[j].PositionMs
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// fakePublisher mencatat danmaku yang disiarkan.
type fakePublisher struct {
	sent []Danmaku
	err  error
}

func (p *fakePublisher) PublishDanmaku(_ context.Context, d Danmaku) error {
	if p.err != nil {
		return p.err
	}
	p.sent = append(p.sent, d)
	return nil
}

// newTestService: video 42 READY berdurasi 60 detik. Jam bisa dimajukan
// dengan *clock = clock.Add(...).
func newTestService() (*Service, *fakeRepo, *fakePublisher, *time.Time) {
	clock := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	repo := &fakeRepo{videos: map[int64]int64{42: 60_000}, now: now}
	pub := &fakePublisher{}
	svc := NewService(repo, pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.now = now
	return svc, repo, pub, &clock
}

func ptr[T any](v T) *T { return &v }

func TestSend(t *testing.T) {
	ctx := context.Background()
	svc, _, pub, _ := newTestService()

	d, err := svc.Send(ctx, SendInput{RequesterID: 7, VideoID: 42, PositionMs: 1500, Body: "  wkwkwk  "})
	require.NoError(t, err)
	assert.Equal(t, int64(1), d.ID)
	assert.Equal(t, "wkwkwk", d.Body, "spasi di awal/akhir dibuang")
	assert.Equal(t, ColorWhite, d.Color, "default putih")
	assert.Equal(t, ModeScroll, d.Mode, "default SCROLL")
	require.Len(t, pub.sent, 1, "langsung disiarkan")
	assert.Equal(t, d, pub.sent[0])

	// Hitam (0) harus bisa dipilih, berbeda dengan "tidak dikirim".
	d, err = svc.Send(ctx, SendInput{RequesterID: 8, VideoID: 42, PositionMs: 60_500, Body: "akhir", Color: ptr(uint32(0)), Mode: ModeBottom})
	require.NoError(t, err)
	assert.Equal(t, uint32(0), d.Color)
	assert.Equal(t, ModeBottom, d.Mode)
}

func TestSendValidation(t *testing.T) {
	svc, _, pub, _ := newTestService()
	var verr *ValidationError
	cases := map[string]SendInput{
		"kosong":          {Body: "   "},
		"terlalu panjang": {Body: strings.Repeat("a", 101)},
		"baris baru":      {Body: "satu\ndua"},
		"tab":             {Body: "satu\tdua"},
		"pembalik arah":   {Body: "abc‮def"},
		"posisi negatif":  {Body: "ok", PositionMs: -1},
		"lewat durasi":    {Body: "ok", PositionMs: 61_001},
		"warna":           {Body: "ok", Color: ptr(uint32(0x1000000))},
		"mode":            {Body: "ok", Mode: "MIRING"},
	}
	for name, in := range cases {
		in.RequesterID, in.VideoID = 7, 42
		_, err := svc.Send(context.Background(), in)
		assert.ErrorAs(t, err, &verr, name)
	}
	assert.Empty(t, pub.sent)

	// 100 emoji (bukan 100 byte) masih boleh.
	_, err := svc.Send(context.Background(), SendInput{RequesterID: 7, VideoID: 42, Body: strings.Repeat("😂", 100)})
	assert.NoError(t, err)

	_, err = svc.Send(context.Background(), SendInput{RequesterID: 7, VideoID: 99, Body: "ok"})
	assert.ErrorIs(t, err, ErrVideoNotFound)
	_, err = svc.Send(context.Background(), SendInput{RequesterID: 0, VideoID: 42, Body: "ok"})
	assert.Error(t, err)
}

func TestSendRateLimit(t *testing.T) {
	ctx := context.Background()
	svc, _, _, clock := newTestService()
	send := func(user int64) error {
		_, err := svc.Send(ctx, SendInput{RequesterID: user, VideoID: 42, Body: "spam"})
		return err
	}

	for i := 0; i < rateLimitCount; i++ {
		require.NoError(t, send(7))
		*clock = clock.Add(time.Second)
	}
	assert.ErrorIs(t, send(7), ErrRateLimited, "danmaku ke-4 dalam 10 detik")
	assert.NoError(t, send(8), "user lain tidak ikut kena")

	// Jam sekarang 3 detik. Pada detik ke-10, danmaku pertama (detik 0)
	// sudah keluar dari jendela 10 detik, jadi satu slot terbuka lagi.
	*clock = clock.Add(7 * time.Second)
	assert.NoError(t, send(7))
	assert.ErrorIs(t, send(7), ErrRateLimited)
}

func TestSendPublishFailure(t *testing.T) {
	svc, repo, pub, _ := newTestService()
	pub.err = errors.New("nats: connection closed")

	// NATS mati: danmaku tetap tersimpan dan Send tetap berhasil.
	d, err := svc.Send(context.Background(), SendInput{RequesterID: 7, VideoID: 42, Body: "halo"})
	require.NoError(t, err)
	assert.Len(t, repo.items, 1)
	assert.Equal(t, int64(1), d.ID)

	// Oracle gagal: error diteruskan, tidak ada yang disiarkan.
	pub.err = nil
	repo.err = errors.New("ORA-03113")
	_, err = svc.Send(context.Background(), SendInput{RequesterID: 8, VideoID: 42, Body: "halo"})
	assert.Error(t, err)
	assert.Empty(t, pub.sent)
}

func TestList(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newTestService()
	// Posisi: id1=5000, id2=1000, id3=5000, id4=9000, id5=400000 (di luar rentang).
	for _, pos := range []int64{5000, 1000, 5000, 9000, 400_000} {
		repo.videos[42] = 500_000
		_, err := repo.Create(ctx, Danmaku{VideoID: 42, UserID: 7, PositionMs: pos, Body: "x", Mode: ModeScroll})
		require.NoError(t, err)
	}

	page1, next, err := svc.List(ctx, 42, 0, 360_000, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{2, 1}, ids(page1), "urut posisi, lalu id")
	require.NotEmpty(t, next)

	page2, next, err := svc.List(ctx, 42, 0, 360_000, 2, next)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 4}, ids(page2), "posisi sama (5000) tetap lanjut dengan benar")
	assert.Empty(t, next)

	all, _, err := svc.List(ctx, 42, 0, 360_000, 0, "")
	require.NoError(t, err)
	assert.Len(t, all, 4, "page_size 0 = default")

	later, _, err := svc.List(ctx, 42, 360_000, 720_000, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{5}, ids(later))
}

func TestListValidation(t *testing.T) {
	svc, _, _, _ := newTestService()
	var verr *ValidationError
	cases := []struct {
		from, to int64
		size     int
		token    string
	}{
		{-1, 10, 0, ""},
		{10, 5, 0, ""},
		{0, 360_001, 0, ""},
		{0, 10, 1001, ""},
		{0, 10, 0, "!!!"},
		{0, 10, 0, encodeRaw("42")},
		{0, 10, 0, encodeRaw("10.0")},
		{0, 10, 0, encodeRaw("-1.5")},
	}
	for _, c := range cases {
		_, _, err := svc.List(context.Background(), 42, c.from, c.to, c.size, c.token)
		assert.ErrorAs(t, err, &verr, "%+v", c)
	}
	_, _, err := svc.List(context.Background(), 99, 0, 10, 0, "")
	assert.ErrorIs(t, err, ErrVideoNotFound)
}

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{PositionMs: 15000, ID: 42}
	got, err := decodeCursor(encodeCursor(c))
	require.NoError(t, err)
	assert.Equal(t, c, got)
}

func ids(items []Danmaku) []int64 {
	out := make([]int64, len(items))
	for i, d := range items {
		out[i] = d.ID
	}
	return out
}

func encodeRaw(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
