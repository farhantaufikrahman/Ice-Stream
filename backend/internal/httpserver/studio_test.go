package httpserver

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Versi palsu RPC langkah 26 (Creator Studio dan thumbnail).

func (f *fakeCommentClient) ListChannelComments(_ context.Context, in *commentv1.ListChannelCommentsRequest, _ ...grpc.CallOption) (*commentv1.ListChannelCommentsResponse, error) {
	f.lastChannel = in
	if f.err != nil {
		return nil, f.err
	}
	return &commentv1.ListChannelCommentsResponse{
		Comments:      []*commentv1.ChannelComment{{Comment: testComment(11, 10), VideoTitle: "Belajar Go"}},
		NextPageToken: "MTE",
	}, nil
}

func (f *fakeDanmakuClient) ListChannelDanmaku(_ context.Context, in *danmakuv1.ListChannelDanmakuRequest, _ ...grpc.CallOption) (*danmakuv1.ListChannelDanmakuResponse, error) {
	f.lastChannel = in
	if f.err != nil {
		return nil, f.err
	}
	return &danmakuv1.ListChannelDanmakuResponse{
		Danmaku: []*danmakuv1.ChannelDanmaku{{Danmaku: testDanmaku(5), VideoTitle: "Belajar Go"}},
	}, nil
}

func (f *fakeDanmakuClient) DeleteDanmaku(_ context.Context, in *danmakuv1.DeleteDanmakuRequest, _ ...grpc.CallOption) (*danmakuv1.DeleteDanmakuResponse, error) {
	f.lastDelete = in
	if f.err != nil {
		return nil, f.err
	}
	return &danmakuv1.DeleteDanmakuResponse{}, nil
}

func (f *fakeVideoClient) ChooseThumbnailFrame(_ context.Context, in *videov1.ChooseThumbnailFrameRequest, _ ...grpc.CallOption) (*videov1.ChooseThumbnailFrameResponse, error) {
	f.lastFrame = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ChooseThumbnailFrameResponse{Video: ownerVideo()}, nil
}

func (f *fakeVideoClient) UploadThumbnail(_ context.Context, in *videov1.UploadThumbnailRequest, _ ...grpc.CallOption) (*videov1.UploadThumbnailResponse, error) {
	f.lastThumb = in
	if f.err != nil {
		return nil, f.err
	}
	v := ownerVideo()
	v.CustomThumbnail = true
	return &videov1.UploadThumbnailResponse{Video: v}, nil
}

// ownerVideo: video READY Tidak publik dengan dua pilihan bingkai.
func ownerVideo() *videov1.Video {
	v := testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)
	v.Visibility = videov1.VideoVisibility_VIDEO_VISIBILITY_UNLISTED
	v.Language = "ja"
	v.ThumbnailFrames = []*videov1.ThumbnailFrame{
		{PositionMs: 3000, Url: "http://media/hls/42/x/frames/3000.jpg", Selected: true},
		{PositionMs: 8000, Url: "http://media/hls/42/x/frames/8000.jpg"},
	}
	return v
}

func TestMyComments(t *testing.T) {
	verifier, as := newTokenSigner(t)
	comments := &fakeCommentClient{}
	r := newTestRouter(t, Deps{Comments: comments, Users: &fakeUserClient{}, Verifier: verifier})

	assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodGet, "/api/v1/me/comments", "").Code)
	rec := do(r, http.MethodGet, "/api/v1/me/comments?video_id=42&page_size=10&page_token=abc", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(7), comments.lastChannel.GetRequesterId())
	assert.Equal(t, int64(42), comments.lastChannel.GetVideoId())
	assert.Equal(t, int32(10), comments.lastChannel.GetPageSize())
	assert.Equal(t, "abc", comments.lastChannel.GetPageToken())
	body := rec.Body.String()
	assert.Contains(t, body, `"video_title":"Belajar Go"`)
	assert.Contains(t, body, `"parent_id":"10"`)
	assert.Contains(t, body, `"next_page_token":"MTE"`)

	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/me/comments?video_id=abc", "", as(7)).Code)
}

