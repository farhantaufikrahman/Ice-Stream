package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// updateVideoRequest memakai *string: nil = field tidak dikirim (jangan
// diubah), "" = dikirim kosong (misalnya menghapus deskripsi). Sama seperti
// updateProfileRequest.
//
// Tags memakai *[]string: nil = tidak dikirim, [] = hapus semua tag.
// encoding/json mengisi pointer hanya jika key-nya ada di body.
type updateVideoRequest struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Category    *string   `json:"category"`
	Tags        *[]string `json:"tags"`
}

// update: PATCH /api/v1/videos/:id dengan body {"title": "...", "description": "...",
// "category": "game", "tags": ["horor"]}.
// Wajib login, hanya pemilik video. Field yang tidak dikirim tidak diubah.
// PATCH (bukan PUT) karena yang dikirim hanya sebagian data video.
func (h *videoHandler) update(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req updateVideoRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	upd := &videov1.UpdateVideoRequest{
		RequesterId: userID,
		VideoId:     videoID,
		Title:       req.Title,
		Description: req.Description,
		Category:    req.Category,
	}
	if req.Tags != nil {
		// Termasuk daftar kosong: TagList tanpa isi = hapus semua tag.
		upd.Tags = &videov1.TagList{Tags: *req.Tags}
	}
	resp, err := h.client.UpdateVideo(ctx, upd)
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"video": toVideoResponse(resp.GetVideo())})
}

// remove: DELETE /api/v1/videos/:id. Wajib login, hanya pemilik video.
// Menjawab 204 (tanpa body). Video yang sedang di-transcode dijawab 409.
func (h *videoHandler) remove(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	// Lebih lama dari panggilan biasa: VideoService juga menghapus semua
	// file HLS video itu di storage sebelum menjawab.
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoDeleteTimeout)
	defer cancel()

	if _, err := h.client.DeleteVideo(ctx, &videov1.DeleteVideoRequest{RequesterId: userID, VideoId: videoID}); err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}
