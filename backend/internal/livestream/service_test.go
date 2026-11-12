package livestream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepo menyimpan data di memori, pengganti Oracle untuk unit test.
// Logika Sync versi Oracle diuji terpisah ke database sungguhan.
type fakeRepo struct {
	mu       sync.Mutex
	channels map[int64]Channel
	streams  []Stream
	syncs    [][]Online
	chats    []ChatMessage
}

func newFakeRepo() *fakeRepo { return &fakeRepo{channels: map[int64]Channel{}} }

func (r *fakeRepo) GetOrCreateChannel(_ context.Context, ownerID int64, _ time.Time) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.channels[ownerID]
	if !ok {
		ch = Channel{OwnerID: ownerID, Title: DefaultTitle}
		r.channels[ownerID] = ch
	}
	return ch, nil
}

func (r *fakeRepo) FindChannel(_ context.Context, ownerID int64) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.channels[ownerID]
	if !ok {
		return Channel{}, ErrNotFound
	}
	return ch, nil
}

func (r *fakeRepo) UpdateChannel(_ context.Context, ownerID int64, title, category string, _ time.Time) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := r.channels[ownerID]
	ch.Title, ch.Category = title, category
	r.channels[ownerID] = ch
	for i := range r.streams {
		if r.streams[i].OwnerID == ownerID && r.streams[i].Status == StatusLive {
			r.streams[i].Title, r.streams[i].Category = title, category
		}
	}
	return ch, nil
}

func (r *fakeRepo) SetStreamKey(_ context.Context, ownerID int64, hash string, now time.Time) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := r.channels[ownerID]
	ch.StreamKeyHash, ch.StreamKeyCreatedAt = hash, now
	r.channels[ownerID] = ch
	return ch, nil
}

func (r *fakeRepo) CurrentStream(_ context.Context, ownerID int64) (Stream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.streams {
		if st.OwnerID == ownerID && st.Status == StatusLive {
			return st, nil
		}
	}
	return Stream{}, ErrNotFound
}

func (r *fakeRepo) FindStream(_ context.Context, id int64) (Stream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.streams {
		if st.ID == id {
			return st, nil
		}
	}
	return Stream{}, ErrNotFound
}

func (r *fakeRepo) ListLive(_ context.Context, f ListFilter) ([]Stream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Stream
	for _, st := range r.streams {
		if st.Status == StatusLive && (f.Category == "" || st.Category == f.Category) {
			out = append(out, st)
		}
	}
	return out, nil
}

func (r *fakeRepo) Sync(_ context.Context, online []Online, _, _ time.Time) (int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncs = append(r.syncs, online)
	return len(online), 0, nil
}

func (r *fakeRepo) SetPublishToken(_ context.Context, ownerID int64, hash string, expiresAt, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.channels[ownerID]
	if !ok {
		return ErrNotFound
	}
	ch.PublishTokenHash, ch.PublishTokenExpiresAt = hash, expiresAt
	r.channels[ownerID] = ch
	return nil
}

func (r *fakeRepo) CreateChat(_ context.Context, m ChatMessage) (ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m.ID = int64(len(r.chats) + 1)
	r.chats = append(r.chats, m)
	return m, nil
}

func (r *fakeRepo) ListChat(_ context.Context, streamID int64, limit int) ([]ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ChatMessage
	for _, m := range r.chats {
		if m.StreamID == streamID {
			out = append(out, m)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (r *fakeRepo) CountChatSince(_ context.Context, userID int64, since time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, m := range r.chats {
		if m.UserID == userID && m.CreatedAt.After(since) {
			n++
		}
	}
	return n, nil
}

type fakePublisher struct {
	mu   sync.Mutex
	sent []ChatMessage
}

func (p *fakePublisher) PublishChat(_ context.Context, m ChatMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, m)
	return nil
}

func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo, Options{
		IngestURL:       "rtmp://localhost:1935/live",
		PlaybackBaseURL: "http://live.ice-stream.localhost:4000/",
		WHIPBaseURL:     "http://webrtc.ice-stream.localhost:4000/",
	})
	svc.now = func() time.Time { return time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC) }
	return svc, repo
}

