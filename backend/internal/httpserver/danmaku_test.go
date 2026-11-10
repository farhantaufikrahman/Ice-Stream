package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
)

// fakeDanmakuClient menggantikan DanmakuService sungguhan di test.
type fakeDanmakuClient struct {
	err      error
	lastSend *danmakuv1.SendDanmakuRequest
	lastList *danmakuv1.ListDanmakuRequest
}

var _ danmakuv1.DanmakuServiceClient = (*fakeDanmakuClient)(nil)

func testDanmaku(id int64) *danmakuv1.Danmaku {
	return &danmakuv1.Danmaku{
		Id: id, VideoId: 42, UserId: 7, PositionMs: 15300, Body: "wkwk",
		Color: 0xFFFFFF, Mode: danmakuv1.DanmakuMode_DANMAKU_MODE_SCROLL,
		CreatedAt: timestamppb.New(testCreatedAt),
	}
}

func (f *fakeDanmakuClient) SendDanmaku(_ context.Context, in *danmakuv1.SendDanmakuRequest, _ ...grpc.CallOption) (*danmakuv1.SendDanmakuResponse, error) {
	f.lastSend = in
	if f.err != nil {
		return nil, f.err
	}
	d := testDanmaku(15)
	d.Body, d.PositionMs = in.GetBody(), in.GetPositionMs()
	if in.Color != nil {
		d.Color = in.GetColor()
	}
	if in.GetMode() != danmakuv1.DanmakuMode_DANMAKU_MODE_UNSPECIFIED {
		d.Mode = in.GetMode()
	}
	return &danmakuv1.SendDanmakuResponse{Danmaku: d}, nil
}

func (f *fakeDanmakuClient) ListDanmaku(_ context.Context, in *danmakuv1.ListDanmakuRequest, _ ...grpc.CallOption) (*danmakuv1.ListDanmakuResponse, error) {
	f.lastList = in
	if f.err != nil {
		return nil, f.err
	}
	top := testDanmaku(16)
	top.Color, top.Mode = 0xFF0000, danmakuv1.DanmakuMode_DANMAKU_MODE_TOP
	return &danmakuv1.ListDanmakuResponse{Danmaku: []*danmakuv1.Danmaku{testDanmaku(15), top}, NextPageToken: "MTUzMDAuMTY"}, nil
}

