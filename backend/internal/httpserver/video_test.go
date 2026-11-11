package httpserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
)

// fakeVideoClient menggantikan VideoService sungguhan di test.
type fakeVideoClient struct {
	err error
	// getStatus mengganti status video dari GetVideo (0 = READY).
	getStatus videov1.VideoStatus

	lastCreate   *videov1.CreateUploadRequest
	lastComplete *videov1.CompleteUploadRequest
	lastGet      *videov1.GetVideoRequest
	lastList     *videov1.ListMyVideosRequest
	lastFeed     *videov1.ListVideosRequest
	lastView     *videov1.RecordViewRequest
	lastLike     *videov1.LikeVideoRequest
	lastUnlike   *videov1.UnlikeVideoRequest
	lastSearch   *videov1.SearchVideosRequest
	lastProgress *videov1.SaveWatchProgressRequest
	lastHistory  *videov1.ListWatchHistoryRequest
	lastDelete   *videov1.DeleteWatchHistoryRequest
	lastClear    *videov1.ClearWatchHistoryRequest
	lastUpdate   *videov1.UpdateVideoRequest
	lastDeleteV  *videov1.DeleteVideoRequest
	// notif berisi request notifikasi terakhir (notification_test.go).
	notif fakeNotifications
	// later berisi request Tonton nanti terakhir (watchlater_test.go).
	later fakeWatchLater
}

var _ videov1.VideoServiceClient = (*fakeVideoClient)(nil)

func testVideo(status videov1.VideoStatus) *videov1.Video {
	v := &videov1.Video{
		Id: 42, OwnerId: 7, Title: "Tes", Status: status, ContentType: "video/mp4",
		SizeBytes: 1234, CreatedAt: timestamppb.New(testCreatedAt),
	}
	if status != videov1.VideoStatus_VIDEO_STATUS_PENDING_UPLOAD {
		v.UploadedAt = timestamppb.New(testCreatedAt.Add(time.Minute))
	}
	switch status {
	case videov1.VideoStatus_VIDEO_STATUS_READY:
		v.DurationMs = 20023
		v.Renditions = []string{"360p", "720p"}
		v.HlsUrl = "http://media.ice-stream.localhost:4000/videos/hls/42/master.m3u8"
		v.ThumbnailUrl = "http://media.ice-stream.localhost:4000/videos/hls/42/thumbnail.jpg"
		v.ProcessedAt = timestamppb.New(testCreatedAt.Add(2 * time.Minute))
		v.ViewCount = 5
		v.CommentCount = 3
	case videov1.VideoStatus_VIDEO_STATUS_FAILED:
		v.FailureReason = "file tidak bisa dibaca sebagai video"
		v.ProcessedAt = timestamppb.New(testCreatedAt.Add(2 * time.Minute))
	}
	return v
}

func (f *fakeVideoClient) CreateUpload(_ context.Context, in *videov1.CreateUploadRequest, _ ...grpc.CallOption) (*videov1.CreateUploadResponse, error) {
	f.lastCreate = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.CreateUploadResponse{
		Video:              testVideo(videov1.VideoStatus_VIDEO_STATUS_PENDING_UPLOAD),
		UploadUrl:          "http://localhost:9000/videos/raw/abc/source?X-Amz-Signature=x",
		UploadUrlExpiresAt: timestamppb.New(testCreatedAt.Add(15 * time.Minute)),
		UploadHeaders:      map[string]string{"Content-Type": "video/mp4"},
	}, nil
}

func (f *fakeVideoClient) CompleteUpload(_ context.Context, in *videov1.CompleteUploadRequest, _ ...grpc.CallOption) (*videov1.CompleteUploadResponse, error) {
	f.lastComplete = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.CompleteUploadResponse{Video: testVideo(videov1.VideoStatus_VIDEO_STATUS_UPLOADED)}, nil
}

func (f *fakeVideoClient) GetVideo(_ context.Context, in *videov1.GetVideoRequest, _ ...grpc.CallOption) (*videov1.GetVideoResponse, error) {
	f.lastGet = in
	if f.err != nil {
		return nil, f.err
	}
	status := videov1.VideoStatus_VIDEO_STATUS_READY
	if f.getStatus != videov1.VideoStatus_VIDEO_STATUS_UNSPECIFIED {
		status = f.getStatus
	}
	resp := &videov1.GetVideoResponse{Video: testVideo(status)}
	// User 8 pernah menonton video ini sampai detik 93,5.
	if in.GetViewerId() == 8 {
		resp.ResumePositionMs = 93500
		resp.ViewerSaved = true
	}
	// Seperti VideoService: file asli hanya untuk pemilik (user 7).
	if in.GetViewerId() == 7 {
		resp.OriginalUrl = "http://localhost:9000/videos/raw/abc/source?X-Amz-Signature=y"
		resp.OriginalUrlExpiresAt = timestamppb.New(testCreatedAt.Add(time.Hour))
	}
	return resp, nil
}

