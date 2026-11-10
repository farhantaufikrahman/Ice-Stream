package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Body PUT /me/history/:id hanya {"position_ms": 123456}.
const maxHistoryBodyBytes = 1 << 10 // 1 KiB

type saveProgressRequest struct {
	// Pointer supaya body {} (lupa mengirim position_ms) bisa dibedakan dari
	// posisi 0. Di browser: Math.floor(video.currentTime * 1000).
	PositionMs *int64 `json:"position_ms"`
}

type saveProgressResponse struct {
	PositionMs int64     `json:"position_ms"`
	WatchedAt  time.Time `json:"watched_at"`
}

type historyEntryResponse struct {
	Video videoResponse `json:"video"`
	// PositionMs dibagi video.duration_ms = panjang garis progres.
	PositionMs int64     `json:"position_ms"`
	WatchedAt  time.Time `json:"watched_at"`
}

type listHistoryResponse struct {
	Entries []historyEntryResponse `json:"entries"`
	// Kosong jika sudah halaman terakhir.
	NextPageToken string `json:"next_page_token"`
}

// saveProgress: PUT /api/v1/me/history/:id dengan body {"position_ms": 93500}.
// Wajib login. Player memanggilnya setiap 10 detik selama video diputar,
// dan sekali lagi saat video dijeda atau selesai.
//
// PUT dipakai karena hasilnya sama walau dikirim berkali-kali: "posisi
// saya di video ini sekarang X".
func (h *videoHandler) saveProgress(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req saveProgressRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.PositionMs == nil {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "position_ms wajib diisi")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.SaveWatchProgress(ctx, &videov1.SaveWatchProgressRequest{
		RequesterId: userID,
		VideoId:     videoID,
		PositionMs:  *req.PositionMs,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, saveProgressResponse{PositionMs: resp.GetPositionMs(), WatchedAt: resp.GetWatchedAt().AsTime()})
}

// history: GET /api/v1/me/history?page_size=20&page_token=...
// Wajib login. Video yang terakhir ditonton tampil paling atas.
func (h *videoHandler) history(c *gin.Context) {
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

	resp, err := h.client.ListWatchHistory(ctx, &videov1.ListWatchHistoryRequest{
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

	body := listHistoryResponse{Entries: make([]historyEntryResponse, 0, len(entries)), NextPageToken: resp.GetNextPageToken()}
	for _, e := range entries {
		v := toVideoResponse(e.GetVideo())
		v.Owner = owners[e.GetVideo().GetOwnerId()]
		body.Entries = append(body.Entries, historyEntryResponse{
			Video:      v,
			PositionMs: e.GetPositionMs(),
			WatchedAt:  e.GetWatchedAt().AsTime(),
		})
	}
	c.JSON(http.StatusOK, body)
}

// deleteHistory: DELETE /api/v1/me/history/:id. Wajib login. Menjawab 204
// (tanpa body), juga jika video itu memang tidak ada di riwayat.
func (h *videoHandler) deleteHistory(c *gin.Context) {
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

	if _, err := h.client.DeleteWatchHistory(ctx, &videov1.DeleteWatchHistoryRequest{RequesterId: userID, VideoId: videoID}); err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// clearHistory: DELETE /api/v1/me/history. Wajib login. Menghapus seluruh
// riwayat dan menjawab {"deleted": jumlah video yang dihapus}.
func (h *videoHandler) clearHistory(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ClearWatchHistory(ctx, &videov1.ClearWatchHistoryRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": resp.GetDeleted()})
}