func TestGetMyChannelCreatesDefault(t *testing.T) {
	svc, _ := newTestService()
	v, err := svc.GetMyChannel(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, DefaultTitle, v.Title)
	assert.Equal(t, "rtmp://localhost:1935/live", v.IngestURL)
	assert.Empty(t, v.StreamKeyHash, "belum ada stream key")
	assert.Nil(t, v.Current)
}

func TestUpdateMyChannel(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()
	repo.streams = []Stream{{ID: 1, OwnerID: 7, Title: DefaultTitle, Status: StatusLive}}

	v, err := svc.UpdateMyChannel(ctx, 7, "  Ngoding   bareng  ", "Teknologi")
	require.NoError(t, err)
	assert.Equal(t, "Ngoding bareng", v.Title, "spasi berlebih dirapikan")
	assert.Equal(t, "teknologi", v.Category)
	require.NotNil(t, v.Current)
	assert.Equal(t, "Ngoding bareng", v.Current.Title, "siaran yang sedang live ikut berubah")

	tests := map[string]struct{ title, category, field string }{
		"judul kosong":         {"   ", "", "title"},
		"judul kepanjangan":    {strings.Repeat("é", 101), "", "title"},
		"kategori tak dikenal": {"Halo", "politik", "category"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc.UpdateMyChannel(ctx, 7, tt.title, tt.category)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tt.field, verr.Field)
		})
	}
	_, err = svc.UpdateMyChannel(ctx, 7, strings.Repeat("é", 100), "")
	assert.NoError(t, err, "100 karakter multibyte masih boleh")
}

func TestStreamKeyAuthorizesPublish(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	_, err := svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Query: "key=apa-saja"})
	assert.ErrorIs(t, err, ErrForbidden, "belum punya channel")

	v, key, err := svc.ResetStreamKey(ctx, 7)
	require.NoError(t, err)
	assert.NotEmpty(t, v.StreamKeyHash)
	require.True(t, strings.HasPrefix(key, "7?key=isk_"), key)
	secret := strings.TrimPrefix(key, "7?")
	assert.NotContains(t, repo.channels[7].StreamKeyHash, "isk_", "yang disimpan hanya hash")

	owner, err := svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Query: secret})
	require.NoError(t, err)
	assert.Equal(t, int64(7), owner)

	for name, tc := range map[string][2]string{
		"key salah":           {"live/7", "key=isk_salah"},
		"tanpa key":           {"live/7", ""},
		"key milik user lain": {"live/8", secret},
		"path lain":           {"rahasia/7", secret},
		"path dengan nol":     {"live/07", secret},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.AuthorizePublish(ctx, PublishRequest{Path: tc[0], Query: tc[1]})
			assert.ErrorIs(t, err, ErrForbidden)
		})
	}

	_, _, err = svc.ResetStreamKey(ctx, 7)
	require.NoError(t, err)
	_, err = svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Query: secret})
	assert.ErrorIs(t, err, ErrForbidden, "key lama mati setelah dibuat ulang")
}

func TestPublishTokenAuthorizesBrowser(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	now := svc.now()

	tok, err := svc.CreatePublishToken(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "http://webrtc.ice-stream.localhost:4000/live/7/whip", tok.WHIPURL)
	require.True(t, strings.HasPrefix(tok.Token, "ipt_"), tok.Token)
	assert.Equal(t, now.Add(5*time.Minute), tok.ExpiresAt)

	owner, err := svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Token: tok.Token})
	require.NoError(t, err)
	assert.Equal(t, int64(7), owner)

	for name, req := range map[string]PublishRequest{
		"token salah":           {Path: "live/7", Token: "ipt_salah"},
		"token milik user lain": {Path: "live/8", Token: tok.Token},
		"token di query":        {Path: "live/7", Query: "key=" + tok.Token},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.AuthorizePublish(ctx, req)
			assert.ErrorIs(t, err, ErrForbidden)
		})
	}

	svc.now = func() time.Time { return now.Add(5 * time.Minute) }
	_, err = svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Token: tok.Token})
	assert.ErrorIs(t, err, ErrForbidden, "token kedaluwarsa")

	svc.now = func() time.Time { return now }
	tok2, err := svc.CreatePublishToken(ctx, 7)
	require.NoError(t, err)
	_, err = svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Token: tok.Token})
	assert.ErrorIs(t, err, ErrForbidden, "token lama mati setelah token baru dibuat")
	_, err = svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Token: tok2.Token})
	assert.NoError(t, err)
}

