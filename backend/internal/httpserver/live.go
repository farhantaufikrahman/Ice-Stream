package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
)

// Langkah 25a: siaran langsung. Langkah 25b: token siaran dari browser.
// Langkah 25c: pencarian siaran dan chat live.
//
// Gateway hanya meneruskan ke LiveService (proses VideoService). Video
// siarannya tidak pernah lewat gateway: OBS mengirim ke MediaMTX lewat
// RTMP, dan browser memutar HLS langsung dari MediaMTX (playback_url).

// Body JSON siaran hanya berisi judul dan kategori.
const maxLiveBodyBytes = 4 << 10 // 4 KiB

type liveHandler struct {
	client livev1.LiveServiceClient
	users  userv1.UserServiceClient
	// chatHub membagikan chat baru ke penonton (SSE). Boleh nil (test);
	// endpoint chat live lalu menjawab 503.
	chatHub   *live.Hub
	heartbeat time.Duration
	logger    *slog.Logger
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
	// Chat hanya ada di GET /live/:id (langkah 27): pilihan chat kanal
	// saat ini, untuk keterangan di panel chat.
	Chat *liveChatOptionsResponse `json:"chat,omitempty"`
}

type liveChatOptionsResponse struct {
	SlowMode      bool `json:"slow_mode"`
	FollowersOnly bool `json:"followers_only"`
}

type liveChannelResponse struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	// IngestURL diisi di kolom "Server" OBS.
	IngestURL          string              `json:"ingest_url"`
	HasStreamKey       bool                `json:"has_stream_key"`
	StreamKeyCreatedAt *time.Time          `json:"stream_key_created_at,omitempty"`
	Current            *liveStreamResponse `json:"current"`
	// Pilihan siaran (langkah 27, desain GoLive "Detail siaran").
	RecordEnabled bool `json:"record_enabled"`
	SlowMode      bool `json:"slow_mode"`
	FollowersOnly bool `json:"followers_only"`
	// LastRecording: rekaman siaran terakhir; null jika belum pernah ada.
	LastRecording *recordingResponse `json:"last_recording"`
}

// recordingResponse adalah status rekaman satu siaran.
type recordingResponse struct {
	StreamID string `json:"stream_id"`
	// Status: PENDING, IMPORTING, DONE, atau FAILED.
	Status string `json:"status"`
	// VideoID hanya ada jika DONE: video draf hasil rekaman.
	VideoID string     `json:"video_id,omitempty"`
	Error   string     `json:"error,omitempty"`
	EndedAt *time.Time `json:"ended_at,omitempty"`
}

type publishTokenResponse struct {
	// WHIPURL adalah alamat yang di-POST browser (offer SDP) untuk mulai
	// siaran WebRTC.
	WHIPURL string `json:"whip_url"`
	// Token dikirim di header "Authorization: Bearer <token>".
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type chatMessageResponse struct {
	ID       string `json:"id"`
	StreamID string `json:"stream_id"`
	// User boleh kosong jika UserService sedang gagal.
	User      *ownerResponse `json:"user"`
	Body      string         `json:"body"`
	Color     string         `json:"color"`
	CreatedAt time.Time      `json:"created_at"`
}

type sendChatRequest struct {
	Body string `json:"body"`
	// Color opsional, format "#RRGGBB". Default "#FFFFFF".
	Color string `json:"color"`
}

type updateLiveChannelRequest struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	// Pilihan siaran (langkah 27). Tidak dikirim = tidak diubah.
	RecordEnabled *bool `json:"record_enabled"`
	SlowMode      *bool `json:"slow_mode"`
	FollowersOnly *bool `json:"followers_only"`
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
		Title:         ch.GetTitle(),
		Category:      ch.GetCategory(),
		IngestURL:     ch.GetIngestUrl(),
		HasStreamKey:  ch.GetHasStreamKey(),
		RecordEnabled: ch.GetRecordEnabled(),
		SlowMode:      ch.GetSlowMode(),
		FollowersOnly: ch.GetFollowersOnly(),
	}
	if r := ch.GetLastRecording(); r != nil {
		rec := &recordingResponse{StreamID: strconv.FormatInt(r.GetStreamId(), 10), Status: r.GetStatus(), Error: r.GetError()}
		if r.GetVideoId() > 0 {
			rec.VideoID = strconv.FormatInt(r.GetVideoId(), 10)
		}
		if r.GetEndedAt() != nil {
			t := r.GetEndedAt().AsTime()
			rec.EndedAt = &t
		}
		out.LastRecording = rec
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

// list: GET /api/v1/live?category=game&limit=8&q=ngoding  (tanpa login)
// Siaran yang sedang LIVE, terbaru dulu. q mencari di judul (tab Live di
// halaman pencarian, langkah 25c).
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

	resp, err := h.client.ListLiveStreams(ctx, &livev1.ListLiveStreamsRequest{Limit: limit, Category: c.Query("category"), Query: c.Query("q")})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"streams": h.withOwners(c.Request.Context(), resp.GetStreams())})
}

