package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

type watchLaterEntryResponse struct {
	Video   videoResponse `json:"video"`
	AddedAt time.Time     `json:"added_at"`
}

type listWatchLaterResponse struct {
	Entries []watchLaterEntryResponse `json:"entries"`
	// Kosong jika sudah halaman terakhir.
	NextPageToken string `json:"next_page_token"`
}

// saveWatchLaterResponse adalah jawaban PUT dan DELETE /me/watch-later/:id.
// Bentuknya sama dengan likeResponse: status terbaru, supaya frontend
// cukup memakai jawaban ini untuk tombol Simpan.
type saveWatchLaterResponse struct {
	Saved bool `json:"saved"`
	// Hanya ada jika saved = true: waktu video pertama kali disimpan.
	AddedAt *time.Time `json:"added_at,omitempty"`
}

// addWatchLater: PUT /api/v1/me/watch-later/:id (tanpa body). Wajib login.
// PUT karena hasilnya sama walau dikirim berkali-kali: "video ini ada di
// Tonton nanti saya".
func (h *videoHandler) addWatchLater(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.AddToWatchLater(ctx, &videov1.AddToWatchLaterRequest{RequesterId: userID, VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	addedAt := resp.GetAddedAt().AsTime()
	c.JSON(http.StatusOK, saveWatchLaterResponse{Saved: true, AddedAt: &addedAt})
}

// removeWatchLater: DELETE /api/v1/me/watch-later/:id. Wajib login. Juga
// menjawab 200 jika video itu memang tidak tersimpan.
func (h *videoHandler) removeWatchLater(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	if _, err := h.client.RemoveFromWatchLater(ctx, &videov1.RemoveFromWatchLaterRequest{RequesterId: userID, VideoId: videoID}); err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, saveWatchLaterResponse{Saved: false})
}

// watchLater: GET /api/v1/me/watch-later?page_size=20&page_token=...
// Wajib login. Video yang terakhir disimpan tampil paling atas.
func (h *videoHandler) watchLater(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListWatchLater(ctx, &videov1.ListWatchLaterRequest{
		RequesterId: userID,
		PageSize:    pageSize,
		PageToken:   c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	entries := resp.GetEntries()
	videos := make([]*videov1.Video, len(entries))
	for i, e := range entries {
		videos[i] = e.GetVideo()
	}
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, videos)

	body := listWatchLaterResponse{Entries: make([]watchLaterEntryResponse, 0, len(entries)), NextPageToken: resp.GetNextPageToken()}
	for _, e := range entries {
		v := toVideoResponse(e.GetVideo())
		v.Owner = owners[e.GetVideo().GetOwnerId()]
		body.Entries = append(body.Entries, watchLaterEntryResponse{Video: v, AddedAt: e.GetAddedAt().AsTime()})
	}
	c.JSON(http.StatusOK, body)
}
