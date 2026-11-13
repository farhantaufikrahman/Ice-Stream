package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// notificationHandler melayani /api/v1/me/notifications. Notifikasi
// disimpan VideoService (tabel notifications ada di schema VIDEO_SVC).
type notificationHandler struct {
	videos videov1.VideoServiceClient
	users  userv1.UserServiceClient
	logger *slog.Logger
}

type notificationResponse struct {
	// ID dikirim sebagai string, sama seperti id video dan komentar.
	ID string `json:"id"`
	// Kind: "NEW_VIDEO" (kanal yang diikuti mengunggah video), atau untuk
	// pemilik video "VIDEO_BLOCKED" / "VIDEO_RESTORED" (langkah 28).
	Kind string `json:"kind"`
	// ModerationReason hanya untuk VIDEO_BLOCKED, misalnya "spam".
	ModerationReason string        `json:"moderation_reason,omitempty"`
	Video            videoResponse `json:"video"`
	// Read = false: tampilkan titik "belum dibaca".
	Read      bool       `json:"read"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type listNotificationsResponse struct {
	Notifications []notificationResponse `json:"notifications"`
	// Kosong jika sudah halaman terakhir.
	NextPageToken string `json:"next_page_token"`
}

type unreadCountResponse struct {
	// 0 sampai 100. Tampilkan "99+" jika lebih dari 99.
	UnreadCount int64 `json:"unread_count"`
}

type markReadResponse struct {
	// Jumlah notifikasi yang baru ditandai dibaca.
	Updated     int64 `json:"updated"`
	UnreadCount int64 `json:"unread_count"`
}

// list: GET /api/v1/me/notifications?page_size=20&page_token=...
// Wajib login. Notifikasi terbaru tampil paling atas.
func (h *notificationHandler) list(c *gin.Context) {
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

	resp, err := h.videos.ListNotifications(ctx, &videov1.ListNotificationsRequest{
		RequesterId: userID,
		PageSize:    pageSize,
		PageToken:   c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	items := resp.GetNotifications()
	videos := make([]*videov1.Video, len(items))
	for i, n := range items {
		videos[i] = n.GetVideo()
	}
	// Nama dan foto channel, sama seperti feed dan riwayat.
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, videos)

	body := listNotificationsResponse{Notifications: make([]notificationResponse, 0, len(items)), NextPageToken: resp.GetNextPageToken()}
	for _, n := range items {
		v := toVideoResponse(n.GetVideo())
		v.Owner = owners[n.GetVideo().GetOwnerId()]
		body.Notifications = append(body.Notifications, notificationResponse{
			ID:               strconv.FormatInt(n.GetId(), 10),
			Kind:             n.GetKind(),
			ModerationReason: reportReasonNames[n.GetModerationReason()],
			Video:            v,
			Read:             n.GetReadAt() != nil,
			ReadAt:           optionalTime(n.GetReadAt()),
			CreatedAt:        n.GetCreatedAt().AsTime(),
		})
	}
	c.JSON(http.StatusOK, body)
}

// unreadCount: GET /api/v1/me/notifications/unread-count. Wajib login.
// Dipanggil frontend secara berkala (misalnya tiap 60 detik) untuk angka
// di ikon lonceng.
func (h *notificationHandler) unreadCount(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.videos.CountUnreadNotifications(ctx, &videov1.CountUnreadNotificationsRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, unreadCountResponse{UnreadCount: resp.GetUnreadCount()})
}

// markRead: POST /api/v1/me/notifications/:id/read. Wajib login. Dipanggil
// saat user mengklik satu notifikasi. Aman diulang; notifikasi milik orang
// lain tidak berubah (updated = 0).
func (h *notificationHandler) markRead(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "notifikasi tidak ditemukan")
		return
	}
	h.mark(c, id)
}

// markAllRead: POST /api/v1/me/notifications/read-all. Wajib login.
// Tombol "Tandai semua sudah dibaca".
func (h *notificationHandler) markAllRead(c *gin.Context) {
	h.mark(c, 0)
}

func (h *notificationHandler) mark(c *gin.Context, notificationID int64) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.videos.MarkNotificationsRead(ctx, &videov1.MarkNotificationsReadRequest{
		RequesterId:    userID,
		NotificationId: notificationID,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, markReadResponse{Updated: resp.GetUpdated(), UnreadCount: resp.GetUnreadCount()})
}