// get: GET /api/v1/live/:id  (tanpa login)
func (h *liveHandler) get(c *gin.Context) {
	id, ok := liveIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetLiveStream(ctx, &livev1.GetLiveStreamRequest{StreamId: id})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	out := h.withOwners(c.Request.Context(), []*livev1.LiveStream{resp.GetStream()})[0]
	out.Chat = &liveChatOptionsResponse{SlowMode: resp.GetStream().GetSlowMode(), FollowersOnly: resp.GetStream().GetFollowersOnly()}
	c.JSON(http.StatusOK, gin.H{"stream": out})
}

// requireLiveBroadcaster dipasang di /me/live saat LIVE_BROADCAST_ADMIN_ONLY
// menyala (langkah 29): backend online lewat Cloudflare Tunnel hanya
// meneruskan HTTP, jadi siaran (RTMP/WebRTC) hanya bisa dari laptop admin.
// Kode LIVE_BROADCAST_ADMIN_ONLY dibaca frontend untuk menampilkan keterangan.
// Dipanggil setelah requireAuth, jadi claims pasti ada.
func requireLiveBroadcaster() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := CurrentUser(c)
		if !ok {
			abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token tidak ada")
			return
		}
		if !isAdmin(claims) {
			abortError(c, http.StatusForbidden, "LIVE_BROADCAST_ADMIN_ONLY", "siaran langsung belum tersedia untuk akun ini")
			return
		}
		c.Next()
	}
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

	resp, err := h.client.UpdateMyChannel(ctx, &livev1.UpdateMyChannelRequest{
		RequesterId:   userID,
		Title:         req.Title,
		Category:      req.Category,
		RecordEnabled: req.RecordEnabled,
		SlowMode:      req.SlowMode,
		FollowersOnly: req.FollowersOnly,
	})
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

// createPublishToken: POST /api/v1/me/live/publish-token  (langkah 25b)
// Token pendek untuk siaran dari browser (WebRTC lewat WHIP). Seperti
// stream key, token hanya ada di jawaban ini, jadi Cache-Control: no-store.
func (h *liveHandler) createPublishToken(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.CreatePublishToken(ctx, &livev1.CreatePublishTokenRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, publishTokenResponse{WHIPURL: resp.GetWhipUrl(), Token: resp.GetToken(), ExpiresAt: resp.GetExpiresAt().AsTime()})
}

// listChat: GET /api/v1/live/:id/chat?limit=50  (tanpa login, langkah 25c)
// Pesan chat terakhir, urut dari yang paling lama.
func (h *liveHandler) listChat(c *gin.Context) {
	id, ok := liveIDParam(c)
	if !ok {
		return
	}
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

	resp, err := h.client.ListChatMessages(ctx, &livev1.ListChatMessagesRequest{StreamId: id, Limit: limit})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	msgs := resp.GetMessages()
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.GetUserId()
	}
	users := lookupProfiles(c.Request.Context(), h.users, h.logger, ids)
	out := make([]chatMessageResponse, len(msgs))
	for i, m := range msgs {
		out[i] = toChatMessageResponse(m, users[m.GetUserId()])
	}
	c.JSON(http.StatusOK, gin.H{"messages": out})
}

// sendChat: POST /api/v1/live/:id/chat  {"body":"halo","color":"#4FE3C1"}
// Wajib login. Mode lambat: satu pesan per 3 detik (429 jika lebih cepat).
func (h *liveHandler) sendChat(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, ok := liveIDParam(c)
	if !ok {
		return
	}
	var req sendChatRequest
	if !bindJSON(c, &req) {
		return
	}
	in := &livev1.SendChatMessageRequest{RequesterId: userID, StreamId: id, Body: req.Body}
	if req.Color != "" {
		color, err := parseColor(req.Color)
		if err != nil {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "color harus berformat #RRGGBB, misalnya #FF0000")
			return
		}
		in.Color = &color
	}
	// "Chat hanya untuk pengikut" (langkah 27): data langganan ada di
	// UserService, jadi gateway yang mengisinya. Jika UserService gagal,
	// requester dianggap belum mengikuti; siaran tanpa pilihan itu tidak
	// terpengaruh, karena LiveService hanya memeriksanya jika aktif.
	in.RequesterFollows = h.follows(c.Request.Context(), userID, id)
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.SendChatMessage(ctx, in)
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	users := lookupProfiles(c.Request.Context(), h.users, h.logger, []int64{userID})
	c.JSON(http.StatusCreated, gin.H{"message": toChatMessageResponse(resp.GetMessage(), users[userID])})
}

