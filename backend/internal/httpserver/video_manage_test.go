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

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

func (f *fakeVideoClient) UpdateVideo(_ context.Context, in *videov1.UpdateVideoRequest, _ ...grpc.CallOption) (*videov1.UpdateVideoResponse, error) {
	f.lastUpdate = in
	if f.err != nil {
		return nil, f.err
	}
	v := testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)
	if in.Title != nil {
		v.Title = in.GetTitle()
	}
	return &videov1.UpdateVideoResponse{Video: v}, nil
}

func (f *fakeVideoClient) DeleteVideo(_ context.Context, in *videov1.DeleteVideoRequest, _ ...grpc.CallOption) (*videov1.DeleteVideoResponse, error) {
	f.lastDeleteV = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.DeleteVideoResponse{}, nil
}

func TestUpdateVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPatch, "/api/v1/videos/42", `{"title":"Judul baru"}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(7), fake.lastUpdate.GetRequesterId(), "requester dari token")
	assert.Equal(t, int64(42), fake.lastUpdate.GetVideoId())
	require.NotNil(t, fake.lastUpdate.Title)
	assert.Equal(t, "Judul baru", fake.lastUpdate.GetTitle())
	assert.Nil(t, fake.lastUpdate.Description, "description tidak dikirim = tidak diubah")
	assert.Contains(t, rec.Body.String(), `"title":"Judul baru"`)

	// Deskripsi kosong dikirim apa adanya (menghapus deskripsi).
	rec = do(r, http.MethodPatch, "/api/v1/videos/42", `{"description":""}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, fake.lastUpdate.Description)
	assert.Empty(t, fake.lastUpdate.GetDescription())
	assert.Nil(t, fake.lastUpdate.Title)

	rec = do(r, http.MethodPatch, "/api/v1/videos/42", `bukan json`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = do(r, http.MethodPatch, "/api/v1/videos/42", `{"title":"x"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")

	fake.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodPatch, "/api/v1/videos/42", `{"title":"x"}`, as(8))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodDelete, "/api/v1/videos/42", "", as(7))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, int64(7), fake.lastDeleteV.GetRequesterId())
	assert.Equal(t, int64(42), fake.lastDeleteV.GetVideoId())

	rec = do(r, http.MethodDelete, "/api/v1/videos/42", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = do(r, http.MethodDelete, "/api/v1/videos/abc", "", as(7))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	fake.err = status.Error(codes.FailedPrecondition, "video sedang diproses, hapus setelah statusnya READY atau FAILED")
	rec = do(r, http.MethodDelete, "/api/v1/videos/42", "", as(7))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "sedang diproses")
}
