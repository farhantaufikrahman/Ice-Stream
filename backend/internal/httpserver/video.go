package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

const (
	// Batas waktu satu panggilan ke VideoService.
	videoCallTimeout = 5 * time.Second
	// DELETE /videos/:id menunggu VideoService menghapus file di storage
	// (paling lama 30 detik di sisi VideoService), jadi batasnya lebih lama.
	videoDeleteTimeout = 35 * time.Second
	// Body JSON video hanya berisi judul, deskripsi, tipe, dan ukuran file.
	// File videonya sendiri TIDAK lewat gateway.
	maxVideoBodyBytes = 16 << 10 // 16 KiB
)

// videoHandler meneruskan request REST dari browser ke VideoService (gRPC).
type videoHandler struct {
	client videov1.VideoServiceClient
	// users dipakai untuk mengambil info pemilik video. Boleh nil (di test).
	users  userv1.UserServiceClient
	logger *slog.Logger
}

type createVideoRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Tipe file dari browser, misalnya file.type = "video/mp4".
	ContentType string `json:"content_type"`
	// Ukuran file dalam byte, misalnya file.size.
	SizeBytes int64 `json:"size_bytes"`
	// Boleh tidak dikirim. Lihat CreateUploadRequest di video.proto.
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	// Langkah 26 (desain Upload). visibility: "PUBLIC" (bawaan),
	// "UNLISTED", atau "PRIVATE". draft: true = "Simpan draf" (belum
	// terbit walau sudah selesai diproses). language: "id", "en", "ja",
	// atau kosong.
	Visibility string `json:"visibility"`
	Draft      bool   `json:"draft"`
	Language   string `json:"language"`
}

