package httpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

func (f *fakeVideoClient) ListTrendingVideos(_ context.Context, in *videov1.ListTrendingVideosRequest, _ ...grpc.CallOption) (*videov1.ListTrendingVideosResponse, error) {
	f.lastTrending = in
	if f.err != nil {
		return nil, f.err
	}
	return &videov1.ListTrendingVideosResponse{Videos: []*videov1.TrendingVideo{
		{Video: testVideo(videov1.VideoStatus_VIDEO_STATUS_READY), RecentViews: 4},
	}}, nil
}

func TestSearchSortAndDurationParams(t *testing.T) {
	videos, users := &fakeVideoClient{}, &fakeUserClient{}
	r := newTestRouter(t, Deps{Videos: videos, Users: users})

	rec := do(r, http.MethodGet, "/api/v1/search/videos?q=golang", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, videov1.SearchSort_SEARCH_SORT_RELEVANCE, videos.lastSearch.GetSort(), "bawaan: relevan")
	assert.Equal(t, videov1.DurationFilter_DURATION_FILTER_UNSPECIFIED, videos.lastSearch.GetDuration())

	rec = do(r, http.MethodGet, "/api/v1/search/videos?q=golang&sort=danmaku&duration=long", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, videov1.SearchSort_SEARCH_SORT_MOST_DANMAKU, videos.lastSearch.GetSort())
	assert.Equal(t, videov1.DurationFilter_DURATION_FILTER_LONG, videos.lastSearch.GetDuration())

	for _, q := range []string{"sort=VIEWS", "sort=popular", "duration=10"} {
		videos.lastSearch = nil
		rec = do(r, http.MethodGet, "/api/v1/search/videos?q=golang&"+q, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
		assert.Nil(t, videos.lastSearch, "nilai salah tidak sampai ke VideoService: %s", q)
	}
}

func TestChannelVideosSort(t *testing.T) {
	users, videos := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: videos})

	rec := do(r, http.MethodGet, "/api/v1/users/budi/videos", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, videov1.VideoSort_VIDEO_SORT_NEWEST, videos.lastFeed.GetSort())

	rec = do(r, http.MethodGet, "/api/v1/users/budi/videos?sort=popular", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, videov1.VideoSort_VIDEO_SORT_POPULAR, videos.lastFeed.GetSort())
	assert.Equal(t, int64(7), videos.lastFeed.GetOwnerId())

	rec = do(r, http.MethodGet, "/api/v1/users/budi/videos?sort=views", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTrending(t *testing.T) {
	users, videos := &fakeUserClient{}, &fakeVideoClient{}
	r := newTestRouter(t, Deps{Users: users, Videos: videos})

	rec := do(r, http.MethodGet, "/api/v1/trending/videos?category=game&limit=5", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "game", videos.lastTrending.GetCategory())
	assert.Equal(t, int32(5), videos.lastTrending.GetLimit())
	body := rec.Body.String()
	assert.Contains(t, body, `"recent_views":4`)
	assert.Contains(t, body, `"id":"42"`, "field video ada di level yang sama")
	assert.Contains(t, body, `"owner":{"id":"7"`)

	rec = do(r, http.MethodGet, "/api/v1/trending/videos?limit=lima", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
