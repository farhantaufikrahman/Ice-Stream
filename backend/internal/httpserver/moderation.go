package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	moderationv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/moderation/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/accesstoken"
)

// Langkah 28: halaman moderasi admin, di bawah /api/v1/admin.
//
//	GET  /admin/reports                    antrean video yang dilaporkan
//	GET  /admin/reports/:id                laporan OPEN satu video
//	POST /admin/videos/:id/block           {"reason":"spam","note":"..."}
//	POST /admin/videos/:id/dismiss         tolak semua laporan OPEN
//	POST /admin/videos/:id/unblock         pulihkan video
//	GET  /admin/blocked                    video yang sedang diturunkan
//
// Semua route memakai RequireAuth lalu RequireAdmin.

// roleAdmin sama dengan auth.RoleAdmin (tabel users, kolom role).
const roleAdmin = "ADMIN"

// maxModerationBodyBytes: body block hanya alasan dan catatan 500 karakter.
const maxModerationBodyBytes = 4 << 10

// RequireAdmin menolak user yang bukan admin (403). Dipasang SETELAH
// RequireAuth. Role dibaca dari access token, jadi user yang baru dijadikan
// admin harus masuk ulang (atau menunggu token diperbarui, maksimal 15
// menit) sebelum bisa membuka halaman moderasi. Padanannya
// hasRole('ADMIN') di Spring Security, atau middleware cek req.user.role
// di Express.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := CurrentUser(c)
		if !ok {
			abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token tidak ada")
			return
		}
		if !isAdmin(claims) {
			abortError(c, http.StatusForbidden, "PERMISSION_DENIED", "hanya admin yang boleh membuka halaman ini")
			return
		}
		c.Next()
	}
}

func isAdmin(claims accesstoken.Claims) bool {
	return claims.Role == roleAdmin
}

type moderationHandler struct {
	client moderationv1.ModerationServiceClient
	users  userv1.UserServiceClient
	logger *slog.Logger
}

type reasonCountResponse struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

type reportedVideoResponse struct {
	Video           videoResponse         `json:"video"`
	OpenReports     int64                 `json:"open_reports"`
	Reasons         []reasonCountResponse `json:"reasons"`
	FirstReportedAt time.Time             `json:"first_reported_at"`
	LastReportedAt  time.Time             `json:"last_reported_at"`
}

type videoReportResponse struct {
	// Reporter kosong jika UserService sedang mati.
	Reporter   *ownerResponse `json:"reporter,omitempty"`
	ReporterID string         `json:"reporter_id"`
	Reason     string         `json:"reason"`
	Details    string         `json:"details"`
	ReportedAt time.Time      `json:"reported_at"`
}

type blockedVideoResponse struct {
	Video     videoResponse  `json:"video"`
	Reason    string         `json:"reason"`
	Note      string         `json:"note"`
	Moderator *ownerResponse `json:"moderator,omitempty"`
	BlockedAt time.Time      `json:"blocked_at"`
}

type blockVideoRequest struct {
	Reason string `json:"reason"`
	// Note opsional, kecuali untuk reason "other". Maksimal 500 karakter.
	Note string `json:"note"`
}

