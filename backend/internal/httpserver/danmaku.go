package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/events"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/live"
)

const (
	// Komentar SSE kosong yang dikirim berkala, supaya proxy (NGINX) tidak
	// menutup koneksi yang terlihat diam. Batas bawaan NGINX 60 detik.
	liveHeartbeat = 20 * time.Second
	// Batas waktu satu kali menulis ke browser. Kalau browser tidak
	// membaca selama ini, koneksinya dianggap mati.
	liveWriteTimeout = 10 * time.Second
	// Browser menunggu sekian milidetik sebelum menyambung ulang otomatis.
	liveRetryMs = 3000
)

// danmakuHandler meneruskan request danmaku ke DanmakuService (gRPC), dan
// menyiarkan danmaku baru ke browser lewat Server-Sent Events (SSE).
type danmakuHandler struct {
	client danmakuv1.DanmakuServiceClient
	// videos dipakai endpoint live untuk memastikan video ada dan READY.
	videos videov1.VideoServiceClient
	// hub boleh nil (di test); endpoint live lalu menjawab 503.
	hub       *live.Hub
	heartbeat time.Duration
	logger    *slog.Logger
}

type sendDanmakuRequest struct {
	Body string `json:"body"`
	// PositionMs wajib: detik video saat tombol kirim ditekan, dalam
	// milidetik, misalnya Math.floor(video.currentTime * 1000).
	PositionMs *int64 `json:"position_ms"`
	// Color opsional, format "#RRGGBB". Default "#FFFFFF".
	Color string `json:"color"`
	// Mode opsional: "SCROLL" (default), "TOP", atau "BOTTOM".
	Mode string `json:"mode"`
}

// danmakuResponse sengaja tanpa id pengirim: danmaku di Bilibili juga
// anonim bagi penonton lain.
type danmakuResponse struct {
	ID         string    `json:"id"`
	VideoID    string    `json:"video_id"`
	PositionMs int64     `json:"position_ms"`
	Body       string    `json:"body"`
	Color      string    `json:"color"`
	Mode       string    `json:"mode"`
	CreatedAt  time.Time `json:"created_at"`
}

type listDanmakuResponse struct {
	Danmaku []danmakuResponse `json:"danmaku"`
	// Kosong jika rentang ini sudah habis.
	NextPageToken string `json:"next_page_token"`
}

var danmakuModes = map[string]danmakuv1.DanmakuMode{
	"SCROLL": danmakuv1.DanmakuMode_DANMAKU_MODE_SCROLL,
	"TOP":    danmakuv1.DanmakuMode_DANMAKU_MODE_TOP,
	"BOTTOM": danmakuv1.DanmakuMode_DANMAKU_MODE_BOTTOM,
}

// send: POST /api/v1/videos/:id/danmaku
// {"body": "wkwk", "position_ms": 15300, "color": "#FF0000", "mode": "TOP"}
// Wajib login. Pengirim diambil dari access token.
func (h *danmakuHandler) send(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	var req sendDanmakuRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.PositionMs == nil {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "position_ms wajib diisi")
		return
	}
	in := &danmakuv1.SendDanmakuRequest{
		RequesterId: userID,
		VideoId:     videoID,
		PositionMs:  *req.PositionMs,
		Body:        req.Body,
	}
	if req.Color != "" {
		color, err := parseColor(req.Color)
		if err != nil {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "color harus berformat #RRGGBB, misalnya #FF0000")
			return
		}
		in.Color = &color
	}
	if req.Mode != "" {
		mode, ok := danmakuModes[strings.ToUpper(req.Mode)]
		if !ok {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "mode harus SCROLL, TOP, atau BOTTOM")
			return
		}
		in.Mode = mode
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.SendDanmaku(ctx, in)
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"danmaku": toDanmakuResponse(resp.GetDanmaku())})
}

