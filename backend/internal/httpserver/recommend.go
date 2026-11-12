package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Langkah 27: "Rekomendasi untukmu" di Beranda dan halaman kanal (angka
// dan "Video unggulan").

// recommended: GET /api/v1/videos/recommended?seed=123&category=game&limit=12
// Boleh tanpa login. Dengan login, rekomendasi memakai riwayat tonton dan
// kanal yang diikuti. seed (angka 0 sampai 2^64-1) mengubah bagian acak:
// frontend membuat seed baru saat tombol "Muat ulang" ditekan.
func (h *videoHandler) recommended(c *gin.Context) {
	var seed uint64
	if v := c.Query("seed"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "seed harus berupa angka positif")
			return
		}
		seed = n
	}
	var limit int32
	if v := c.Query("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "limit harus berupa angka 1 sampai 24")
			return
		}
		limit = int32(n)
	}
	viewerID := optionalUserID(c)
	followed := followedChannelIDs(c.Request.Context(), h.users, h.logger, viewerID)

	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ListRecommendedVideos(ctx, &videov1.ListRecommendedVideosRequest{
		ViewerId:         viewerID,
		FollowedOwnerIds: followed,
		Category:         c.Query("category"),
		Seed:             seed,
		Limit:            limit,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := toListResponse(resp.GetVideos(), "")
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, resp.GetVideos())
	for i, v := range resp.GetVideos() {
		body.Videos[i].Owner = owners[v.GetOwnerId()]
	}
	c.JSON(http.StatusOK, body)
}

// followedChannelIDs: kanal yang diikuti userID. Rekomendasi tetap jalan
// tanpa sinyal ini jika belum login atau UserService gagal (nil).
func followedChannelIDs(ctx context.Context, users userv1.UserServiceClient, logger *slog.Logger, userID int64) []int64 {
	if userID == 0 || users == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	resp, err := users.ListSubscribedChannelIds(ctx, &userv1.ListSubscribedChannelIdsRequest{RequesterId: userID})
	if err != nil {
		logger.WarnContext(ctx, "gagal mengambil kanal yang diikuti untuk rekomendasi", slog.Any("error", err))
		return nil
	}
	return resp.GetChannelIds()
}

// channelCountsResponse adalah angka di kepala halaman kanal (desain
// Channel: video, total tayangan, suka). Jumlah pengikut sudah ada di
// profil (subscriber_count).
type channelCountsResponse struct {
	VideoCount int64 `json:"video_count"`
	TotalViews int64 `json:"total_views"`
	TotalLikes int64 `json:"total_likes"`
}

// channelSummary mengambil angka kanal dan Video unggulan. ok false jika
// VideoService gagal; halaman kanal tetap tampil tanpa bagian ini.
func (h *videoHandler) channelSummary(ctx context.Context, ownerID int64) (channelCountsResponse, *videoResponse, bool) {
	if h.client == nil {
		// Hanya di test yang tidak memasang VideoService.
		return channelCountsResponse{}, nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	resp, err := h.client.GetChannelSummary(ctx, &videov1.GetChannelSummaryRequest{OwnerId: ownerID})
	if err != nil {
		h.logger.WarnContext(ctx, "gagal mengambil ringkasan kanal", slog.Any("error", err))
		return channelCountsResponse{}, nil, false
	}
	stats := channelCountsResponse{VideoCount: resp.GetVideoCount(), TotalViews: resp.GetTotalViews(), TotalLikes: resp.GetTotalLikes()}
	var featured *videoResponse
	if v := resp.GetFeaturedVideo(); v != nil {
		r := toVideoResponse(v)
		featured = &r
	}
	return stats, featured, true
}

// setFeaturedVideoRequest: video_id kosong atau null = hapus pilihan.
type setFeaturedVideoRequest struct {
	VideoID *string `json:"video_id"`
}

// setFeaturedVideo: PUT /api/v1/me/featured-video {"video_id": "42"}.
// Wajib login. Hanya video publik yang sudah terbit milik sendiri.
func (h *videoHandler) setFeaturedVideo(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req setFeaturedVideoRequest
	if !bindJSON(c, &req) {
		return
	}
	var videoID int64
	if req.VideoID != nil && *req.VideoID != "" {
		n, err := strconv.ParseInt(*req.VideoID, 10, 64)
		if err != nil || n <= 0 {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "video_id tidak valid")
			return
		}
		videoID = n
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.SetFeaturedVideo(ctx, &videov1.SetFeaturedVideoRequest{RequesterId: userID, VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	var featured *videoResponse
	if v := resp.GetFeaturedVideo(); v != nil {
		r := toVideoResponse(v)
		featured = &r
	}
	c.JSON(http.StatusOK, gin.H{"featured_video": featured})
}