func TestStreamKeyIsNotAPublishToken(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	_, key, err := svc.ResetStreamKey(ctx, 7)
	require.NoError(t, err)
	secret := strings.TrimPrefix(key, "7?key=")
	// Stream key sebagai bearer token juga diterima: rahasianya sama-sama
	// milik kreator, dan awalannya menentukan hash mana yang dicek.
	_, err = svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Token: secret})
	assert.NoError(t, err)
	// Tapi hash stream key tidak pernah dicocokkan dengan awalan "ipt_".
	_, err = svc.AuthorizePublish(ctx, PublishRequest{Path: "live/7", Token: "ipt_" + strings.TrimPrefix(secret, "isk_")})
	assert.ErrorIs(t, err, ErrForbidden)
}

func TestSendChat(t *testing.T) {
	svc, repo := newTestService()
	pub := &fakePublisher{}
	svc.opts.Publisher = pub
	ctx := context.Background()
	repo.streams = []Stream{
		{ID: 1, OwnerID: 7, Status: StatusLive},
		{ID: 2, OwnerID: 8, Status: StatusEnded},
	}

	m, err := svc.SendChat(ctx, SendChatInput{RequesterID: 9, StreamID: 1, Body: "  halo kak  "})
	require.NoError(t, err)
	assert.Equal(t, "halo kak", m.Body)
	assert.Equal(t, ColorWhite, m.Color)
	assert.Equal(t, svc.now(), m.CreatedAt)
	require.Len(t, pub.sent, 1, "pesan disiarkan ke penonton lain")

	_, err = svc.SendChat(ctx, SendChatInput{RequesterID: 9, StreamID: 1, Body: "lagi"})
	assert.ErrorIs(t, err, ErrRateLimited, "mode lambat 3 detik")

	start := svc.now()
	svc.now = func() time.Time { return start.Add(3 * time.Second) }
	pink := uint32(0xFF6B8B)
	m, err = svc.SendChat(ctx, SendChatInput{RequesterID: 9, StreamID: 1, Body: "lagi", Color: &pink})
	require.NoError(t, err, "boleh lagi setelah 3 detik")
	assert.Equal(t, pink, m.Color)

	_, err = svc.SendChat(ctx, SendChatInput{RequesterID: 10, StreamID: 2, Body: "halo"})
	assert.ErrorIs(t, err, ErrStreamEnded)
	_, err = svc.SendChat(ctx, SendChatInput{RequesterID: 10, StreamID: 99, Body: "halo"})
	assert.ErrorIs(t, err, ErrNotFound)

	bad := uint32(0x1000000)
	for name, in := range map[string]SendChatInput{
		"kosong":     {RequesterID: 10, StreamID: 1, Body: "  "},
		"baris baru": {RequesterID: 10, StreamID: 1, Body: "a\nb"},
		"warna":      {RequesterID: 10, StreamID: 1, Body: "a", Color: &bad},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.SendChat(ctx, in)
			var verr *ValidationError
			assert.ErrorAs(t, err, &verr)
		})
	}

	list, err := svc.ListChat(ctx, 1, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "halo kak", list[0].Body, "urut dari yang paling lama")
	_, err = svc.ListChat(ctx, 1, 101)
	assert.Error(t, err)
	_, err = svc.ListChat(ctx, 99, 0)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestOwnerFromPath(t *testing.T) {
	for path, want := range map[string]int64{"live/7": 7, "live/22522": 22522, "live/": 0, "live/abc": 0, "live/-1": 0, "live/7/x": 0, "video/7": 0} {
		got, ok := OwnerFromPath(path)
		assert.Equal(t, want, got, path)
		assert.Equal(t, want != 0, ok, path)
	}
	assert.Equal(t, "live/42", PathFor(42))
}

