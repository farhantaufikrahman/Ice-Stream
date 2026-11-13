package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
)

// fakeLiveClient menggantikan LiveService sungguhan di test.
type fakeLiveClient struct {
	err error

	lastList   *livev1.ListLiveStreamsRequest
	lastGet    *livev1.GetLiveStreamRequest
	lastMine   *livev1.GetMyChannelRequest
	lastUpdate *livev1.UpdateMyChannelRequest
	lastReset  *livev1.ResetStreamKeyRequest
	lastToken  *livev1.CreatePublishTokenRequest
	lastSend   *livev1.SendChatMessageRequest
	lastChat   *livev1.ListChatMessagesRequest
	// followersOnly mengisi pilihan chat di GetLiveStream (langkah 27).
	followersOnly bool
}

var _ livev1.LiveServiceClient = (*fakeLiveClient)(nil)

var testLiveStart = time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)

func testLiveStream() *livev1.LiveStream {
	return &livev1.LiveStream{
		Id: 3, OwnerId: 7, Title: "Ngoding bareng", Category: "teknologi", Status: livev1.LiveStatus_LIVE_STATUS_LIVE,
		StartedAt: timestamppb.New(testLiveStart), PlaybackUrl: "http://live.ice-stream.localhost:4000/live/7/index.m3u8",
		ViewerCount: 4, PeakViewers: 9,
	}
}

func testLiveChannel() *livev1.LiveChannel {
	return &livev1.LiveChannel{
		OwnerId: 7, Title: "Ngoding bareng", Category: "teknologi", IngestUrl: "rtmp://localhost:1935/live",
		HasStreamKey: true, StreamKeyCreatedAt: timestamppb.New(testLiveStart), Current: testLiveStream(),
		RecordEnabled: true, SlowMode: true,
		LastRecording: &livev1.Recording{StreamId: 2, Status: "DONE", VideoId: 42, EndedAt: timestamppb.New(testLiveStart.Add(-time.Hour))},
	}
}

func (f *fakeLiveClient) GetMyChannel(_ context.Context, in *livev1.GetMyChannelRequest, _ ...grpc.CallOption) (*livev1.GetMyChannelResponse, error) {
	f.lastMine = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.GetMyChannelResponse{Channel: testLiveChannel()}, nil
}

func (f *fakeLiveClient) UpdateMyChannel(_ context.Context, in *livev1.UpdateMyChannelRequest, _ ...grpc.CallOption) (*livev1.UpdateMyChannelResponse, error) {
	f.lastUpdate = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.UpdateMyChannelResponse{Channel: testLiveChannel()}, nil
}

func (f *fakeLiveClient) ResetStreamKey(_ context.Context, in *livev1.ResetStreamKeyRequest, _ ...grpc.CallOption) (*livev1.ResetStreamKeyResponse, error) {
	f.lastReset = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.ResetStreamKeyResponse{Channel: testLiveChannel(), StreamKey: "7?key=isk_rahasia"}, nil
}

func (f *fakeLiveClient) ListLiveStreams(_ context.Context, in *livev1.ListLiveStreamsRequest, _ ...grpc.CallOption) (*livev1.ListLiveStreamsResponse, error) {
	f.lastList = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.ListLiveStreamsResponse{Streams: []*livev1.LiveStream{testLiveStream()}}, nil
}

func (f *fakeLiveClient) GetLiveStream(_ context.Context, in *livev1.GetLiveStreamRequest, _ ...grpc.CallOption) (*livev1.GetLiveStreamResponse, error) {
	f.lastGet = in
	if f.err != nil {
		return nil, f.err
	}
	st := testLiveStream()
	st.Status = livev1.LiveStatus_LIVE_STATUS_ENDED
	st.EndedAt = timestamppb.New(testLiveStart.Add(time.Hour))
	st.PlaybackUrl, st.ViewerCount = "", 0
	st.SlowMode, st.FollowersOnly = true, f.followersOnly
	return &livev1.GetLiveStreamResponse{Stream: st}, nil
}

func testChatMessage() *livev1.ChatMessage {
	return &livev1.ChatMessage{Id: 15, StreamId: 3, UserId: 7, Body: "halo kak", Color: 0x4FE3C1, CreatedAt: timestamppb.New(testLiveStart)}
}

