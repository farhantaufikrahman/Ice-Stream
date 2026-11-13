package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Langkah 24b: tombol "Laporkan" di halaman tonton.

// reportReasonParams: nilai "reason" di body JSON, sama dengan
// REPORT_REASONS di frontend (src/lib/report.ts).
var reportReasonParams = map[string]videov1.ReportReason{
	"spam":       videov1.ReportReason_REPORT_REASON_SPAM,
	"sexual":     videov1.ReportReason_REPORT_REASON_SEXUAL,
	"violence":   videov1.ReportReason_REPORT_REASON_VIOLENCE,
	"hate":       videov1.ReportReason_REPORT_REASON_HATE,
	"harassment": videov1.ReportReason_REPORT_REASON_HARASSMENT,
	"misleading": videov1.ReportReason_REPORT_REASON_MISLEADING,
	"copyright":  videov1.ReportReason_REPORT_REASON_COPYRIGHT,
	"other":      videov1.ReportReason_REPORT_REASON_OTHER,
}

// reportReasonNames adalah kebalikan reportReasonParams (enum -> teks
// JSON). Alasan yang tidak dikenal menjadi "".
var reportReasonNames = func() map[videov1.ReportReason]string {
	out := make(map[videov1.ReportReason]string, len(reportReasonParams))
	for name, r := range reportReasonParams {
		out[r] = name
	}
	return out
}()

type reportVideoRequest struct {
	Reason string `json:"reason"`
	// Details opsional, kecuali untuk reason "other". Maksimal 500 karakter.
	Details string `json:"details"`
}

type reportVideoResponse struct {
	ReportedAt time.Time `json:"reported_at"`
}

// report: POST /api/v1/videos/:id/reports  {"reason":"spam","details":"..."}
// Wajib login. Mengirim lagi mengganti laporan sebelumnya dari user yang
// sama, jadi tetap 200 (bukan 201).
func (h *videoHandler) report(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req reportVideoRequest
	if !bindJSON(c, &req) {
		return
	}
	reason, ok := reportReasonParams[req.Reason]
	if !ok {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT",
			"reason harus salah satu dari: spam, sexual, violence, hate, harassment, misleading, copyright, other")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ReportVideo(ctx, &videov1.ReportVideoRequest{
		RequesterId: userID, VideoId: videoID, Reason: reason, Details: req.Details,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, reportVideoResponse{ReportedAt: resp.GetReportedAt().AsTime()})
}