// thumbnailFrameResponse adalah satu pilihan "Bingkai" di desain Upload.
type thumbnailFrameResponse struct {
	PositionMs int64  `json:"position_ms"`
	URL        string `json:"url"`
	Selected   bool   `json:"selected"`
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
	// CommentCount adalah jumlah komentar dan balasan (selalu ada).
	CommentCount int64 `json:"comment_count"`
	// LikeCount adalah jumlah like (selalu ada).
	LikeCount int64 `json:"like_count"`
	// DanmakuCount adalah jumlah danmaku (selalu ada, langkah 24a).
	DanmakuCount int64 `json:"danmaku_count"`
	// Category selalu ada; "" jika video tidak punya kategori.
	Category string `json:"category"`
	// Tags selalu berupa array (bisa kosong), tanpa "#".
	Tags []string `json:"tags"`
	// Owner berisi nama dan foto pemilik video (feed, detail, channel).
	// Tidak ada di /me/videos, dan kosong jika UserService sedang mati.
	Owner *ownerResponse `json:"owner,omitempty"`
	// Langkah 26. Visibility selalu ada: "PUBLIC", "UNLISTED", "PRIVATE",
	// atau "BLOCKED" (diturunkan admin, langkah 28; hanya terlihat oleh
	// pemiliknya dan admin). PublishedAt = waktu video pertama kali terbit (bukan
	// draf dan selesai diproses); tanggal di kartu video memakai ini.
	Visibility  string     `json:"visibility"`
	Draft       bool       `json:"draft"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Language    string     `json:"language"`
	// CustomThumbnail: true jika thumbnail diunggah kreator.
	CustomThumbnail bool `json:"custom_thumbnail"`
	// ThumbnailFrames hanya untuk pemilik video (lihat
	// toOwnerVideoResponse): pilihan bingkai buatan transcoder.
	ThumbnailFrames []thumbnailFrameResponse `json:"thumbnail_frames,omitempty"`
	// Moderation hanya ada jika visibility = "BLOCKED" (langkah 28):
	// alasan dan catatan admin untuk pemilik video.
	Moderation *videoModerationResponse `json:"moderation,omitempty"`
}

// videoModerationResponse: keterangan video yang diturunkan admin.
type videoModerationResponse struct {
	// Reason memakai nilai yang sama dengan alasan laporan ("spam", ...).
	Reason    string    `json:"reason"`
	Note      string    `json:"note"`
	BlockedAt time.Time `json:"blocked_at"`
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
	// ViewerLiked hanya ada jika request membawa access token: true jika
	// user ini sudah like. Tanpa login field ini tidak dikirim, supaya
	// frontend bisa membedakan "belum like" dan "belum login".
	ViewerLiked *bool `json:"viewer_liked,omitempty"`
	// ResumePositionMs hanya ada jika request membawa access token: detik
	// (milidetik) untuk melanjutkan video, 0 = putar dari awal. Di browser:
	// video.currentTime = resume_position_ms / 1000.
	ResumePositionMs *int64 `json:"resume_position_ms,omitempty"`
	// ViewerSaved hanya ada jika request membawa access token: true jika
	// video ini ada di Tonton nanti user tersebut.
	ViewerSaved *bool `json:"viewer_saved,omitempty"`
	// ViewerInPlaylist hanya ada jika request membawa access token: true
	// jika video ini ada di salah satu playlist milik user tersebut.
	ViewerInPlaylist *bool `json:"viewer_in_playlist,omitempty"`
}

// likeResponse adalah jawaban PUT dan DELETE /videos/:id/like.
type likeResponse struct {
	Liked     bool  `json:"liked"`
	LikeCount int64 `json:"like_count"`
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

	visibility, ok := parseVideoVisibility(c, req.Visibility)
	if !ok {
		return
	}
	resp, err := h.client.CreateUpload(ctx, &videov1.CreateUploadRequest{
		RequesterId: userID,
		Title:       req.Title,
		Description: req.Description,
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
		Category:    req.Category,
		Tags:        req.Tags,
		Visibility:  visibility,
		Draft:       req.Draft,
		Language:    req.Language,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"video": toOwnerVideoResponse(resp.GetVideo()),
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
	c.JSON(http.StatusOK, gin.H{"video": toOwnerVideoResponse(resp.GetVideo())})
}

func (h *videoHandler) get(c *gin.Context) {
	videoID, ok := videoIDParam(c)
	if !ok {
		return
	}
	viewerID := optionalUserID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetVideo(ctx, &videov1.GetVideoRequest{VideoId: videoID, ViewerId: viewerID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := getVideoResponse{Video: toVideoResponse(resp.GetVideo())}
	if viewerID != 0 && viewerID == resp.GetVideo().GetOwnerId() {
		body.Video = toOwnerVideoResponse(resp.GetVideo())
	}
	if viewerID != 0 {
		liked := resp.GetViewerLiked()
		body.ViewerLiked = &liked
		resume := resp.GetResumePositionMs()
		body.ResumePositionMs = &resume
		saved := resp.GetViewerSaved()
		body.ViewerSaved = &saved
		inPlaylist := resp.GetViewerInPlaylist()
		body.ViewerInPlaylist = &inPlaylist
	}
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, []*videov1.Video{resp.GetVideo()})
	body.Video.Owner = owners[resp.GetVideo().GetOwnerId()]
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
	out := listVideosResponse{Videos: make([]videoResponse, 0, len(resp.GetVideos())), NextPageToken: resp.GetNextPageToken()}
	for _, v := range resp.GetVideos() {
		out.Videos = append(out.Videos, toOwnerVideoResponse(v))
	}
	c.JSON(http.StatusOK, out)
}

// search: GET /api/v1/search/videos?q=belajar+golang&page_size=20&page_token=...
// Tanpa login. Hasilnya berbentuk sama dengan feed. Langkah 24a:
// ?sort=relevance|newest|views|danmaku (bawaan relevance) dan
// ?duration=short|medium|long (kurang dari 10 menit, 10-30 menit, lebih
// dari 30 menit). Halaman berikutnya harus mengirim sort dan duration
// yang sama.
// Path-nya /search/videos (bukan /videos/search) supaya tidak tertukar
// dengan /videos/:id, dan nanti bisa ditambah /search/channels.
func (h *videoHandler) search(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "parameter q wajib diisi")
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	sort, ok := enumQuery(c, "sort", searchSortParams, "relevance, newest, views, danmaku")
	if !ok {
		return
	}
	duration, ok := enumQuery(c, "duration", durationParams, "short, medium, long")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.SearchVideos(ctx, &videov1.SearchVideosRequest{
		Query:     q,
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
		Sort:      sort,
		Duration:  duration,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := toListResponse(resp.GetVideos(), resp.GetNextPageToken())
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, resp.GetVideos())
	for i, v := range resp.GetVideos() {
		body.Videos[i].Owner = owners[v.GetOwnerId()]
	}
	c.JSON(http.StatusOK, body)
}

// list adalah feed publik: GET /api/v1/videos?page_size=20&page_token=...
// Tidak butuh login. ?category=game hanya mengambil video kategori itu
// (chip di beranda); kategori yang tidak dikenal dijawab 400.
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
		Category:  c.Query("category"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := toListResponse(resp.GetVideos(), resp.GetNextPageToken())
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, resp.GetVideos())
	for i, v := range resp.GetVideos() {
		body.Videos[i].Owner = owners[v.GetOwnerId()]
	}
	c.JSON(http.StatusOK, body)
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

// like: PUT /api/v1/videos/:id/like (tanpa body). Wajib login.
// PUT dipakai (bukan POST) karena hasilnya sama walau dikirim berkali-kali:
// "jadikan video ini disukai saya". DELETE membatalkannya.
func (h *videoHandler) like(c *gin.Context) {
	h.changeLike(c, true)
}

// unlike: DELETE /api/v1/videos/:id/like. Wajib login.
func (h *videoHandler) unlike(c *gin.Context) {
	h.changeLike(c, false)
}

func (h *videoHandler) changeLike(c *gin.Context, like bool) {
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

	var out likeResponse
	if like {
		resp, err := h.client.LikeVideo(ctx, &videov1.LikeVideoRequest{RequesterId: userID, VideoId: videoID})
		if err != nil {
			writeGRPCError(c, h.logger, err)
			return
		}
		out = likeResponse{Liked: resp.GetLiked(), LikeCount: resp.GetLikeCount()}
	} else {
		resp, err := h.client.UnlikeVideo(ctx, &videov1.UnlikeVideoRequest{RequesterId: userID, VideoId: videoID})
		if err != nil {
			writeGRPCError(c, h.logger, err)
			return
		}
		out = likeResponse{Liked: resp.GetLiked(), LikeCount: resp.GetLikeCount()}
	}
	c.JSON(http.StatusOK, out)
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

// optionalUserID mengambil id user dari access token yang diperiksa
// OptionalAuth. 0 = pengunjung belum login.
func optionalUserID(c *gin.Context) int64 {
	if claims, ok := CurrentUser(c); ok {
		if id, err := claims.UserID(); err == nil {
			return id
		}
	}
	return 0
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
		CommentCount:  v.GetCommentCount(),
		LikeCount:     v.GetLikeCount(),
		DanmakuCount:  v.GetDanmakuCount(),
		Category:      v.GetCategory(),
		// Selalu array, bukan null, supaya frontend bisa langsung
		// video.tags.map(...).
		Tags: append([]string{}, v.GetTags()...),

		Visibility:      videoVisibilityNames[v.GetVisibility()],
		Draft:           v.GetDraft(),
		PublishedAt:     optionalTime(v.GetPublishedAt()),
		Language:        v.GetLanguage(),
		CustomThumbnail: v.GetCustomThumbnail(),
		Moderation:      toVideoModerationResponse(v.GetModeration()),
	}
}

func toVideoModerationResponse(m *videov1.VideoModeration) *videoModerationResponse {
	if m == nil {
		return nil
	}
	return &videoModerationResponse{
		Reason:    reportReasonNames[m.GetReason()],
		Note:      m.GetNote(),
		BlockedAt: m.GetBlockedAt().AsTime(),
	}
}

// toOwnerVideoResponse sama dengan toVideoResponse ditambah pilihan
// bingkai thumbnail. Hanya untuk jawaban yang dilihat pemilik video.
func toOwnerVideoResponse(v *videov1.Video) videoResponse {
	out := toVideoResponse(v)
	for _, f := range v.GetThumbnailFrames() {
		out.ThumbnailFrames = append(out.ThumbnailFrames, thumbnailFrameResponse{
			PositionMs: f.GetPositionMs(),
			URL:        f.GetUrl(),
			Selected:   f.GetSelected(),
		})
	}
	return out
}

// videoVisibilityNames: enum gRPC -> teks di JSON. UNSPECIFIED (video lama
// dari VideoService versi sebelumnya) berarti PUBLIC.
var videoVisibilityNames = map[videov1.VideoVisibility]string{
	videov1.VideoVisibility_VIDEO_VISIBILITY_UNSPECIFIED: "PUBLIC",
	videov1.VideoVisibility_VIDEO_VISIBILITY_PUBLIC:      "PUBLIC",
	videov1.VideoVisibility_VIDEO_VISIBILITY_UNLISTED:    "UNLISTED",
	videov1.VideoVisibility_VIDEO_VISIBILITY_PRIVATE:     "PRIVATE",
	videov1.VideoVisibility_VIDEO_VISIBILITY_BLOCKED:     "BLOCKED",
}

// parseVideoVisibility: "" = tidak dipilih (VideoService memakai PUBLIC).
// Huruf besar/kecil tidak dibedakan. Selain itu dijawab 400.
func parseVideoVisibility(c *gin.Context, raw string) (videov1.VideoVisibility, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "":
		return videov1.VideoVisibility_VIDEO_VISIBILITY_UNSPECIFIED, true
	case "PUBLIC":
		return videov1.VideoVisibility_VIDEO_VISIBILITY_PUBLIC, true
	case "UNLISTED":
		return videov1.VideoVisibility_VIDEO_VISIBILITY_UNLISTED, true
	case "PRIVATE":
		return videov1.VideoVisibility_VIDEO_VISIBILITY_PRIVATE, true
	}
	abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "visibility harus PUBLIC, UNLISTED, atau PRIVATE")
	return 0, false
}

func optionalTime(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}
