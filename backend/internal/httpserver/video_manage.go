package httpserver

import (
	"context"
	"errors"
	"io"
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
	// Langkah 26: lihat createVideoRequest. draft false = "Terbitkan".
	Visibility *string `json:"visibility"`
	Draft      *bool   `json:"draft"`
	Language   *string `json:"language"`
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
		Draft:       req.Draft,
		Language:    req.Language,
	}
	if req.Visibility != nil {
		vis, ok := parseVideoVisibility(c, *req.Visibility)
		if !ok {
			return
		}
		if vis == videov1.VideoVisibility_VIDEO_VISIBILITY_UNSPECIFIED {
			// Saat mengubah, "" bukan pilihan: jangan diam-diam jadi PUBLIC.
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "visibility harus PUBLIC, UNLISTED, atau PRIVATE")
			return
		}
		upd.Visibility = &vis
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
	c.JSON(http.StatusOK, gin.H{"video": toOwnerVideoResponse(resp.GetVideo())})
}

// chooseFrameRequest: body PUT /videos/:id/thumbnail/frame.
type chooseFrameRequest struct {
	// Salah satu thumbnail_frames[].position_ms.
	PositionMs int64 `json:"position_ms"`
}

// chooseThumbnailFrame: PUT /api/v1/videos/:id/thumbnail/frame dengan body
// {"position_ms": 45000}. Wajib login, hanya pemilik video READY. 404 jika
// posisi itu bukan salah satu bingkai buatan transcoder.
func (h *videoHandler) chooseThumbnailFrame(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req chooseFrameRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ChooseThumbnailFrame(ctx, &videov1.ChooseThumbnailFrameRequest{
		RequesterId: userID, VideoId: videoID, PositionMs: req.PositionMs,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"video": toOwnerVideoResponse(resp.GetVideo())})
}

// uploadThumbnail: PUT /api/v1/videos/:id/thumbnail dengan isi file gambar
// apa adanya sebagai body (JPEG, PNG, atau WebP), sama seperti
// PUT /me/avatar. Di browser:
// fetch(url, {method: "PUT", body: file}). Gambar dipotong 16:9 oleh
// VideoService. Boleh sebelum video selesai diproses.
func (h *videoHandler) uploadThumbnail(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	// MaxBytesReader berhenti membaca setelah 2 MiB, jadi file raksasa
	// tidak pernah masuk ke memori gateway.
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxThumbnailBodyBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		abortError(c, http.StatusRequestEntityTooLarge, "TOO_LARGE", "thumbnail maksimal 2 MiB")
		return
	case err != nil:
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "gagal membaca file")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), avatarCallTimeout)
	defer cancel()
	resp, err := h.client.UploadThumbnail(ctx, &videov1.UploadThumbnailRequest{RequesterId: userID, VideoId: videoID, Image: data})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"video": toOwnerVideoResponse(resp.GetVideo())})
}

// maxThumbnailBodyBytes sama dengan video.MaxThumbnailBytes (2 MiB).
const maxThumbnailBodyBytes = 2 << 20

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