// chatLive: GET /api/v1/live/:id/chat/live  (tanpa login)
//
// Koneksi Server-Sent Events, sama seperti danmaku video: setiap pesan chat
// baru dikirim sebagai
//
//	event: chat
//	data: {"id":"15","stream_id":"3","user":{...},"body":"halo",...}
//
// follows: userID mengikuti pemilik siaran streamID. Dua panggilan kecil
// (pemilik siaran, lalu daftar kanal yang diikuti); false jika salah satu
// gagal.
func (h *liveHandler) follows(ctx context.Context, userID, streamID int64) bool {
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	st, err := h.client.GetLiveStream(ctx, &livev1.GetLiveStreamRequest{StreamId: streamID})
	if err != nil || !st.GetStream().GetFollowersOnly() {
		// Pilihan tidak aktif (atau siaran tidak ada; SendChatMessage yang
		// menjawab error-nya): tidak perlu bertanya ke UserService.
		return false
	}
	subs, err := h.users.ListSubscribedChannelIds(ctx, &userv1.ListSubscribedChannelIdsRequest{RequesterId: userID})
	if err != nil {
		h.logger.WarnContext(ctx, "gagal memeriksa langganan untuk chat khusus pengikut", slog.Any("error", err))
		return false
	}
	return slices.Contains(subs.GetChannelIds(), st.GetStream().GetOwnerId())
}

func (h *liveHandler) chatLive(c *gin.Context) {
	if h.chatHub == nil {
		abortError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "chat live sedang tidak tersedia")
		return
	}
	id, ok := liveIDParam(c)
	if !ok {
		return
	}
	// Pastikan siarannya ada sebelum membuka koneksi yang berumur panjang.
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	_, err := h.client.GetLiveStream(ctx, &livev1.GetLiveStreamRequest{StreamId: id})
	cancel()
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	serveSSE(c, h.chatHub, id, "chat", h.heartbeat, h.logger)
}

func liveIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "siaran tidak ditemukan")
		return 0, false
	}
	return id, true
}

func toChatMessageResponse(m *livev1.ChatMessage, user *ownerResponse) chatMessageResponse {
	return chatMessageResponse{
		ID:        strconv.FormatInt(m.GetId(), 10),
		StreamID:  strconv.FormatInt(m.GetStreamId(), 10),
		User:      user,
		Body:      m.GetBody(),
		Color:     formatColor(m.GetColor()),
		CreatedAt: m.GetCreatedAt().AsTime(),
	}
}

// Cache profil pengirim chat di NewLiveChatEncoder.
const (
	chatProfileTTL     = time.Minute
	chatProfileMaxSize = 10_000
)

// NewLiveChatEncoder membuat live.Options.Transform untuk chat: mengubah
// event NATS live.chat.* menjadi JSON yang sama dengan respons REST,
// lengkap dengan nama dan foto pengirim.
//
// Transform dijalankan sekali per pesan per pod (bukan per penonton), tapi
// di chat yang ramai orang yang sama mengirim berkali-kali. Profil
// disimpan sebentar (1 menit) supaya UserService tidak dipanggil untuk
// setiap pesan. Padanannya: cache in-memory seperti node-cache (Node.js)
// atau Caffeine (Java).
func NewLiveChatEncoder(users userv1.UserServiceClient, logger *slog.Logger) func([]byte) ([]byte, error) {
	type entry struct {
		user    *ownerResponse
		expires time.Time
	}
	var (
		mu    sync.Mutex
		cache = make(map[int64]entry)
	)
	profile := func(id int64) *ownerResponse {
		now := time.Now()
		mu.Lock()
		e, ok := cache[id]
		mu.Unlock()
		if ok && now.Before(e.expires) {
			return e.user
		}
		u := lookupProfiles(context.Background(), users, logger, []int64{id})[id]
		if u == nil {
			// Gagal: jangan disimpan, coba lagi di pesan berikutnya.
			return nil
		}
		mu.Lock()
		if len(cache) >= chatProfileMaxSize {
			// Cara paling sederhana membatasi memori: kosongkan semua.
			clear(cache)
		}
		cache[id] = entry{user: u, expires: now.Add(chatProfileTTL)}
		mu.Unlock()
		return u
	}
	return func(data []byte) ([]byte, error) {
		var ev events.LiveChatSent
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, fmt.Errorf("decode event chat: %w", err)
		}
		return json.Marshal(chatMessageResponse{
			ID:        strconv.FormatInt(ev.ID, 10),
			StreamID:  strconv.FormatInt(ev.StreamID, 10),
			User:      profile(ev.UserID),
			Body:      ev.Body,
			Color:     formatColor(ev.Color),
			CreatedAt: ev.CreatedAt,
		})
	}
}
