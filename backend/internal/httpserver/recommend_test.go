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

// Langkah 27 (recommend.go, search.go: searchCountsHandler).

func (f *fakeVideoClient) ListRecommendedVideos(_ context.Context, in *videov1.ListRecommendedVideosRequest, _ ...grpc.CallOption) (*videov1.ListRecommendedVideosResponse, error) {
	f.step27.recommend = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListRecommendedVideosResponse{Videos: []*videov1.Video{testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)}}, nil
}

func (f *fakeVideoClient) GetChannelSummary(_ context.Context, in *videov1.GetChannelSummaryRequest, _ ...grpc.CallOption) (*videov1.GetChannelSummaryResponse, error) {
	f.step27.summary = in
	if f.step27.summaryErr != nil {
		return nil, f.step27.summaryErr
	}
	return &videov1.GetChannelSummaryResponse{VideoCount: 3, TotalViews: 1500, TotalLikes: 40, FeaturedVideo: testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)}, nil
}

func (f *fakeVideoClient) SetFeaturedVideo(_ context.Context, in *videov1.SetFeaturedVideoRequest, _ ...grpc.CallOption) (*videov1.SetFeaturedVideoResponse, error) {
	f.step27.featured = in
	if f.err != nil {
		return nil, f.err
	}
	resp := &videov1.SetFeaturedVideoResponse{}
	if in.GetVideoId() != 0 {
		resp.FeaturedVideo = testVideo(videov1.VideoStatus_VIDEO_STATUS_READY)
	}
	return resp, nil
}

// fakeStep27 menyimpan request langkah 27 terakhir.
type fakeStep27 struct {
	recommend  *videov1.ListRecommendedVideosRequest
	summary    *videov1.GetChannelSummaryRequest
	summaryErr error
	featured   *videov1.SetFeaturedVideoRequest
}

func TestRecommendedVideos(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos, users := &fakeVideoClient{}, &fakeUserClient{channelIDs: []int64{8, 9}}
	r := newTestRouter(t, Deps{Videos: videos, Users: users, Verifier: verifier})

	rec := do(r, http.MethodGet, "/api/v1/videos/recommended?seed=18446744073709551615&category=game&limit=12", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, uint64(18446744073709551615), videos.step27.recommend.GetSeed())
	assert.Equal(t, "game", videos.step27.recommend.GetCategory())
	assert.Equal(t, int32(12), videos.step27.recommend.GetLimit())
	assert.Zero(t, videos.step27.recommend.GetViewerId())
	assert.Empty(t, videos.step27.recommend.GetFollowedOwnerIds())
	assert.Nil(t, users.lastChannelIDs, "tamu: langganan tidak ditanya")
	assert.Contains(t, rec.Body.String(), `"videos":[{"id":"42"`)
	assert.Contains(t, rec.Body.String(), `"owner":{"id":"7","username":"budi"`)
	assert.Nil(t, videos.lastGet, "tidak tertukar dengan GET /videos/:id")

	rec = do(r, http.MethodGet, "/api/v1/videos/recommended", "", as(5))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(5), videos.step27.recommend.GetViewerId())
	assert.Equal(t, []int64{8, 9}, videos.step27.recommend.GetFollowedOwnerIds())

	for _, q := range []string{"seed=-1", "seed=abc", "limit=0", "limit=x"} {
		assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/videos/recommended?"+q, "").Code, q)
	}
	videos.err = status.Error(codes.InvalidArgument, "limit: maksimal 24")
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/videos/recommended?limit=30", "").Code)
}

func TestChannelPageSummary(t *testing.T) {
	videos, users := &fakeVideoClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users})

	rec := do(r, http.MethodGet, "/api/v1/users/budi", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(7), videos.step27.summary.GetOwnerId())
	assert.Contains(t, rec.Body.String(), `"stats":{"video_count":3,"total_views":1500,"total_likes":40}`)
	assert.Contains(t, rec.Body.String(), `"featured_video":{"id":"42"`)

	// VideoService gagal: profil tetap tampil, tanpa angka.
	videos.step27.summaryErr = status.Error(codes.Unavailable, "mati")
	rec = do(r, http.MethodGet, "/api/v1/users/budi", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "stats")
	assert.Contains(t, rec.Body.String(), `"username":"budi"`)
}

func TestSetFeaturedVideo(t *testing.T) {
	verifier, as := newTokenSigner(t)
	videos := &fakeVideoClient{}
	r := newTestRouter(t, Deps{Videos: videos, Verifier: verifier})

	assert.Equal(t, http.StatusUnauthorized, do(r, http.MethodPut, "/api/v1/me/featured-video", `{"video_id":"42"}`).Code)

	rec := do(r, http.MethodPut, "/api/v1/me/featured-video", `{"video_id":"42"}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, int64(7), videos.step27.featured.GetRequesterId())
	assert.Equal(t, int64(42), videos.step27.featured.GetVideoId())
	assert.Contains(t, rec.Body.String(), `"featured_video":{"id":"42"`)

	rec = do(r, http.MethodPut, "/api/v1/me/featured-video", `{"video_id":null}`, as(7))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, videos.step27.featured.GetVideoId())
	assert.JSONEq(t, `{"featured_video":null}`, rec.Body.String())

	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodPut, "/api/v1/me/featured-video", `{"video_id":"abc"}`, as(7)).Code)
	videos.err = status.Error(codes.InvalidArgument, "video_id: hanya video publik yang sudah terbit yang bisa dijadikan unggulan")
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodPut, "/api/v1/me/featured-video", `{"video_id":"43"}`, as(7)).Code)
}

func TestSearchCounts(t *testing.T) {
	videos, users, lives := &fakeVideoClient{}, &fakeUserClient{}, &fakeLiveClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users, LiveStreams: lives})

	rec := do(r, http.MethodGet, "/api/v1/search/counts?q=golang&duration=short", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"video":{"count":1000,"capped":true},
		"channel":{"count":2,"capped":false},
		"playlist":{"count":1,"capped":false},
		"live":{"count":1,"capped":false}}`, rec.Body.String())
	assert.Equal(t, int32(1), videos.lastSearch.GetPageSize())
	assert.Equal(t, videov1.DurationFilter_DURATION_FILTER_SHORT, videos.lastSearch.GetDuration())
	assert.Equal(t, "golang", users.lastSearch.GetQuery())
	assert.Equal(t, "golang", lives.lastList.GetQuery())
	assert.Equal(t, int32(50), lives.lastList.GetLimit())

	// Satu service mati: tab itu null, tab lain tetap ada.
	lives.err = status.Error(codes.Unavailable, "mati")
	rec = do(r, http.MethodGet, "/api/v1/search/counts?q=golang", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"live":null`)
	assert.Contains(t, rec.Body.String(), `"channel":{"count":2`)

	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/counts", "").Code)
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/counts?q=a&duration=x", "").Code)
	videos.err = status.Error(codes.InvalidArgument, "query: isi minimal satu kata dengan 2 huruf atau lebih")
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/api/v1/search/counts?q=a", "").Code)
}
