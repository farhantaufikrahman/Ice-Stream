package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Tab Kreator dan Playlist di halaman pencarian (langkah 22c). Tab Video
// memakai /search/videos (video.go).

// channelResultResponse adalah satu kreator di hasil pencarian.
type channelResultResponse struct {
	profileResponse
	// Hanya ada jika request membawa access token: true jika penonton
	// sudah mengikuti kreator ini (tombol Ikuti / Mengikuti).
	ViewerSubscribed *bool `json:"viewer_subscribed,omitempty"`
}

type searchChannelsResponse struct {
	Channels      []channelResultResponse `json:"channels"`
	NextPageToken string                  `json:"next_page_token"`
}

// searchQuery membaca ?q= dan ?page_size=. Menjawab 400 dan mengembalikan
// false jika tidak valid.
func searchQuery(c *gin.Context) (string, int32, bool) {
	q := c.Query("q")
	if q == "" {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "parameter q wajib diisi")
		return "", 0, false
	}
	pageSize, ok := pageSizeQuery(c)
	return q, pageSize, ok
}

// searchChannels: GET /api/v1/search/channels?q=budi&page_size=20&page_token=...
// Boleh tanpa login; dengan login, setiap kreator membawa viewer_subscribed.
func (h *userHandler) searchChannels(c *gin.Context) {
	q, pageSize, ok := searchQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.users.SearchUsers(ctx, &userv1.SearchUsersRequest{Query: q, PageSize: pageSize, PageToken: c.Query("page_token")})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := searchChannelsResponse{
		Channels:      make([]channelResultResponse, len(resp.GetProfiles())),
		NextPageToken: resp.GetNextPageToken(),
	}
	following := h.followingSet(c.Request.Context(), optionalUserID(c))
	for i, p := range resp.GetProfiles() {
		body.Channels[i] = channelResultResponse{profileResponse: toProfileResponse(p)}
		if following != nil {
			subscribed := following[p.GetId()]
			body.Channels[i].ViewerSubscribed = &subscribed
		}
	}
	c.JSON(http.StatusOK, body)
}

// followingSet mengembalikan channel yang diikuti userID (maksimal 1000,
// lihat maxSubscriptions di UserService). nil jika belum login atau
// UserService gagal; hasil pencarian tetap dikirim tanpa viewer_subscribed.
func (h *userHandler) followingSet(ctx context.Context, userID int64) map[int64]bool {
	if userID == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	resp, err := h.users.ListSubscribedChannelIds(ctx, &userv1.ListSubscribedChannelIdsRequest{RequesterId: userID})
	if err != nil {
		h.logger.WarnContext(ctx, "gagal mengambil daftar channel yang diikuti", slog.Any("error", err))
		return nil
	}
	set := make(map[int64]bool, len(resp.GetChannelIds()))
	for _, id := range resp.GetChannelIds() {
		set[id] = true
	}
	return set
}

// searchPlaylists: GET /api/v1/search/playlists?q=belajar&page_size=20&page_token=...
// Tanpa login. Hanya playlist PUBLIC yang berisi video, dengan info pemilik.
func (h *videoHandler) searchPlaylists(c *gin.Context) {
	q, pageSize, ok := searchQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.SearchPlaylists(ctx, &videov1.SearchPlaylistsRequest{Query: q, PageSize: pageSize, PageToken: c.Query("page_token")})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := listPlaylistsResponse{
		Playlists:     make([]playlistResponse, len(resp.GetPlaylists())),
		NextPageToken: resp.GetNextPageToken(),
	}
	ownerIDs := make([]int64, len(resp.GetPlaylists()))
	for i, p := range resp.GetPlaylists() {
		body.Playlists[i] = toPlaylistResponse(p)
		ownerIDs[i] = p.GetOwnerId()
	}
	owners := lookupProfiles(c.Request.Context(), h.users, h.logger, ownerIDs)
	for i, p := range resp.GetPlaylists() {
		body.Playlists[i].Owner = owners[p.GetOwnerId()]
	}
	c.JSON(http.StatusOK, body)
}

// Langkah 27: angka di setiap tab pencarian (desain Search: "Video 1.204",
// "Sekitar 1.284 hasil untuk ...").

