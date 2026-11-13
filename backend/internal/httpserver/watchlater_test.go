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

// fakeWatchLater mencatat request Tonton nanti terakhir.
type fakeWatchLater struct {
	add    *videov1.AddToWatchLaterRequest
	remove *videov1.RemoveFromWatchLaterRequest
	list   *videov1.ListWatchLaterRequest
}

func (f *fakeVideoClient) AddToWatchLater(_ context.Context, in *videov1.AddToWatchLaterRequest, _ ...grpc.CallOption) (*videov1.AddToWatchLaterResponse, error) {
	f.later.add = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.AddToWatchLaterResponse{AddedAt: timestamppb.New(testCreatedAt)}, nil
}

func (f *fakeVideoClient) RemoveFromWatchLater(_ context.Context, in *videov1.RemoveFromWatchLaterRequest, _ ...grpc.CallOption) (*videov1.RemoveFromWatchLaterResponse, error) {
	f.later.remove = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.RemoveFromWatchLaterResponse{}, nil
}

func (f *fakeVideoClient) ListWatchLater(_ context.Context, in *videov1.ListWatchLaterRequest, _ ...grpc.CallOption) (*videov1.ListWatchLaterResponse, error) {
	f.later.list = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListWatchLaterResponse{
		Entries: []*videov1.WatchLaterEntry{{
			Video:   testVideo(videov1.VideoStatus_VIDEO_STATUS_READY),
			AddedAt: timestamppb.New(testCreatedAt),
		}},
		NextPageToken: "dzE",
	}, nil
}

func TestAddAndRemoveWatchLater(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPut, "/api/v1/me/watch-later/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"saved":true,"added_at":"2026-10-29T04:00:00Z"}`, rec.Body.String())
	assert.Equal(t, int64(8), fake.later.add.GetRequesterId(), "requester dari token")
	assert.Equal(t, int64(42), fake.later.add.GetVideoId())

	rec = do(r, http.MethodDelete, "/api/v1/me/watch-later/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"saved":false}`, rec.Body.String())
	assert.Equal(t, int64(8), fake.later.remove.GetRequesterId())
	assert.Equal(t, int64(42), fake.later.remove.GetVideoId())

	t.Run("wajib login", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodPut, "/api/v1/me/watch-later/42", "").Code)
		assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodDelete, "/api/v1/me/watch-later/42", "").Code)
	})
	t.Run("id bukan angka", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, do(r, http.MethodPut, "/api/v1/me/watch-later/abc", "", as(8)).Code)
	})
	t.Run("video belum READY", func(t *testing.T) {
		fake.err = status.Error(codes.NotFound, "video tidak ditemukan")
		t.Cleanup(func() { fake.err = nil })
		assert.Equal(t, http.StatusNotFound, do(r, http.MethodPut, "/api/v1/me/watch-later/42", "", as(8)).Code)
	})
}

func TestListWatchLater(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos, users := &fakeVideoClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/watch-later?page_size=5&page_token=abc", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8), videos.later.list.GetRequesterId())
	assert.Equal(t, int32(5), videos.later.list.GetPageSize())
	assert.Equal(t, "abc", videos.later.list.GetPageToken())
	body := rec.Body.String()
	assert.Contains(t, body, `"added_at":"2026-10-29T04:00:00Z"`)
	assert.Contains(t, body, `"id":"42"`)
	assert.Contains(t, body, `"owner":{`, "pemilik video ikut diisi seperti di riwayat")
	assert.Contains(t, body, `"next_page_token":"dzE"`)

	rec = do(r, http.MethodGet, "/api/v1/me/watch-later", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")
}

func TestGetVideoViewerSaved(t *testing.T) {
	verifier, as := newTokenSigner(t)
	r := newTestRouter(t, Deps{Videos: &fakeVideoClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/videos/42", "", as(8))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"viewer_saved":true`)

	rec = do(r, http.MethodGet, "/api/v1/videos/42", "", as(9))
	assert.Contains(t, rec.Body.String(), `"viewer_saved":false`, "login tapi belum menyimpan")

	rec = do(r, http.MethodGet, "/api/v1/videos/42", "")
	assert.NotContains(t, rec.Body.String(), "viewer_saved", "tanpa login tidak dikirim")
}
