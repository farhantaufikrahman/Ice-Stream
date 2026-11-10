package httpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

func (f *fakeVideoClient) SaveWatchProgress(_ context.Context, in *videov1.SaveWatchProgressRequest, _ ...grpc.CallOption) (*videov1.SaveWatchProgressResponse, error) {
	f.lastProgress = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.SaveWatchProgressResponse{PositionMs: in.GetPositionMs(), WatchedAt: timestamppb.New(testCreatedAt)}, nil
}

func (f *fakeVideoClient) ListWatchHistory(_ context.Context, in *videov1.ListWatchHistoryRequest, _ ...grpc.CallOption) (*videov1.ListWatchHistoryResponse, error) {
	f.lastHistory = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListWatchHistoryResponse{
		Entries: []*videov1.WatchHistoryEntry{{
			Video:      testVideo(videov1.VideoStatus_VIDEO_STATUS_READY),
			PositionMs: 12000,
			WatchedAt:  timestamppb.New(testCreatedAt),
		}},
		NextPageToken: "aDE",
	}, nil
}

func (f *fakeVideoClient) DeleteWatchHistory(_ context.Context, in *videov1.DeleteWatchHistoryRequest, _ ...grpc.CallOption) (*videov1.DeleteWatchHistoryResponse, error) {
	f.lastDelete = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.DeleteWatchHistoryResponse{}, nil
}

func (f *fakeVideoClient) ClearWatchHistory(_ context.Context, in *videov1.ClearWatchHistoryRequest, _ ...grpc.CallOption) (*videov1.ClearWatchHistoryResponse, error) {
	f.lastClear = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ClearWatchHistoryResponse{Deleted: 3}, nil
}

func TestSaveWatchProgress(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPut, "/api/v1/me/history/42", `{"position_ms":93500}`, as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8), fake.lastProgress.GetRequesterId(), "requester dari token")
	assert.Equal(t, int64(42), fake.lastProgress.GetVideoId())
	assert.Equal(t, int64(93500), fake.lastProgress.GetPositionMs())
	assert.JSONEq(t, `{"position_ms":93500,"watched_at":"2026-10-29T04:00:00Z"}`, rec.Body.String())

	// Posisi 0 itu valid (baru mulai), berbeda dengan lupa mengirim.
	rec = do(r, http.MethodPut, "/api/v1/me/history/42", `{"position_ms":0}`, as(8))
	assert.Equal(t, http.StatusOK, rec.Code)

	cases := map[string]struct {
		path, body string
		want       int
	}{
		"tanpa position_ms":    {"/api/v1/me/history/42", `{}`, http.StatusBadRequest},
		"bukan bilangan bulat": {"/api/v1/me/history/42", `{"position_ms":1.5}`, http.StatusBadRequest},
		"bukan JSON":           {"/api/v1/me/history/42", `halo`, http.StatusBadRequest},
		"id bukan angka":       {"/api/v1/me/history/abc", `{"position_ms":1}`, http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(r, http.MethodPut, tc.path, tc.body, as(8))
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}

	rec = do(r, http.MethodPut, "/api/v1/me/history/42", `{"position_ms":1}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")

	fake.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodPut, "/api/v1/me/history/42", `{"position_ms":1}`, as(8))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListWatchHistory(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos, users := &fakeVideoClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/history?page_size=5&page_token=abc", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8), videos.lastHistory.GetRequesterId())
	assert.Equal(t, int32(5), videos.lastHistory.GetPageSize())
	assert.Equal(t, "abc", videos.lastHistory.GetPageToken())
	body := rec.Body.String()
	assert.Contains(t, body, `"position_ms":12000`)
	assert.Contains(t, body, `"watched_at":"2026-10-29T04:00:00Z"`)
	assert.Contains(t, body, `"id":"42"`)
	assert.Contains(t, body, `"owner":{`, "pemilik video ikut diisi seperti di feed")
	assert.Contains(t, body, `"next_page_token":"aDE"`)

	rec = do(r, http.MethodGet, "/api/v1/me/history", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")
}

func TestDeleteWatchHistory(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodDelete, "/api/v1/me/history/42", "", as(8))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, int64(8), fake.lastDelete.GetRequesterId())
	assert.Equal(t, int64(42), fake.lastDelete.GetVideoId())

	rec = do(r, http.MethodDelete, "/api/v1/me/history", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"deleted":3}`, rec.Body.String())
	assert.Equal(t, int64(8), fake.lastClear.GetRequesterId())

	rec = do(r, http.MethodDelete, "/api/v1/me/history", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetVideoResumePosition(t *testing.T) {
	verifier, as := newTokenSigner(t)
	r := newTestRouter(t, Deps{Videos: &fakeVideoClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/videos/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"resume_position_ms":93500`)

	// Login tapi belum pernah menonton: tetap dikirim, nilainya 0.
	rec = do(r, http.MethodGet, "/api/v1/videos/42", "", as(9))
	assert.Contains(t, rec.Body.String(), `"resume_position_ms":0`)

	// Tanpa login: tidak dikirim sama sekali.
	rec = do(r, http.MethodGet, "/api/v1/videos/42", "")
	assert.NotContains(t, rec.Body.String(), "resume_position_ms")
}