// listReports: GET /api/v1/admin/reports?page_size=20&page_token=...
func (h *moderationHandler) listReports(c *gin.Context) {
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ListReportedVideos(ctx, &moderationv1.ListReportedVideosRequest{
		PageSize: pageSize, PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	items := resp.GetVideos()
	videos := make([]*videov1.Video, len(items))
	for i, it := range items {
		videos[i] = it.GetVideo()
	}
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, videos)
	out := make([]reportedVideoResponse, len(items))
	for i, it := range items {
		v := toVideoResponse(it.GetVideo())
		v.Owner = owners[it.GetVideo().GetOwnerId()]
		reasons := make([]reasonCountResponse, len(it.GetReasons()))
		for j, rc := range it.GetReasons() {
			reasons[j] = reasonCountResponse{Reason: reportReasonNames[rc.GetReason()], Count: rc.GetCount()}
		}
		out[i] = reportedVideoResponse{
			Video:           v,
			OpenReports:     it.GetOpenReports(),
			Reasons:         reasons,
			FirstReportedAt: it.GetFirstReportedAt().AsTime(),
			LastReportedAt:  it.GetLastReportedAt().AsTime(),
		}
	}
	c.JSON(http.StatusOK, gin.H{"videos": out, "next_page_token": resp.GetNextPageToken()})
}

// listVideoReports: GET /api/v1/admin/reports/:id
func (h *moderationHandler) listVideoReports(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ListVideoReports(ctx, &moderationv1.ListVideoReportsRequest{VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	ids := make([]int64, len(resp.GetReports()))
	for i, r := range resp.GetReports() {
		ids[i] = r.GetReporterId()
	}
	reporters := lookupProfiles(c.Request.Context(), h.users, h.logger, ids)
	out := make([]videoReportResponse, len(resp.GetReports()))
	for i, r := range resp.GetReports() {
		out[i] = videoReportResponse{
			Reporter:   reporters[r.GetReporterId()],
			ReporterID: strconv.FormatInt(r.GetReporterId(), 10),
			Reason:     reportReasonNames[r.GetReason()],
			Details:    r.GetDetails(),
			ReportedAt: r.GetReportedAt().AsTime(),
		}
	}
	c.JSON(http.StatusOK, gin.H{"reports": out})
}

// block: POST /api/v1/admin/videos/:id/block {"reason":"spam","note":"..."}
func (h *moderationHandler) block(c *gin.Context) {
	adminID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req blockVideoRequest
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
	resp, err := h.client.BlockVideo(ctx, &moderationv1.BlockVideoRequest{
		ModeratorId: adminID, VideoId: videoID, Reason: reason, Note: req.Note,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	h.logger.InfoContext(ctx, "video diturunkan admin",
		slog.Int64("video_id", videoID), slog.Int64("admin_id", adminID), slog.String("reason", req.Reason))
	c.JSON(http.StatusOK, gin.H{
		"blocked":        h.toBlockedResponses(c.Request.Context(), []*moderationv1.BlockedVideo{resp.GetBlocked()})[0],
		"closed_reports": resp.GetClosedReports(),
	})
}

// dismiss: POST /api/v1/admin/videos/:id/dismiss (tanpa body).
func (h *moderationHandler) dismiss(c *gin.Context) {
	adminID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.DismissReports(ctx, &moderationv1.DismissReportsRequest{ModeratorId: adminID, VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"closed_reports": resp.GetClosedReports()})
}

// unblock: POST /api/v1/admin/videos/:id/unblock (tanpa body).
func (h *moderationHandler) unblock(c *gin.Context) {
	adminID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.UnblockVideo(ctx, &moderationv1.UnblockVideoRequest{ModeratorId: adminID, VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	h.logger.InfoContext(ctx, "video dipulihkan admin", slog.Int64("video_id", videoID), slog.Int64("admin_id", adminID))
	c.JSON(http.StatusOK, gin.H{"video": toVideoResponse(resp.GetVideo())})
}

// listBlocked: GET /api/v1/admin/blocked?page_size=20&page_token=...
func (h *moderationHandler) listBlocked(c *gin.Context) {
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.client.ListBlockedVideos(ctx, &moderationv1.ListBlockedVideosRequest{
		PageSize: pageSize, PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"videos":          h.toBlockedResponses(c.Request.Context(), resp.GetVideos()),
		"next_page_token": resp.GetNextPageToken(),
	})
}

// toBlockedResponses mengambil nama pemilik video dan admin dengan satu
// panggilan BatchGetProfiles.
func (h *moderationHandler) toBlockedResponses(ctx context.Context, items []*moderationv1.BlockedVideo) []blockedVideoResponse {
	ids := make([]int64, 0, 2*len(items))
	for _, b := range items {
		ids = append(ids, b.GetVideo().GetOwnerId(), b.GetModeratorId())
	}
	profiles := lookupProfiles(ctx, h.users, h.logger, ids)
	out := make([]blockedVideoResponse, len(items))
	for i, b := range items {
		v := toVideoResponse(b.GetVideo())
		v.Owner = profiles[b.GetVideo().GetOwnerId()]
		out[i] = blockedVideoResponse{
			Video:     v,
			Reason:    reportReasonNames[b.GetReason()],
			Note:      b.GetNote(),
			Moderator: profiles[b.GetModeratorId()],
			BlockedAt: b.GetBlockedAt().AsTime(),
		}
	}
	return out
}