func TestMyDanmaku(t *testing.T) {
	verifier, as := newTokenSigner(t)
	dm := &fakeDanmakuClient{}
	r := newTestRouter(t, Deps{Danmaku: dm, Users: &fakeUserClient{}, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/danmaku", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(0), dm.lastChannel.GetVideoId())
	body := rec.Body.String()
	// Pemilik video melihat pengirim danmaku (moderasi).
	assert.Contains(t, body, `"user_id":"7"`)
	assert.Contains(t, body, `"video_title":"Belajar Go"`)
	assert.Contains(t, body, `"body":"wkwk"`)

	rec = do(r, http.MethodDelete, "/api/v1/me/danmaku/5", "", as(7))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, int64(5), dm.lastDelete.GetDanmakuId())
	assert.Equal(t, int64(7), dm.lastDelete.GetRequesterId())
	assert.Equal(t, http.StatusNotFound, do(r, http.MethodDelete, "/api/v1/me/danmaku/abc", "", as(7)).Code)

	dm.err = status.Error(codes.PermissionDenied, "kamu tidak boleh menghapus danmaku ini")
	assert.Equal(t, http.StatusForbidden, do(r, http.MethodDelete, "/api/v1/me/danmaku/5", "", as(8)).Code)
}

func TestVideoVisibilityAndThumbnail(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos", `{"title":"Tes","content_type":"video/mp4","size_bytes":1,"visibility":"unlisted","draft":true,"language":"ja"}`, as(7))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, videov1.VideoVisibility_VIDEO_VISIBILITY_UNLISTED, fake.lastCreate.GetVisibility())
	assert.True(t, fake.lastCreate.GetDraft())
	assert.Equal(t, "ja", fake.lastCreate.GetLanguage())
	rec = do(r, http.MethodPost, "/api/v1/videos", `{"title":"Tes","visibility":"rahasia"}`, as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// PATCH: "Terbitkan" sebagai Pribadi.
	rec = do(r, http.MethodPatch, "/api/v1/videos/42", `{"draft":false,"visibility":"PRIVATE","language":"id"}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, fake.lastUpdate.GetDraft())
	require.NotNil(t, fake.lastUpdate.Draft)
	assert.Equal(t, videov1.VideoVisibility_VIDEO_VISIBILITY_PRIVATE, fake.lastUpdate.GetVisibility())
	assert.Equal(t, "id", fake.lastUpdate.GetLanguage())
	rec = do(r, http.MethodPatch, "/api/v1/videos/42", `{"title":"Baru"}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, fake.lastUpdate.Visibility, "tidak dikirim = tidak diubah")
	assert.Nil(t, fake.lastUpdate.Draft)
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodPatch, "/api/v1/videos/42", `{"visibility":""}`, as(7)).Code)

	// Pilih bingkai.
	rec = do(r, http.MethodPut, "/api/v1/videos/42/thumbnail/frame", `{"position_ms":8000}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(8000), fake.lastFrame.GetPositionMs())
	body := rec.Body.String()
	assert.Contains(t, body, `"visibility":"UNLISTED"`)
	assert.Contains(t, body, `"language":"ja"`)
	assert.Contains(t, body, `"thumbnail_frames":[{"position_ms":3000,"url":"http://media/hls/42/x/frames/3000.jpg","selected":true}`)

	// Unggah gambar: body apa adanya, maksimal 2 MiB.
	rec = do(r, http.MethodPut, "/api/v1/videos/42/thumbnail", "\x89PNG...", as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []byte("\x89PNG..."), fake.lastThumb.GetImage())
	assert.Contains(t, rec.Body.String(), `"custom_thumbnail":true`)
	rec = do(r, http.MethodPut, "/api/v1/videos/42/thumbnail", strings.Repeat("x", maxThumbnailBodyBytes+1), as(7))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodPut, "/api/v1/videos/42/thumbnail", "x").Code)
}

func TestThumbnailFramesOwnerOnly(t *testing.T) {
	assert.Empty(t, toVideoResponse(ownerVideo()).ThumbnailFrames, "jawaban publik tanpa bingkai")
	assert.Len(t, toOwnerVideoResponse(ownerVideo()).ThumbnailFrames, 2)
}