func TestSendDanmaku(t *testing.T) {
	verifier, asUser := newTokenSigner(t)
	dm := &fakeDanmakuClient{}
	r := newTestRouter(t, Deps{Danmaku: dm, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos/42/danmaku", `{"body":"wkwk","position_ms":15300}`, asUser(8))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"danmaku":{"id":"15","video_id":"42","position_ms":15300,"body":"wkwk",
		"color":"#FFFFFF","mode":"SCROLL","created_at":"2026-10-29T04:00:00Z"}}`, rec.Body.String())
	assert.Equal(t, int64(8), dm.lastSend.GetRequesterId(), "pengirim dari token")
	assert.Equal(t, int64(42), dm.lastSend.GetVideoId())
	assert.Nil(t, dm.lastSend.Color, "warna tidak dikirim = biarkan service memakai default")
	assert.NotContains(t, rec.Body.String(), "user_id", "pengirim tidak ditampilkan")

	// Warna dan mode (huruf kecil juga diterima).
	rec = do(r, http.MethodPost, "/api/v1/videos/42/danmaku", `{"body":"hitam","position_ms":0,"color":"#000000","mode":"bottom"}`, asUser(8))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, uint32(0), dm.lastSend.GetColor())
	require.NotNil(t, dm.lastSend.Color)
	assert.Equal(t, danmakuv1.DanmakuMode_DANMAKU_MODE_BOTTOM, dm.lastSend.GetMode())
	assert.Contains(t, rec.Body.String(), `"color":"#000000"`)
	assert.Contains(t, rec.Body.String(), `"mode":"BOTTOM"`)
}

func TestSendDanmakuErrors(t *testing.T) {
	verifier, asUser := newTokenSigner(t)
	dm := &fakeDanmakuClient{}
	r := newTestRouter(t, Deps{Danmaku: dm, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos/42/danmaku", `{"body":"x","position_ms":1}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")

	for _, body := range []string{
		`{"body":"x"}`,
		`{"body":"x","position_ms":1,"color":"merah"}`,
		`{"body":"x","position_ms":1,"color":"#GGGGGG"}`,
		`{"body":"x","position_ms":1,"color":"#FFF"}`,
		`{"body":"x","position_ms":1,"mode":"MIRING"}`,
	} {
		dm.lastSend = nil
		rec = do(r, http.MethodPost, "/api/v1/videos/42/danmaku", body, asUser(8))
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
		assert.Nil(t, dm.lastSend, "tidak diteruskan ke service: %s", body)
	}

	dm.err = status.Error(codes.ResourceExhausted, "terlalu banyak danmaku, tunggu beberapa detik")
	rec = do(r, http.MethodPost, "/api/v1/videos/42/danmaku", `{"body":"x","position_ms":1}`, asUser(8))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "10", rec.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"error":{"code":"RESOURCE_EXHAUSTED","message":"terlalu banyak danmaku, tunggu beberapa detik"}}`, rec.Body.String())
}

func TestListDanmaku(t *testing.T) {
	dm := &fakeDanmakuClient{}
	r := newTestRouter(t, Deps{Danmaku: dm})

	rec := do(r, http.MethodGet, "/api/v1/videos/42/danmaku", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"danmaku":[
		{"id":"15","video_id":"42","position_ms":15300,"body":"wkwk","color":"#FFFFFF","mode":"SCROLL","created_at":"2026-10-29T04:00:00Z"},
		{"id":"16","video_id":"42","position_ms":15300,"body":"wkwk","color":"#FF0000","mode":"TOP","created_at":"2026-10-29T04:00:00Z"}
	],"next_page_token":"MTUzMDAuMTY"}`, rec.Body.String())
	assert.Equal(t, int64(0), dm.lastList.GetFromMs())
	assert.Equal(t, int64(360_000), dm.lastList.GetToMs(), "default 6 menit")

	rec = do(r, http.MethodGet, "/api/v1/videos/42/danmaku?from_ms=360000&page_size=100&page_token=abc", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(360_000), dm.lastList.GetFromMs())
	assert.Equal(t, int64(720_000), dm.lastList.GetToMs())
	assert.Equal(t, int32(100), dm.lastList.GetPageSize())
	assert.Equal(t, "abc", dm.lastList.GetPageToken())

	rec = do(r, http.MethodGet, "/api/v1/videos/42/danmaku?from_ms=satu", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	dm.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodGet, "/api/v1/videos/42/danmaku", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestEncodeLiveDanmaku(t *testing.T) {
	data, err := json.Marshal(events.DanmakuSent{
		ID: 15, VideoID: 42, PositionMs: 1500, Body: "halo", Color: 0x00FF88, Mode: "TOP", CreatedAt: testCreatedAt,
	})
	require.NoError(t, err)
	out, err := EncodeLiveDanmaku(data)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"15","video_id":"42","position_ms":1500,"body":"halo","color":"#00FF88","mode":"TOP","created_at":"2026-10-29T04:00:00Z"}`, string(out))

	_, err = EncodeLiveDanmaku([]byte("{"))
	assert.Error(t, err)
}

// liveBroker menggantikan NATS untuk test endpoint live.
type liveBroker struct {
	mu       sync.Mutex
	handlers map[string]func([]byte)
}

func (b *liveBroker) Subscribe(subject string, h func([]byte)) (func() error, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[subject] = h
	return func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.handlers, subject)
		return nil
	}, nil
}

func (b *liveBroker) publish(subject string, data []byte) bool {
	b.mu.Lock()
	h := b.handlers[subject]
	b.mu.Unlock()
	if h == nil {
		return false
	}
	h(data)
	return true
}

// startLiveServer menjalankan router di server HTTP sungguhan, karena
// ResponseRecorder tidak bisa membaca response yang belum selesai.
func startLiveServer(t *testing.T, videos *fakeVideoClient, maxClients int) (*httptest.Server, *liveBroker, *live.Hub) {
	t.Helper()
	broker := &liveBroker{handlers: map[string]func([]byte){}}
	hub := live.NewHub(broker, live.Options{
		Subject:    events.SubjectDanmakuLive,
		Transform:  EncodeLiveDanmaku,
		MaxClients: maxClients,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := newTestRouter(t, Deps{Videos: videos, Live: hub})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Cleanup(hub.Close)
	return srv, broker, hub
}

// readEvent membaca baris SSE sampai baris kosong (akhir satu event).
func readEvent(t *testing.T, rd *bufio.Reader) string {
	t.Helper()
	var lines []string
	for {
		line, err := rd.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, line)
	}
}

func TestDanmakuLive(t *testing.T) {
	srv, broker, hub := startLiveServer(t, &fakeVideoClient{}, 10)

	resp, err := http.Get(srv.URL + "/api/v1/videos/42/danmaku/live")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))

	rd := bufio.NewReader(resp.Body)
	assert.Equal(t, "retry: 3000", readEvent(t, rd))

	data, _ := json.Marshal(events.DanmakuSent{ID: 15, VideoID: 42, PositionMs: 1500, Body: "halo", Color: 0xFFFFFF, Mode: "SCROLL", CreatedAt: testCreatedAt})
	require.Eventually(t, func() bool { return broker.publish("danmaku.live.42", data) }, time.Second, 10*time.Millisecond)
	assert.Equal(t, `event: danmaku
data: {"id":"15","video_id":"42","position_ms":1500,"body":"halo","color":"#FFFFFF","mode":"SCROLL","created_at":"2026-10-29T04:00:00Z"}`, readEvent(t, rd))

	clients, _ := hub.Stats()
	assert.Equal(t, 1, clients)

	// Browser menutup koneksi: penonton dihapus dari hub.
	resp.Body.Close()
	assert.Eventually(t, func() bool { c, _ := hub.Stats(); return c == 0 }, 2*time.Second, 10*time.Millisecond)
}

func TestDanmakuLiveHeartbeatAndShutdown(t *testing.T) {
	broker := &liveBroker{handlers: map[string]func([]byte){}}
	hub := live.NewHub(broker, live.Options{Subject: events.SubjectDanmakuLive}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := &danmakuHandler{videos: &fakeVideoClient{}, hub: hub, heartbeat: 20 * time.Millisecond, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r := newTestRouter(t, Deps{})
	r.GET("/live/:id", h.live)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/live/42")
	require.NoError(t, err)
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	assert.Equal(t, "retry: 3000", readEvent(t, rd))
	assert.Equal(t, ": ping", readEvent(t, rd), "heartbeat saat tidak ada danmaku")

	// Pod berhenti: hub.Close mengakhiri response.
	hub.Close()
	_, err = io.ReadAll(rd)
	assert.NoError(t, err, "response selesai dengan rapi")
}

func TestDanmakuLiveErrors(t *testing.T) {
	// Tanpa hub: 503.
	r := newTestRouter(t, Deps{Videos: &fakeVideoClient{}})
	rec := do(r, http.MethodGet, "/api/v1/videos/42/danmaku/live", "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// Video belum READY: 404, tidak ada yang bergabung ke hub.
	srv, _, hub := startLiveServer(t, &fakeVideoClient{getStatus: videov1.VideoStatus_VIDEO_STATUS_PROCESSING}, 1)
	resp, err := http.Get(srv.URL + "/api/v1/videos/42/danmaku/live")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	clients, _ := hub.Stats()
	assert.Zero(t, clients)

	// Video tidak ada (VideoService menjawab NOT_FOUND): 404.
	srv, _, _ = startLiveServer(t, &fakeVideoClient{err: status.Error(codes.NotFound, "video tidak ditemukan")}, 1)
	resp, err = http.Get(srv.URL + "/api/v1/videos/42/danmaku/live")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Penonton penuh: 503.
	srv, _, _ = startLiveServer(t, &fakeVideoClient{}, 1)
	first, err := http.Get(srv.URL + "/api/v1/videos/42/danmaku/live")
	require.NoError(t, err)
	defer first.Body.Close()
	require.Equal(t, http.StatusOK, first.StatusCode)
	second, err := http.Get(srv.URL + "/api/v1/videos/" + strconv.Itoa(43) + "/danmaku/live")
	require.NoError(t, err)
	second.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, second.StatusCode)
}
