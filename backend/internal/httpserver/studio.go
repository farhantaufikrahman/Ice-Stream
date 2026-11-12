package httpserver

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// Halaman Komentar dan Danmaku di Creator Studio (langkah 26). Semua
// endpoint wajib login dan hanya berisi data di video milik user itu.

// videoIDQuery membaca ?video_id=... (opsional). 0 = semua video.
func videoIDQuery(c *gin.Context) (int64, bool) {
	raw := c.Query("video_id")
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "video_id tidak valid")
		return 0, false
	}
	return id, true
}

type channelCommentResponse struct {
	commentResponse
	VideoTitle string `json:"video_title"`
}

type listChannelCommentsResponse struct {
	Comments      []channelCommentResponse `json:"comments"`
	NextPageToken string                   `json:"next_page_token"`
}

// listChannel: GET /api/v1/me/comments?video_id=&page_size=&page_token=
// Komentar dan balasan di video milik user, terbaru lebih dulu. Untuk
// menghapus, pakai DELETE /videos/:id/comments/:commentId yang sudah ada
// (pemilik video boleh menghapus komentar apa pun di videonya).
func (h *commentHandler) listChannel(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDQuery(c)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ListChannelComments(ctx, &commentv1.ListChannelCommentsRequest{
		RequesterId: userID,
		VideoId:     videoID,
		PageSize:    pageSize,
		PageToken:   c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	plain := make([]*commentv1.Comment, len(resp.GetComments()))
	for i, cc := range resp.GetComments() {
		plain[i] = cc.GetComment()
	}
	list := h.toListResponse(c.Request.Context(), plain, resp.GetNextPageToken())
	out := listChannelCommentsResponse{Comments: make([]channelCommentResponse, len(list.Comments)), NextPageToken: list.NextPageToken}
	for i, cm := range list.Comments {
		out.Comments[i] = channelCommentResponse{commentResponse: cm, VideoTitle: resp.GetComments()[i].GetVideoTitle()}
	}
	c.JSON(http.StatusOK, out)
}

// channelDanmakuResponse: berbeda dengan danmaku untuk penonton, pemilik
// video melihat pengirimnya (untuk moderasi).
type channelDanmakuResponse struct {
	danmakuResponse
	UserID     string         `json:"user_id"`
	Author     *ownerResponse `json:"author,omitempty"`
	VideoTitle string         `json:"video_title"`
}

type listChannelDanmakuResponse struct {
	Danmaku       []channelDanmakuResponse `json:"danmaku"`
	NextPageToken string                   `json:"next_page_token"`
}

// studioDanmakuHandler melayani /me/danmaku. Dipisah dari danmakuHandler
// karena butuh UserService (nama pengirim) dan tidak butuh hub SSE.
type studioDanmakuHandler struct {
	*danmakuHandler
	// users boleh nil (di test): author lalu tidak dikirim.
	users userv1.UserServiceClient
}

// listChannel: GET /api/v1/me/danmaku?video_id=&page_size=&page_token=
// Danmaku di video milik user, terbaru lebih dulu (page_size maksimal 100).
func (h *studioDanmakuHandler) listChannel(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDQuery(c)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ListChannelDanmaku(ctx, &danmakuv1.ListChannelDanmakuRequest{
		RequesterId: userID,
		VideoId:     videoID,
		PageSize:    pageSize,
		PageToken:   c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	ids := make([]int64, len(resp.GetDanmaku()))
	for i, d := range resp.GetDanmaku() {
		ids[i] = d.GetDanmaku().GetUserId()
	}
	authors := lookupProfiles(c.Request.Context(), h.users, h.logger, ids)
	out := listChannelDanmakuResponse{Danmaku: make([]channelDanmakuResponse, len(ids)), NextPageToken: resp.GetNextPageToken()}
	for i, d := range resp.GetDanmaku() {
		out.Danmaku[i] = channelDanmakuResponse{
			danmakuResponse: toDanmakuResponse(d.GetDanmaku()),
			UserID:          strconv.FormatInt(ids[i], 10),
			Author:          authors[ids[i]],
			VideoTitle:      d.GetVideoTitle(),
		}
	}
	c.JSON(http.StatusOK, out)
}

// remove: DELETE /api/v1/me/danmaku/:id. Pengirim atau pemilik video.
// Menjawab 204; 404 jika tidak ada, 403 jika bukan keduanya.
func (h *studioDanmakuHandler) remove(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "danmaku tidak ditemukan")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	if _, err := h.client.DeleteDanmaku(ctx, &danmakuv1.DeleteDanmakuRequest{RequesterId: userID, DanmakuId: id}); err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// topVideoResponse adalah satu baris "Video terpopuler" di Analitik.
type topVideoResponse struct {
	VideoID      string `json:"video_id"`
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Views        int64  `json:"views"`
}