func (f *fakeLiveClient) CreatePublishToken(_ context.Context, in *livev1.CreatePublishTokenRequest, _ ...grpc.CallOption) (*livev1.CreatePublishTokenResponse, error) {
	f.lastToken = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.CreatePublishTokenResponse{
		WhipUrl: "http://webrtc.ice-stream.localhost:4000/live/7/whip", Token: "ipt_rahasia",
		ExpiresAt: timestamppb.New(testLiveStart.Add(5 * time.Minute)),
	}, nil
}

func (f *fakeLiveClient) SendChatMessage(_ context.Context, in *livev1.SendChatMessageRequest, _ ...grpc.CallOption) (*livev1.SendChatMessageResponse, error) {
	f.lastSend = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.SendChatMessageResponse{Message: testChatMessage()}, nil
}

func (f *fakeLiveClient) ListChatMessages(_ context.Context, in *livev1.ListChatMessagesRequest, _ ...grpc.CallOption) (*livev1.ListChatMessagesResponse, error) {
	f.lastChat = in
	if f.err != nil {
		return nil, f.err
	}
	return &livev1.ListChatMessagesResponse{Messages: []*livev1.ChatMessage{testChatMessage()}}, nil
}

func TestListLive(t *testing.T) {
	fake := &fakeLiveClient{}
	r := newTestRouter(t, Deps{LiveStreams: fake, Users: &fakeUserClient{}})

	rec := do(r, http.MethodGet, "/api/v1/live?category=teknologi&limit=8", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"streams":[{
		"id":"3","owner_id":"7","title":"Ngoding bareng","category":"teknologi","status":"LIVE",
		"started_at":"2026-11-12T07:00:00Z","playback_url":"http://live.ice-stream.localhost:4000/live/7/index.m3u8",
		"viewer_count":4,"peak_viewers":9,
		"owner":{"id":"7","username":"budi","display_name":"Budi","avatar_url":"http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg"}}]}`,
		rec.Body.String())
	assert.Equal(t, int32(8), fake.lastList.GetLimit())
	assert.Equal(t, "teknologi", fake.lastList.GetCategory())

	rec = do(r, http.MethodGet, "/api/v1/live?q=ngoding", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ngoding", fake.lastList.GetQuery(), "tab Live di pencarian")

	rec = do(r, http.MethodGet, "/api/v1/live?limit=banyak", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	fake.err = status.Error(codes.InvalidArgument, "category: harus salah satu dari: ...")
	rec = do(r, http.MethodGet, "/api/v1/live?category=politik", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetLive(t *testing.T) {
	fake := &fakeLiveClient{}
	r := newTestRouter(t, Deps{LiveStreams: fake, Users: &fakeUserClient{}})

	rec := do(r, http.MethodGet, "/api/v1/live/3", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Stream map[string]any `json:"stream"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ENDED", body.Stream["status"])
	assert.Equal(t, "2026-11-12T08:00:00Z", body.Stream["ended_at"])
	assert.NotContains(t, body.Stream, "playback_url", "siaran selesai tanpa URL putar")
	assert.Equal(t, int64(3), fake.lastGet.GetStreamId())

	rec = do(r, http.MethodGet, "/api/v1/live/abc", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	fake.err = status.Error(codes.NotFound, "siaran tidak ditemukan")
	rec = do(r, http.MethodGet, "/api/v1/live/99", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMyLiveChannel(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeLiveClient{}
	r := newTestRouter(t, Deps{LiveStreams: fake, Verifier: verifier})

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/me/live", ""},
		{http.MethodPatch, "/api/v1/me/live", `{"title":"x"}`},
		{http.MethodPost, "/api/v1/me/live/stream-key", ""},
	} {
		rec := do(r, req.method, req.path, req.body)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login: "+req.method+" "+req.path)
	}

	rec := do(r, http.MethodGet, "/api/v1/me/live", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"ingest_url":"rtmp://localhost:1935/live"`)
	assert.Contains(t, rec.Body.String(), `"has_stream_key":true`)
	assert.Contains(t, rec.Body.String(), `"current":{"id":"3"`)
	assert.NotContains(t, rec.Body.String(), "isk_", "key tidak pernah ada di GET")
	assert.Contains(t, rec.Body.String(), `"record_enabled":true,"slow_mode":true,"followers_only":false`)
	assert.Contains(t, rec.Body.String(), `"last_recording":{"stream_id":"2","status":"DONE","video_id":"42","ended_at":"2026-11-12T06:00:00Z"}`)
	assert.Equal(t, int64(7), fake.lastMine.GetRequesterId())

	rec = do(r, http.MethodPatch, "/api/v1/me/live", `{"title":"Ngoding bareng","category":"teknologi"}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Ngoding bareng", fake.lastUpdate.GetTitle())
	assert.Equal(t, "teknologi", fake.lastUpdate.GetCategory())
	assert.Nil(t, fake.lastUpdate.RecordEnabled, "pilihan yang tidak dikirim tidak diubah")

	rec = do(r, http.MethodPatch, "/api/v1/me/live", `{"title":"Ngoding bareng","record_enabled":true,"slow_mode":false,"followers_only":true}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, fake.lastUpdate.RecordEnabled)
	assert.True(t, fake.lastUpdate.GetRecordEnabled())
	require.NotNil(t, fake.lastUpdate.SlowMode)
	assert.False(t, fake.lastUpdate.GetSlowMode())
	assert.True(t, fake.lastUpdate.GetFollowersOnly())

	rec = do(r, http.MethodPost, "/api/v1/me/live/stream-key", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"stream_key":"7?key=isk_rahasia"`)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, int64(7), fake.lastReset.GetRequesterId())

	fake.err = status.Error(codes.InvalidArgument, "title: judul siaran wajib diisi")
	rec = do(r, http.MethodPatch, "/api/v1/me/live", `{"title":""}`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Langkah 29: LIVE_BROADCAST_ADMIN_ONLY menutup /me/live untuk non-admin,
// sementara halaman siaran dan chat tetap terbuka untuk semua.
func TestMyLiveAdminOnly(t *testing.T) {
	verifier, asUser, asAdmin := newRoleTokenSigner(t)
	fake := &fakeLiveClient{}
	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		Env:                    "development",
		CORSAllowedOrigins:     []string{"http://localhost:5173"},
		Cookie:                 config.CookieConfig{SameSite: http.SameSiteLaxMode},
		LiveBroadcastAdminOnly: true,
	}
	r, err := NewRouter(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Deps{LiveStreams: fake, Users: &fakeUserClient{}, Verifier: verifier})
	require.NoError(t, err)

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/me/live", ""},
		{http.MethodPatch, "/api/v1/me/live", `{"title":"x"}`},
		{http.MethodPost, "/api/v1/me/live/stream-key", ""},
		{http.MethodPost, "/api/v1/me/live/publish-token", ""},
	} {
		rec := do(r, req.method, req.path, req.body, asUser(7))
		assert.Equal(t, http.StatusForbidden, rec.Code, req.method+" "+req.path)
		assert.Contains(t, rec.Body.String(), `"LIVE_BROADCAST_ADMIN_ONLY"`)
	}
	assert.Nil(t, fake.lastMine, "VideoService tidak dipanggil untuk non-admin")

	rec := do(r, http.MethodGet, "/api/v1/me/live", "", asAdmin(1))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(1), fake.lastMine.GetRequesterId())

	rec = do(r, http.MethodGet, "/api/v1/live", "")
	assert.Equal(t, http.StatusOK, rec.Code, "daftar siaran tetap terbuka")
}

const testChatJSON = `{"id":"15","stream_id":"3","user":{"id":"7","username":"budi","display_name":"Budi","avatar_url":"http://media.ice-stream.localhost:4000/images/avatars/7/ab.jpg"},"body":"halo kak","color":"#4FE3C1","created_at":"2026-11-12T07:00:00Z"}`

func TestPublishToken(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeLiveClient{}
	r := newTestRouter(t, Deps{LiveStreams: fake, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/me/live/publish-token", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = do(r, http.MethodPost, "/api/v1/me/live/publish-token", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"whip_url":"http://webrtc.ice-stream.localhost:4000/live/7/whip","token":"ipt_rahasia","expires_at":"2026-11-12T07:05:00Z"}`, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, int64(7), fake.lastToken.GetRequesterId())
}

func TestLiveChat(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeLiveClient{}
	r := newTestRouter(t, Deps{LiveStreams: fake, Users: &fakeUserClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/live/3/chat?limit=20", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"messages":[`+testChatJSON+`]}`, rec.Body.String())
	assert.Equal(t, int64(3), fake.lastChat.GetStreamId())
	assert.Equal(t, int32(20), fake.lastChat.GetLimit())

	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"halo kak"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "kirim chat wajib login")

	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"halo kak","color":"#4FE3C1"}`, as(7))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"message":`+testChatJSON+`}`, rec.Body.String())
	assert.Equal(t, int64(7), fake.lastSend.GetRequesterId())
	assert.Equal(t, int64(3), fake.lastSend.GetStreamId())
	assert.Equal(t, uint32(0x4FE3C1), fake.lastSend.GetColor())
	assert.False(t, fake.lastSend.GetRequesterFollows(), "chat bebas: langganan tidak diperiksa")

	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"x","color":"merah"}`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = do(r, http.MethodGet, "/api/v1/live/abc/chat", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	fake.err = status.Error(codes.ResourceExhausted, "tunggu sebentar sebelum mengirim pesan lagi")
	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"lagi"}`, as(7))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	fake.err = status.Error(codes.FailedPrecondition, "siaran sudah selesai")
	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"lagi"}`, as(7))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestLiveChatFollowersOnly(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeLiveClient{followersOnly: true}
	users := &fakeUserClient{channelIDs: []int64{5, 7}}
	r := newTestRouter(t, Deps{LiveStreams: fake, Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/live/3", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"chat":{"slow_mode":true,"followers_only":true}`)

	// Pemilik siaran (user 7) ada di daftar kanal yang diikuti user 9.
	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"halo"}`, as(9))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, int64(9), users.lastChannelIDs.GetRequesterId())
	assert.True(t, fake.lastSend.GetRequesterFollows())

	users.channelIDs = []int64{5}
	do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"halo"}`, as(9))
	assert.False(t, fake.lastSend.GetRequesterFollows())

	fake.err = status.Error(codes.PermissionDenied, "chat siaran ini hanya untuk pengikut kanal")
	rec = do(r, http.MethodPost, "/api/v1/live/3/chat", `{"body":"halo"}`, as(9))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "hanya untuk pengikut")
}

