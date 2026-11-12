package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
)

// fakeLiveClient menggantikan LiveService sungguhan di test.
type fakeLiveClient struct {
	err error

	lastList   *livev1.ListLiveStreamsRequest
	lastGet    *livev1.GetLiveStreamRequest
	lastMine   *livev1.GetMyChannelRequest
	lastUpdate *livev1.UpdateMyChannelRequest
	lastReset  *livev1.ResetStreamKeyRequest
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
	return &livev1.GetLiveStreamResponse{Stream: st}, nil
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
	assert.Equal(t, int64(7), fake.lastMine.GetRequesterId())

	rec = do(r, http.MethodPatch, "/api/v1/me/live", `{"title":"Ngoding bareng","category":"teknologi"}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Ngoding bareng", fake.lastUpdate.GetTitle())
	assert.Equal(t, "teknologi", fake.lastUpdate.GetCategory())

	rec = do(r, http.MethodPost, "/api/v1/me/live/stream-key", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"stream_key":"7?key=isk_rahasia"`)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, int64(7), fake.lastReset.GetRequesterId())

	fake.err = status.Error(codes.InvalidArgument, "title: judul siaran wajib diisi")
	rec = do(r, http.MethodPatch, "/api/v1/me/live", `{"title":""}`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
