package httpserver

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Langkah 24a: urutan dan filter pencarian, tab Terpopuler, Trending.
//
// Di URL dipakai kata pendek yang mudah dibaca ("?sort=views"), lalu
// gateway menerjemahkannya ke enum gRPC. Nilai yang tidak dikenal dijawab
// 400 di sini, sebelum memanggil VideoService.

// searchSortParams: GET /search/videos?sort=...
var searchSortParams = map[string]videov1.SearchSort{
	"":          videov1.SearchSort_SEARCH_SORT_RELEVANCE,
	"relevance": videov1.SearchSort_SEARCH_SORT_RELEVANCE,
	"newest":    videov1.SearchSort_SEARCH_SORT_NEWEST,
	"views":     videov1.SearchSort_SEARCH_SORT_MOST_VIEWED,
	"danmaku":   videov1.SearchSort_SEARCH_SORT_MOST_DANMAKU,
}

// durationParams: GET /search/videos?duration=...
var durationParams = map[string]videov1.DurationFilter{
	"":       videov1.DurationFilter_DURATION_FILTER_UNSPECIFIED,
	"short":  videov1.DurationFilter_DURATION_FILTER_SHORT,
	"medium": videov1.DurationFilter_DURATION_FILTER_MEDIUM,
	"long":   videov1.DurationFilter_DURATION_FILTER_LONG,
}

// channelSortParams: GET /users/:username/videos?sort=...
var channelSortParams = map[string]videov1.VideoSort{
	"":        videov1.VideoSort_VIDEO_SORT_NEWEST,
	"newest":  videov1.VideoSort_VIDEO_SORT_NEWEST,
	"popular": videov1.VideoSort_VIDEO_SORT_POPULAR,
}

// enumQuery membaca parameter query yang nilainya harus salah satu kunci
// allowed. Jika tidak, menjawab 400 dan mengembalikan false.
func enumQuery[T any](c *gin.Context, name string, allowed map[string]T, hint string) (T, bool) {
	v, ok := allowed[c.Query(name)]
	if !ok {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", name+" harus salah satu dari: "+hint)
	}
	return v, ok
}

// trendingVideoResponse adalah video biasa ditambah tayangan 7 hari
// terakhir. Field video "menempel" di level yang sama karena struct
// videoResponse di-embed (tanpa nama field).
type trendingVideoResponse struct {
	videoResponse
	// RecentViews adalah tayangan 7 hari terakhir (WIB), angka urutannya.
	RecentViews int64 `json:"recent_views"`
}

// trending: GET /api/v1/trending/videos?category=game&limit=5
// Tanpa login. Path-nya /trending/videos (bukan /videos/trending) dengan
// alasan yang sama seperti /search/videos: tidak tertukar dengan /videos/:id.
func (h *videoHandler) trending(c *gin.Context) {
	var limit int32
	if v := c.Query("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "limit harus berupa angka")
			return
		}
		limit = int32(n)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListTrendingVideos(ctx, &videov1.ListTrendingVideosRequest{
		Category: c.Query("category"),
		Limit:    limit,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	videos := make([]*videov1.Video, len(resp.GetVideos()))
	for i, t := range resp.GetVideos() {
		videos[i] = t.GetVideo()
	}
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, videos)
	out := make([]trendingVideoResponse, len(videos))
	for i, t := range resp.GetVideos() {
		out[i] = trendingVideoResponse{videoResponse: toVideoResponse(t.GetVideo()), RecentViews: t.GetRecentViews()}
		out[i].Owner = owners[t.GetVideo().GetOwnerId()]
	}
	c.JSON(http.StatusOK, gin.H{"videos": out})
}
