package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

const (
	// Batas waktu satu panggilan ke VideoService.
	videoCallTimeout = 5 * time.Second
	// Body JSON video hanya berisi judul, deskripsi, tipe, dan ukuran file.
	// File videonya sendiri TIDAK lewat gateway.
	maxVideoBodyBytes = 16 << 10 // 16 KiB
)

// videoHandler meneruskan request REST dari browser ke VideoService (gRPC).
type videoHandler struct {
	client videov1.VideoServiceClient
	logger *slog.Logger
}

type createVideoRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Tipe file dari browser, misalnya file.type = "video/mp4".
	ContentType string `json:"content_type"`
	// Ukuran file dalam byte, misalnya file.size.
	SizeBytes int64 `json:"size_bytes"`
}

type videoResponse struct {
	// ID dikirim sebagai string, sama seperti id user (batas angka JavaScript).
	ID          string     `json:"id"`
	OwnerID     string     `json:"owner_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	CreatedAt   time.Time  `json:"created_at"`
	UploadedAt  *time.Time `json:"uploaded_at,omitempty"`
	// Field di bawah hanya ada setelah transcoding (READY atau FAILED).
	DurationMs int64    `json:"duration_ms,omitempty"`
	Renditions []string `json:"renditions,omitempty"`
	// HLSURL dibuka player HLS, misalnya hls.js: hls.loadSource(video.hls_url).
	// Tidak kedaluwarsa. Hanya ada jika status READY.
	HLSURL       string `json:"hls_url,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	// FailureReason hanya ada jika status FAILED, kalimat untuk ditampilkan
	// ke pemilik video.
	FailureReason string     `json:"failure_reason,omitempty"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
	// ViewCount selalu ada (0 untuk video baru).
	ViewCount int64 `json:"view_count"`
}

// listVideosResponse dipakai GET /videos (feed publik) dan GET /me/videos.
type listVideosResponse struct {
	Videos []videoResponse `json:"videos"`
	// Kosong jika sudah halaman terakhir. Kirim kembali sebagai
	// ?page_token=... untuk mengambil halaman berikutnya.
	NextPageToken string `json:"next_page_token"`
}

// recordViewResponse adalah jawaban POST /videos/:id/views.
type recordViewResponse struct {
	// Counted bernilai false jika penonton ini sudah dihitung dalam 30
	// menit terakhir (refresh tidak menambah views).
	Counted   bool  `json:"counted"`
	ViewCount int64 `json:"view_count"`
}

// uploadResponse memberi tahu browser cara mengunggah file:
// fetch(upload.url, {method: upload.method, headers: upload.headers, body: file}).
type uploadResponse struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// originalResponse berisi URL untuk mengunduh file asli (sebelum
// di-transcode). URL ini kedaluwarsa, jadi frontend mengambil ulang
// GET /videos/:id jika sudah lewat expires_at.
type originalResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type getVideoResponse struct {
	Video videoResponse `json:"video"`
	// Hanya untuk pemilik video, setelah file selesai diunggah. Untuk
	// memutar video, pakai video.hls_url.
	Original *originalResponse `json:"original,omitempty"`
}

func (h *videoHandler) create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req createVideoRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.CreateUpload(ctx, &videov1.CreateUploadRequest{
		RequesterId: userID,
		Title:       req.Title,
		Description: req.Description,
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"video": toVideoResponse(resp.GetVideo()),
		"upload": uploadResponse{
			URL:       resp.GetUploadUrl(),
			Method:    http.MethodPut,
			Headers:   resp.GetUploadHeaders(),
			ExpiresAt: resp.GetUploadUrlExpiresAt().AsTime(),
		},
	})
}

func (h *videoHandler) complete(c *gin.Context) {
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

	resp, err := h.client.CompleteUpload(ctx, &videov1.CompleteUploadRequest{RequesterId: userID, VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"video": toVideoResponse(resp.GetVideo())})
}

func (h *videoHandler) get(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	// 0 = pengunjung belum login (OptionalAuth tidak menyimpan klaim).
	var viewerID int64
	if claims, ok := CurrentUser(c); ok {
		viewerID, _ = claims.UserID()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetVideo(ctx, &videov1.GetVideoRequest{VideoId: videoID, ViewerId: viewerID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := getVideoResponse{Video: toVideoResponse(resp.GetVideo())}
	if resp.GetOriginalUrl() != "" {
		body.Original = &originalResponse{
			URL:       resp.GetOriginalUrl(),
			ExpiresAt: resp.GetOriginalUrlExpiresAt().AsTime(),
		}
	}
	c.JSON(http.StatusOK, body)
}

func (h *videoHandler) listMine(c *gin.Context) {
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

	resp, err := h.client.ListMyVideos(ctx, &videov1.ListMyVideosRequest{
		RequesterId: userID,
		PageSize:    pageSize,
		PageToken:   c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toListResponse(resp.GetVideos(), resp.GetNextPageToken()))
}

// list adalah feed publik: GET /api/v1/videos?page_size=20&page_token=...
// Tidak butuh login.
func (h *videoHandler) list(c *gin.Context) {
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListVideos(ctx, &videov1.ListVideosRequest{
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toListResponse(resp.GetVideos(), resp.GetNextPageToken()))
}

// recordView dipanggil frontend saat video mulai diputar:
// POST /api/v1/videos/:id/views (tanpa body). Login tidak wajib.
func (h *videoHandler) recordView(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.RecordView(ctx, &videov1.RecordViewRequest{VideoId: videoID, ViewerKey: viewerKey(c)})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, recordViewResponse{Counted: resp.GetCounted(), ViewCount: resp.GetViewCount()})
}

// viewerKey membuat penanda penonton untuk anti-dobel views:
//   - user login: "u-<id user>", jadi dihitung sekali walau pindah perangkat;
//   - tanpa login: "a-" + 32 huruf pertama SHA-256 dari IP dan User-Agent.
//     IP tidak disimpan apa adanya, dan orang di jaringan yang sama (satu
//     IP kantor) tetap dibedakan oleh browser-nya.
//
// Penanda ini hanya disimpan sementara di NATS (sesuai VIEW_DEDUP_WINDOW).
// Ini bukan perlindungan sempurna terhadap bot; tujuannya hanya supaya
// refresh berulang tidak menambah views.
func viewerKey(c *gin.Context) string {
	if claims, ok := CurrentUser(c); ok {
		if id, err := claims.UserID(); err == nil {
			return "u-" + strconv.FormatInt(id, 10)
		}
	}
	sum := sha256.Sum256([]byte(c.ClientIP() + "\n" + c.Request.UserAgent()))
	return "a-" + hex.EncodeToString(sum[:16])
}

// pageSizeQuery membaca ?page_size=. Kosong = 0 (VideoService memakai
// default). Jika bukan angka, langsung menjawab 400 dan mengembalikan false.
func pageSizeQuery(c *gin.Context) (int32, bool) {
	v := c.Query("page_size")
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "page_size harus berupa angka")
		return 0, false
	}
	return int32(n), true
}

func toListResponse(videos []*videov1.Video, next string) listVideosResponse {
	out := listVideosResponse{Videos: make([]videoResponse, 0, len(videos)), NextPageToken: next}
	for _, v := range videos {
		out.Videos = append(out.Videos, toVideoResponse(v))
	}
	return out
}

// currentUserID mengambil id user dari access token yang sudah diperiksa
// RequireAuth. Jika tidak ada, langsung menjawab 401 dan mengembalikan false.
func currentUserID(c *gin.Context) (int64, bool) {
	claims, ok := CurrentUser(c)
	if ok {
		if id, err := claims.UserID(); err == nil {
			return id, true
		}
	}
	abortError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token tidak ada")
	return 0, false
}

// videoIDParam membaca :id dari URL. Id yang bukan angka dijawab 404, sama
// seperti id yang tidak ada.
func videoIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "video tidak ditemukan")
		return 0, false
	}
	return id, true
}

// videoStatusNames mengubah enum gRPC menjadi string untuk JSON, misalnya
// VIDEO_STATUS_UPLOADED menjadi "UPLOADED".
var videoStatusNames = map[videov1.VideoStatus]string{
	videov1.VideoStatus_VIDEO_STATUS_PENDING_UPLOAD: "PENDING_UPLOAD",
	videov1.VideoStatus_VIDEO_STATUS_UPLOADED:       "UPLOADED",
	videov1.VideoStatus_VIDEO_STATUS_PROCESSING:     "PROCESSING",
	videov1.VideoStatus_VIDEO_STATUS_READY:          "READY",
	videov1.VideoStatus_VIDEO_STATUS_FAILED:         "FAILED",
}

func toVideoResponse(v *videov1.Video) videoResponse {
	status, ok := videoStatusNames[v.GetStatus()]
	if !ok {
		status = "UNKNOWN"
	}
	return videoResponse{
		ID:          strconv.FormatInt(v.GetId(), 10),
		OwnerID:     strconv.FormatInt(v.GetOwnerId(), 10),
		Title:       v.GetTitle(),
		Description: v.GetDescription(),
		Status:      status,
		ContentType: v.GetContentType(),
		SizeBytes:   v.GetSizeBytes(),
		CreatedAt:   v.GetCreatedAt().AsTime(),
		UploadedAt:  optionalTime(v.GetUploadedAt()),

		DurationMs:    v.GetDurationMs(),
		Renditions:    v.GetRenditions(),
		HLSURL:        v.GetHlsUrl(),
		ThumbnailURL:  v.GetThumbnailUrl(),
		FailureReason: v.GetFailureReason(),
		ProcessedAt:   optionalTime(v.GetProcessedAt()),
		ViewCount:     v.GetViewCount(),
	}
}

func optionalTime(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}