// searchCountsHandler memanggil tiga service sekaligus.
type searchCountsHandler struct {
	videos videov1.VideoServiceClient
	users  userv1.UserServiceClient
	live   livev1.LiveServiceClient
	logger *slog.Logger
}

const (
	// searchCountCap: VideoService dan UserService berhenti menghitung di
	// 1001; yang lebih dari 1000 ditampilkan "1.000+".
	searchCountCap = 1000
	// liveCountCap: siaran dicari dengan ListLiveStreams (maksimal 50).
	liveCountCap = 50
)

// tabCount adalah jumlah hasil satu tab. Capped true berarti hasilnya
// lebih banyak dari Count (frontend menampilkan "1.000+").
type tabCount struct {
	Count  int64 `json:"count"`
	Capped bool  `json:"capped"`
}

// searchCountsResponse: tab yang gagal dihitung bernilai null (frontend
// tidak menampilkan angkanya), supaya satu service yang mati tidak
// menghilangkan angka tab lain.
type searchCountsResponse struct {
	Video    *tabCount `json:"video"`
	Channel  *tabCount `json:"channel"`
	Playlist *tabCount `json:"playlist"`
	Live     *tabCount `json:"live"`
}

func capCount(n, limit int64) *tabCount {
	if n > limit {
		return &tabCount{Count: limit, Capped: true}
	}
	return &tabCount{Count: n}
}

// get: GET /api/v1/search/counts?q=golang&duration=short
// Tanpa login. duration hanya berlaku untuk tab Video, sama dengan
// /search/videos. Empat panggilan berjalan bersamaan (goroutine), jadi
// lamanya kira-kira sama dengan panggilan yang paling lambat. Padanannya
// Promise.allSettled di Node.js atau CompletableFuture.allOf di Java.
func (h *searchCountsHandler) get(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "parameter q wajib diisi")
		return
	}
	duration, ok := enumQuery(c, "duration", durationParams, "short, medium, long")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	var (
		body     searchCountsResponse
		videoErr error
		wg       sync.WaitGroup
	)
	// page_size 1: yang dibutuhkan hanya total_size di halaman pertama.
	wg.Go(func() {
		resp, err := h.videos.SearchVideos(ctx, &videov1.SearchVideosRequest{Query: q, PageSize: 1, Duration: duration})
		if err != nil {
			videoErr = err
			return
		}
		body.Video = capCount(resp.GetTotalSize(), searchCountCap)
	})
	wg.Go(func() {
		resp, err := h.users.SearchUsers(ctx, &userv1.SearchUsersRequest{Query: q, PageSize: 1})
		if err != nil {
			h.logger.WarnContext(ctx, "gagal menghitung hasil kreator", slog.Any("error", err))
			return
		}
		body.Channel = capCount(resp.GetTotalSize(), searchCountCap)
	})
	wg.Go(func() {
		resp, err := h.videos.SearchPlaylists(ctx, &videov1.SearchPlaylistsRequest{Query: q, PageSize: 1})
		if err != nil {
			h.logger.WarnContext(ctx, "gagal menghitung hasil playlist", slog.Any("error", err))
			return
		}
		body.Playlist = capCount(resp.GetTotalSize(), searchCountCap)
	})
	wg.Go(func() {
		resp, err := h.live.ListLiveStreams(ctx, &livev1.ListLiveStreamsRequest{Query: q, Limit: liveCountCap})
		if err != nil {
			h.logger.WarnContext(ctx, "gagal menghitung hasil siaran", slog.Any("error", err))
			return
		}
		n := int64(len(resp.GetStreams()))
		// ListLiveStreams tidak memberi tahu apakah masih ada lagi; jika
		// penuh, anggap "50+".
		body.Live = &tabCount{Count: n, Capped: n == liveCountCap}
	})
	wg.Wait()

	// Kata kunci yang ditolak (misalnya hanya satu huruf) dijawab 400,
	// sama seperti /search/videos.
	if status.Code(videoErr) == codes.InvalidArgument {
		writeGRPCError(c, h.logger, videoErr)
		return
	}
	if videoErr != nil {
		h.logger.WarnContext(ctx, "gagal menghitung hasil video", slog.Any("error", videoErr))
	}
	c.JSON(http.StatusOK, body)
}