func (f *fakeVideoClient) ListMyVideos(_ context.Context, in *videov1.ListMyVideosRequest, _ ...grpc.CallOption) (*videov1.ListMyVideosResponse, error) {
	f.lastList = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListMyVideosResponse{
		Videos: []*videov1.Video{
			testVideo(videov1.VideoStatus_VIDEO_STATUS_READY),
			testVideo(videov1.VideoStatus_VIDEO_STATUS_FAILED),
		},
		NextPageToken: "42",
	}, nil
}

func (f *fakeVideoClient) ListVideos(_ context.Context, in *videov1.ListVideosRequest, _ ...grpc.CallOption) (*videov1.ListVideosResponse, error) {
	f.lastFeed = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListVideosResponse{
		Videos:        []*videov1.Video{testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)},
		NextPageToken: "MTc2MjM4ODk2MDAwMDAwMC40Mg",
	}, nil
}

func (f *fakeVideoClient) RecordView(_ context.Context, in *videov1.RecordViewRequest, _ ...grpc.CallOption) (*videov1.RecordViewResponse, error) {
	f.lastView = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.RecordViewResponse{Counted: true, ViewCount: 6}, nil
}

// newTokenSigner membuat Verifier dan fungsi untuk menandatangani access
// token user tertentu, supaya test bisa memanggil endpoint yang butuh login.
func newTokenSigner(t *testing.T) (*accesstoken.Verifier, func(userID int64) func(*http.Request)) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return accesstoken.NewVerifier(pub), func(userID int64) func(*http.Request) {
		now := time.Now()
		token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, accesstoken.Claims{
			Role: "USER",
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: accesstoken.Issuer, Subject: strconv.FormatInt(userID, 10), Audience: jwt.ClaimStrings{accesstoken.Audience},
				IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			},
		}).SignedString(priv)
		require.NoError(t, err)
		return func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+token) }
	}
}

func TestCreateVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos", `{"title":"Tes","content_type":"video/mp4","size_bytes":1234}`, as(7))

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{
		"video": {"id":"42","owner_id":"7","title":"Tes","description":"","status":"PENDING_UPLOAD",
		          "content_type":"video/mp4","size_bytes":1234,"created_at":"2026-10-29T04:00:00Z","view_count":0,"comment_count":0,"like_count":0},
		"upload": {"url":"http://localhost:9000/videos/raw/abc/source?X-Amz-Signature=x","method":"PUT",
		           "headers":{"Content-Type":"video/mp4"},"expires_at":"2026-10-29T04:15:00Z"}
	}`, rec.Body.String())
	// Id pemilik diambil dari access token, bukan dari body.
	assert.Equal(t, int64(7), fake.lastCreate.GetRequesterId())
	assert.Equal(t, int64(1234), fake.lastCreate.GetSizeBytes())

	rec = do(r, http.MethodPost, "/api/v1/videos", `{"title":"Tes"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "wajib login")
}

func TestCompleteVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodPost, "/api/v1/videos/42/complete", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"UPLOADED"`)
	assert.Contains(t, rec.Body.String(), `"uploaded_at":"2026-10-29T04:01:00Z"`)
	assert.Equal(t, int64(42), fake.lastComplete.GetVideoId())
	assert.Equal(t, int64(7), fake.lastComplete.GetRequesterId())

	rec = do(r, http.MethodPost, "/api/v1/videos/abc/complete", "", as(7))
	assert.Equal(t, http.StatusNotFound, rec.Code, "id bukan angka")

	fake.err = status.Error(codes.FailedPrecondition, "file video belum selesai diunggah")
	rec = do(r, http.MethodPost, "/api/v1/videos/42/complete", "", as(7))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"FAILED_PRECONDITION"`)
}

func TestGetVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	// Tanpa login boleh, viewer_id = 0. Video READY: ada URL HLS dan
	// thumbnail, tapi tidak ada URL file asli.
	rec := do(r, http.MethodGet, "/api/v1/videos/42", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(0), fake.lastGet.GetViewerId())
	assert.JSONEq(t, `{
		"video": {"id":"42","owner_id":"7","title":"Tes","description":"","status":"READY",
		          "content_type":"video/mp4","size_bytes":1234,"created_at":"2026-10-29T04:00:00Z",
		          "uploaded_at":"2026-10-29T04:01:00Z","duration_ms":20023,"renditions":["360p","720p"],
		          "hls_url":"http://media.ice-stream.localhost:4000/videos/hls/42/master.m3u8",
		          "thumbnail_url":"http://media.ice-stream.localhost:4000/videos/hls/42/thumbnail.jpg",
		          "processed_at":"2026-10-29T04:02:00Z","view_count":5,"comment_count":3,"like_count":0}
	}`, rec.Body.String())

	// Dengan login, viewer_id diisi dari token. Pemilik juga mendapat URL
	// file asli.
	rec = do(r, http.MethodGet, "/api/v1/videos/42", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(7), fake.lastGet.GetViewerId())
	assert.Contains(t, rec.Body.String(), `"original":{"url":"http://localhost:9000/videos/raw/abc/source?X-Amz-Signature=y"`)

	// Token rusak tetap ditolak, supaya frontend tahu harus refresh.
	rec = do(r, http.MethodGet, "/api/v1/videos/42", "", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer rusak")
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	fake.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodGet, "/api/v1/videos/999", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListMyVideos(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/me/videos?page_size=10&page_token=50", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"next_page_token":"42"`)
	assert.Contains(t, rec.Body.String(), `"thumbnail_url":"http://media.ice-stream.localhost:4000/videos/hls/42/thumbnail.jpg"`)
	assert.Contains(t, rec.Body.String(), `"failure_reason":"file tidak bisa dibaca sebagai video"`)
	assert.Equal(t, int64(7), fake.lastList.GetRequesterId())
	assert.Equal(t, int32(10), fake.lastList.GetPageSize())
	assert.Equal(t, "50", fake.lastList.GetPageToken())

	rec = do(r, http.MethodGet, "/api/v1/me/videos?page_size=abc", "", as(7))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(r, http.MethodGet, "/api/v1/me/videos", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListVideosFeed(t *testing.T) {
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake})

	// Tanpa login.
	rec := do(r, http.MethodGet, "/api/v1/videos?page_size=10&page_token=abc", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"hls_url":"http://media.ice-stream.localhost:4000/videos/hls/42/master.m3u8"`)
	assert.Contains(t, rec.Body.String(), `"view_count":5`)
	assert.Contains(t, rec.Body.String(), `"next_page_token":"MTc2MjM4ODk2MDAwMDAwMC40Mg"`)
	assert.Equal(t, int32(10), fake.lastFeed.GetPageSize())
	assert.Equal(t, "abc", fake.lastFeed.GetPageToken())

	rec = do(r, http.MethodGet, "/api/v1/videos?page_size=banyak", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	fake.err = status.Error(codes.InvalidArgument, "page_token: tidak valid")
	rec = do(r, http.MethodGet, "/api/v1/videos?page_token=rusak", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRecordView(t *testing.T) {
	verifier, as := newTokenSigner(t)
	fake := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: fake, Verifier: verifier})

	// User login: penanda dari id user.
	rec := do(r, http.MethodPost, "/api/v1/videos/42/views", "", as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"counted":true,"view_count":6}`, rec.Body.String())
	assert.Equal(t, int64(42), fake.lastView.GetVideoId())
	assert.Equal(t, "u-7", fake.lastView.GetViewerKey())

	// Tanpa login: penanda dari hash IP + User-Agent. Browser yang sama
	// mendapat penanda yang sama, browser lain mendapat penanda lain.
	firefox := func(req *http.Request) { req.Header.Set("User-Agent", "Firefox") }
	chrome := func(req *http.Request) { req.Header.Set("User-Agent", "Chrome") }
	do(r, http.MethodPost, "/api/v1/videos/42/views", "", firefox)
	key1 := fake.lastView.GetViewerKey()
	do(r, http.MethodPost, "/api/v1/videos/42/views", "", firefox)
	key2 := fake.lastView.GetViewerKey()
	do(r, http.MethodPost, "/api/v1/videos/42/views", "", chrome)
	key3 := fake.lastView.GetViewerKey()
	assert.Regexp(t, `^a-[0-9a-f]{32}$`, key1)
	assert.Equal(t, key1, key2)
	assert.NotEqual(t, key1, key3)

	rec = do(r, http.MethodPost, "/api/v1/videos/abc/views", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	fake.err = status.Error(codes.NotFound, "video tidak ditemukan")
	rec = do(r, http.MethodPost, "/api/v1/videos/42/views", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	fake.err = status.Error(codes.Unavailable, "pencatatan views sedang tidak tersedia")
	rec = do(r, http.MethodPost, "/api/v1/videos/42/views", "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func (f *fakeVideoClient) SearchVideos(_ context.Context, in *videov1.SearchVideosRequest, _ ...grpc.CallOption) (*videov1.SearchVideosResponse, error) {
	f.lastSearch = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.SearchVideosResponse{Videos: []*videov1.Video{testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)}, NextPageToken: "czE"}, nil
}

func TestSearchVideos(t *testing.T) {
	videos, users := &fakeVideoClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users})

	rec := do(r, http.MethodGet, "/api/v1/search/videos?q=belajar+golang&page_size=5&page_token=abc", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "belajar golang", videos.lastSearch.GetQuery())
	assert.Equal(t, int32(5), videos.lastSearch.GetPageSize())
	assert.Equal(t, "abc", videos.lastSearch.GetPageToken())
	assert.Contains(t, rec.Body.String(), `"next_page_token":"czE"`)
	assert.Contains(t, rec.Body.String(), `"id":"42"`)
	assert.Contains(t, rec.Body.String(), `"owner":{`, "pemilik video ikut diisi seperti di feed")

	rec = do(r, http.MethodGet, "/api/v1/search/videos", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "q wajib")

	videos.err = status.Error(codes.InvalidArgument, "query: isi minimal satu kata dengan 2 huruf atau lebih")
	rec = do(r, http.MethodGet, "/api/v1/search/videos?q=a", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