func TestListLiveAndPlaybackURL(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()
	repo.streams = []Stream{
		{ID: 1, OwnerID: 7, Status: StatusLive, Category: "game"},
		{ID: 2, OwnerID: 8, Status: StatusEnded},
	}
	got, err := svc.ListLive(ctx, ListFilter{Category: "GAME"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "http://live.ice-stream.localhost:4000/live/7/index.m3u8", svc.PlaybackURL(got[0]))
	assert.Empty(t, svc.PlaybackURL(repo.streams[1]), "siaran selesai tidak punya URL putar")

	_, err = svc.ListLive(ctx, ListFilter{Limit: 51})
	assert.Error(t, err)
	_, err = svc.ListLive(ctx, ListFilter{Category: "politik"})
	assert.Error(t, err)
	_, err = svc.ListLive(ctx, ListFilter{Query: strings.Repeat("a", 101)})
	assert.Error(t, err)

	_, err = svc.GetStream(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestHookHandler(t *testing.T) {
	svc, _ := newTestService()
	_, key, err := svc.ResetStreamKey(context.Background(), 7)
	require.NoError(t, err)
	h := HookHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	post := func(body string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mediamtx/auth", strings.NewReader(body)))
		return rec.Code
	}
	query := strings.TrimPrefix(key, "7?")
	assert.Equal(t, http.StatusOK, post(`{"action":"publish","path":"live/7","protocol":"rtmp","query":"`+query+`"}`))
	assert.Equal(t, http.StatusUnauthorized, post(`{"action":"publish","path":"live/7","query":"key=salah"}`))
	assert.Equal(t, http.StatusForbidden, post(`{"action":"api","path":""}`), "selain publish ditolak")
	tok, err := svc.CreatePublishToken(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, post(`{"action":"publish","path":"live/7","protocol":"webrtc","token":"`+tok.Token+`"}`), "siaran browser lewat WHIP")
	assert.Equal(t, http.StatusUnauthorized, post(`{"action":"publish","path":"live/7","protocol":"webrtc","token":"ipt_salah"}`))
	assert.Equal(t, http.StatusBadRequest, post(`{`))
}

func TestMediaMTXClientOnline(t *testing.T) {
	pages := map[string]string{
		"0": `{"pageCount":2,"items":[
			{"name":"live/7","ready":true,"readyTime":"2026-11-12T07:27:32Z","readers":[{"type":"hlsSession"},{"type":"hlsSession"}]},
			{"name":"live/8","ready":false,"readers":[]}]}`,
		"1": `{"pageCount":2,"items":[{"name":"lain/9","ready":true,"readers":[]},{"name":"live/10","ready":true,"readyTime":"2026-11-12T07:30:00Z","readers":null}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v3/paths/list", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("itemsPerPage"))
		_, _ = io.WriteString(w, pages[r.URL.Query().Get("page")])
	}))
	defer srv.Close()

	got, err := MediaMTXClient{BaseURL: srv.URL}.Online(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []Online{
		{OwnerID: 7, ReadySince: time.Date(2026, 11, 12, 7, 27, 32, 0, time.UTC), Viewers: 2},
		{OwnerID: 10, ReadySince: time.Date(2026, 11, 12, 7, 30, 0, 0, time.UTC), Viewers: 0},
	}, got, "hanya live/<id> yang ready")
}

type fakeSource struct {
	online []Online
	err    error
}

func (f fakeSource) Online(context.Context) ([]Online, error) { return f.online, f.err }

func TestSyncerSkipsWhenMediaMTXDown(t *testing.T) {
	svc, repo := newTestService()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := Syncer{Source: fakeSource{err: errors.New("connection refused")}, Service: svc, Interval: time.Second, Logger: logger}
	assert.Error(t, s.tick(context.Background()))
	assert.Empty(t, repo.syncs, "MediaMTX mati: status siaran tidak diubah")

	s.Source = fakeSource{online: []Online{{OwnerID: 7}}}
	require.NoError(t, s.tick(context.Background()))
	require.Len(t, repo.syncs, 1)
	assert.Equal(t, int64(7), repo.syncs[0][0].OwnerID)
}
