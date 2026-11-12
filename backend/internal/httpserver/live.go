package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// Langkah 25a: siaran langsung.
//
// Gateway hanya meneruskan ke LiveService (proses VideoService). Video
// siarannya tidak pernah lewat gateway: OBS mengirim ke MediaMTX lewat
// RTMP, dan browser memutar HLS langsung dari MediaMTX (playback_url).

// Body JSON siaran hanya berisi judul dan kategori.
const maxLiveBodyBytes = 4 << 10 // 4 KiB

type liveHandler struct {
	client livev1.LiveServiceClient
	users  userv1.UserServiceClient
	logger *slog.Logger
}

type liveStreamResponse struct {
	ID       string         `json:"id"`
	OwnerID  string         `json:"owner_id"`
	Owner    *ownerResponse `json:"owner,omitempty"`
	Title    string         `json:"title"`
	Category string         `json:"category"`
	// Status "LIVE" atau "ENDED".
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// PlaybackURL dibuka hls.js. Hanya ada selama LIVE.
	PlaybackURL string `json:"playback_url,omitempty"`
	ViewerCount int64  `json:"viewer_count"`
	PeakViewers int64  `json:"peak_viewers"`
}

type liveChannelResponse struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	// IngestURL diisi di kolom "Server" OBS.
	IngestURL          string              `json:"ingest_url"`
	HasStreamKey       bool                `json:"has_stream_key"`
	StreamKeyCreatedAt *time.Time          `json:"stream_key_created_at,omitempty"`
	Current            *liveStreamResponse `json:"current"`
}

type updateLiveChannelRequest struct {
	Title    string `json:"title"`
	Category string `json:"category"`
}

var liveStatusNames = map[livev1.LiveStatus]string{
	livev1.LiveStatus_LIVE_STATUS_LIVE:  "LIVE",
	livev1.LiveStatus_LIVE_STATUS_ENDED: "ENDED",
}

func toLiveStreamResponse(s *livev1.LiveStream) liveStreamResponse {
	out := liveStreamResponse{
		ID:          strconv.FormatInt(s.GetId(), 10),
		OwnerID:     strconv.FormatInt(s.GetOwnerId(), 10),
		Title:       s.GetTitle(),
		Category:    s.GetCategory(),
		Status:      liveStatusNames[s.GetStatus()],
		StartedAt:   s.GetStartedAt().AsTime(),
		PlaybackURL: s.GetPlaybackUrl(),
		ViewerCount: s.GetViewerCount(),
		PeakViewers: s.GetPeakViewers(),
	}
	if s.GetEndedAt() != nil {
		t := s.GetEndedAt().AsTime()
		out.EndedAt = &t
	}
	return out
}

func toLiveChannelResponse(ch *livev1.LiveChannel) liveChannelResponse {
	out := liveChannelResponse{
		Title:        ch.GetTitle(),
		Category:     ch.GetCategory(),
		IngestURL:    ch.GetIngestUrl(),
		HasStreamKey: ch.GetHasStreamKey(),
	}
	if ch.GetStreamKeyCreatedAt() != nil {
		t := ch.GetStreamKeyCreatedAt().AsTime()
		out.StreamKeyCreatedAt = &t
	}
	if ch.GetCurrent() != nil {
		cur := toLiveStreamResponse(ch.GetCurrent())
		out.Current = &cur
	}
	return out
}

// withOwners menambahkan info pemilik (nama, foto) ke setiap siaran dengan
// satu panggilan BatchGetProfiles. Jika UserService gagal, owner dikosongkan.
func (h *liveHandler) withOwners(ctx context.Context, streams []*livev1.LiveStream) []liveStreamResponse {
	ids := make([]int64, len(streams))
	for i, s := range streams {
		ids[i] = s.GetOwnerId()
	}
	owners := lookupProfiles(ctx, h.users, h.logger, ids)
	out := make([]liveStreamResponse, len(streams))
	for i, s := range streams {
		out[i] = toLiveStreamResponse(s)
		out[i].Owner = owners[s.GetOwnerId()]
	}
	return out
}

// list: GET /api/v1/live?category=game&limit=8  (tanpa login)
// Siaran yang sedang LIVE, terbaru dulu.
func (h *liveHandler) list(c *gin.Context) {
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

	resp, err := h.client.ListLiveStreams(ctx, &livev1.ListLiveStreamsRequest{Limit: limit, Category: c.Query("category")})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"streams": h.withOwners(c.Request.Context(), resp.GetStreams())})
}

// get: GET /api/v1/live/:id  (tanpa login)
func (h *liveHandler) get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "siaran tidak ditemukan")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetLiveStream(ctx, &livev1.GetLiveStreamRequest{StreamId: id})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"stream": h.withOwners(c.Request.Context(), []*livev1.LiveStream{resp.GetStream()})[0]})
}

// myChannel: GET /api/v1/me/live  (wajib login)
// Pengaturan siaran milik user, dibuat otomatis saat pertama kali dibuka.
func (h *liveHandler) myChannel(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetMyChannel(ctx, &livev1.GetMyChannelRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"channel": toLiveChannelResponse(resp.GetChannel())})
}

// updateMyChannel: PATCH /api/v1/me/live  {"title":"...","category":"game"}
func (h *liveHandler) updateMyChannel(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req updateLiveChannelRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.UpdateMyChannel(ctx, &livev1.UpdateMyChannelRequest{RequesterId: userID, Title: req.Title, Category: req.Category})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"channel": toLiveChannelResponse(resp.GetChannel())})
}

// resetStreamKey: POST /api/v1/me/live/stream-key
// Membuat stream key baru. Key hanya ada di jawaban ini, jadi
// Cache-Control: no-store supaya browser atau proxy tidak menyimpannya.
func (h *liveHandler) resetStreamKey(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ResetStreamKey(ctx, &livev1.ResetStreamKeyRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"channel": toLiveChannelResponse(resp.GetChannel()), "stream_key": resp.GetStreamKey()})
}