// list: GET /api/v1/videos/:id/danmaku?from_ms=0&to_ms=360000&page_size=&page_token=
// Danmaku dalam satu rentang waktu, urut dari posisi paling awal. Tanpa
// login. from_ms default 0, to_ms default from_ms + 6 menit.
func (h *danmakuHandler) list(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	fromMs, ok := int64Query(c, "from_ms", 0)
	if !ok {
		return
	}
	toMs, ok := int64Query(c, "to_ms", fromMs+6*60*1000)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListDanmaku(ctx, &danmakuv1.ListDanmakuRequest{
		VideoId:   videoID,
		FromMs:    fromMs,
		ToMs:      toMs,
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	out := listDanmakuResponse{Danmaku: make([]danmakuResponse, 0, len(resp.GetDanmaku())), NextPageToken: resp.GetNextPageToken()}
	for _, d := range resp.GetDanmaku() {
		out.Danmaku = append(out.Danmaku, toDanmakuResponse(d))
	}
	c.JSON(http.StatusOK, out)
}

// live: GET /api/v1/videos/:id/danmaku/live
//
// Koneksi Server-Sent Events: response tidak pernah selesai, dan setiap
// danmaku baru dikirim sebagai satu event:
//
//	event: danmaku
//	data: {"id":"15","video_id":"42","position_ms":15300,...}
//
// Di browser cukup:
//
//	const es = new EventSource(".../videos/42/danmaku/live");
//	es.addEventListener("danmaku", (e) => tampilkan(JSON.parse(e.data)));
//
// EventSource otomatis menyambung ulang jika koneksi putus. Danmaku yang
// terkirim selama putus tidak dikirim ulang; penonton tetap melihatnya di
// ListDanmaku saat memutar ulang bagian itu.
//
// Mengapa SSE, bukan WebSocket: arah datanya hanya server ke browser
// (mengirim danmaku memakai POST biasa), SSE berjalan di atas HTTP biasa
// sehingga melewati proxy dan middleware yang sama, dan tidak butuh library
// tambahan. WebSocket baru perlu kalau browser harus mengirim pesan terus-
// menerus lewat koneksi yang sama, misalnya game atau chat live streaming.
func (h *danmakuHandler) live(c *gin.Context) {
	if h.hub == nil {
		abortError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "danmaku live sedang tidak tersedia")
		return
	}
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	if !h.checkReady(c, videoID) {
		return
	}

	serveSSE(c, h.hub, videoID, "danmaku", h.heartbeat, h.logger)
}

// checkReady memastikan video ada dan READY sebelum membuka koneksi live.
func (h *danmakuHandler) checkReady(c *gin.Context, videoID int64) bool {
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.videos.GetVideo(ctx, &videov1.GetVideoRequest{VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return false
	}
	if resp.GetVideo().GetStatus() != videov1.VideoStatus_VIDEO_STATUS_READY {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "video tidak ditemukan")
		return false
	}
	return true
}

// EncodeLiveDanmaku mengubah event NATS danmaku.live.* menjadi JSON yang
// sama dengan respons REST. Dipakai sebagai live.Options.Transform, jadi
// dijalankan sekali per danmaku, bukan sekali per penonton.
func EncodeLiveDanmaku(data []byte) ([]byte, error) {
	var ev events.DanmakuSent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("decode event danmaku: %w", err)
	}
	return json.Marshal(danmakuResponse{
		ID:         strconv.FormatInt(ev.ID, 10),
		VideoID:    strconv.FormatInt(ev.VideoID, 10),
		PositionMs: ev.PositionMs,
		Body:       ev.Body,
		Color:      formatColor(ev.Color),
		Mode:       ev.Mode,
		CreatedAt:  ev.CreatedAt,
	})
}

func toDanmakuResponse(d *danmakuv1.Danmaku) danmakuResponse {
	return danmakuResponse{
		ID:         strconv.FormatInt(d.GetId(), 10),
		VideoID:    strconv.FormatInt(d.GetVideoId(), 10),
		PositionMs: d.GetPositionMs(),
		Body:       d.GetBody(),
		Color:      formatColor(d.GetColor()),
		Mode:       strings.TrimPrefix(d.GetMode().String(), "DANMAKU_MODE_"),
		CreatedAt:  d.GetCreatedAt().AsTime(),
	}
}

// parseColor mengubah "#FF8800" menjadi 0xFF8800.
func parseColor(s string) (uint32, error) {
	if len(s) != 7 || s[0] != '#' {
		return 0, errors.New("format warna salah")
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}

// formatColor mengubah 0xFF8800 menjadi "#FF8800".
func formatColor(c uint32) string {
	return fmt.Sprintf("#%06X", c&0xFFFFFF)
}

// int64Query membaca angka dari query string, atau fallback jika kosong.
func int64Query(c *gin.Context, key string, fallback int64) (int64, bool) {
	v := c.Query(key)
	if v == "" {
		return fallback, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", key+" harus berupa angka")
		return 0, false
	}
	return n, true
}