func TestLiveChatStream(t *testing.T) {
	// Tanpa hub: 503.
	r := newTestRouter(t, Deps{LiveStreams: &fakeLiveClient{}})
	rec := do(r, http.MethodGet, "/api/v1/live/3/chat/live", "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	broker := &liveBroker{handlers: map[string]func([]byte){}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	users := &fakeUserClient{}
	hub := live.NewHub(broker, live.Options{Subject: events.SubjectLiveChat, Transform: NewLiveChatEncoder(users, logger)}, logger)
	t.Cleanup(hub.Close)
	fake := &fakeLiveClient{}
	srv := httptest.NewServer(newTestRouter(t, Deps{LiveStreams: fake, LiveChat: hub}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/live/3/chat/live")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	rd := bufio.NewReader(resp.Body)
	assert.Equal(t, "retry: 3000", readEvent(t, rd))

	data, _ := json.Marshal(events.LiveChatSent{ID: 15, StreamID: 3, UserID: 7, Body: "halo kak", Color: 0x4FE3C1, CreatedAt: testLiveStart})
	require.Eventually(t, func() bool { return broker.publish("live.chat.3", data) }, time.Second, 10*time.Millisecond)
	assert.Equal(t, "event: chat\ndata: "+testChatJSON, readEvent(t, rd))

	// Pesan kedua dari orang yang sama: profil diambil dari cache.
	users.lastBatch = nil
	require.True(t, broker.publish("live.chat.3", data))
	readEvent(t, rd)
	assert.Nil(t, users.lastBatch, "profil pengirim disimpan sebentar")

	// Siaran tidak ada: 404, tidak bergabung ke hub.
	fake.err = status.Error(codes.NotFound, "siaran tidak ditemukan")
	resp2, err := http.Get(srv.URL + "/api/v1/live/99/chat/live")
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
	clients, _ := hub.Stats()
	assert.Equal(t, 1, clients)
}
