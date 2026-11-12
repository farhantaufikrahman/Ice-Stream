package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

const (
	// Body JSON ubah profil hanya berisi nama tampilan dan bio.
	maxProfileBodyBytes = 8 << 10 // 8 KiB
	// maxAvatarBodyBytes sama dengan batas di UserService (2 MiB). Foto
	// profil kecil, jadi boleh lewat gateway, berbeda dengan file video
	// yang langsung ke storage.
	maxAvatarBodyBytes = 2 << 20
	// Upload foto profil butuh waktu lebih lama (gambar diproses ulang).
	avatarCallTimeout = 15 * time.Second
	// ownerLookupTimeout membatasi waktu mengambil info pemilik video. Kalau
	// lewat, feed tetap dikirim tanpa info pemilik.
	ownerLookupTimeout = 2 * time.Second
)

// userHandler melayani profil dan halaman channel.
type userHandler struct {
	users  userv1.UserServiceClient
	videos *videoHandler
	logger *slog.Logger
}

// profileResponse adalah profil di JSON. Email hanya ada di /me/profile.
type profileResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	// AvatarURL kosong ("") jika belum ada foto profil; frontend
	// menampilkan gambar default.
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
	Email     string    `json:"email,omitempty"`
	// SubscriberCount adalah jumlah subscriber (selalu ada).
	SubscriberCount int64 `json:"subscriber_count"`
	// Role hanya ada di GET /me/profile (langkah 28): "USER" atau "ADMIN",
	// dibaca dari access token. Frontend memakainya untuk menampilkan menu
	// moderasi; izin yang sebenarnya tetap dicek RequireAdmin di server.
	Role string `json:"role,omitempty"`
}

// subscriptionResponse adalah jawaban PUT dan DELETE
// /users/:username/subscription.
type subscriptionResponse struct {
	Subscribed      bool  `json:"subscribed"`
	SubscriberCount int64 `json:"subscriber_count"`
}

// listSubscriptionsResponse adalah jawaban GET /me/subscriptions.
type listSubscriptionsResponse struct {
	Channels      []profileResponse `json:"channels"`
	NextPageToken string            `json:"next_page_token"`
}

// ownerResponse adalah ringkasan pemilik video untuk kartu di feed.
type ownerResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

// updateProfileRequest memakai *string: nil = field tidak dikirim (jangan
// diubah), "" = dikirim kosong (misalnya menghapus bio). Di Node.js ini
// seperti membedakan body.bio === undefined dan body.bio === "".
type updateProfileRequest struct {
	DisplayName *string `json:"display_name"`
	Bio         *string `json:"bio"`
}

// getByUsername: GET /api/v1/users/:username (halaman channel). Login tidak
// wajib; jika ada access token, jawaban juga berisi viewer_subscribed.
func (h *userHandler) getByUsername(c *gin.Context) {
	viewerID := optionalUserID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.users.GetProfile(ctx, &userv1.GetProfileRequest{
		User:     &userv1.GetProfileRequest_Username{Username: c.Param("username")},
		ViewerId: viewerID,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	user := toProfileResponse(resp.GetProfile())
	// UserService mengisi email jika viewer melihat profilnya sendiri.
	// Endpoint ini publik (bisa di-cache atau dibagikan), jadi email dibuang.
	user.Email = ""
	body := gin.H{"user": user}
	if viewerID != 0 {
		body["viewer_subscribed"] = resp.GetViewerSubscribed()
	}
	// Angka kanal dan Video unggulan (langkah 27). Jika VideoService gagal,
	// keduanya tidak dikirim dan halaman kanal tampil tanpa bagian itu.
	if stats, featured, ok := h.videos.channelSummary(c.Request.Context(), resp.GetProfile().GetId()); ok {
		body["stats"] = stats
		body["featured_video"] = featured
	}
	c.JSON(http.StatusOK, body)
}

// subscribe: PUT /api/v1/users/:username/subscription (tanpa body). Wajib login.
func (h *userHandler) subscribe(c *gin.Context) {
	h.changeSubscription(c, true)
}

// unsubscribe: DELETE /api/v1/users/:username/subscription. Wajib login.
func (h *userHandler) unsubscribe(c *gin.Context) {
	h.changeSubscription(c, false)
}

func (h *userHandler) changeSubscription(c *gin.Context, subscribe bool) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	channel, ok := h.lookup(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	var out subscriptionResponse
	if subscribe {
		resp, err := h.users.Subscribe(ctx, &userv1.SubscribeRequest{RequesterId: userID, ChannelId: channel.GetId()})
		if err != nil {
			writeGRPCError(c, h.logger, err)
			return
		}
		out = subscriptionResponse{Subscribed: resp.GetSubscribed(), SubscriberCount: resp.GetSubscriberCount()}
	} else {
		resp, err := h.users.Unsubscribe(ctx, &userv1.UnsubscribeRequest{RequesterId: userID, ChannelId: channel.GetId()})
		if err != nil {
			writeGRPCError(c, h.logger, err)
			return
		}
		out = subscriptionResponse{Subscribed: resp.GetSubscribed(), SubscriberCount: resp.GetSubscriberCount()}
	}
	c.JSON(http.StatusOK, out)
}

// subscriptions: GET /api/v1/me/subscriptions?page_size=&page_token=
// Channel yang saya ikuti, yang terakhir di-subscribe lebih dulu.
func (h *userHandler) subscriptions(c *gin.Context) {
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
	resp, err := h.users.ListSubscriptions(ctx, &userv1.ListSubscriptionsRequest{
		RequesterId: userID,
		PageSize:    pageSize,
		PageToken:   c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	out := listSubscriptionsResponse{Channels: make([]profileResponse, 0, len(resp.GetChannels())), NextPageToken: resp.GetNextPageToken()}
	for _, p := range resp.GetChannels() {
		out.Channels = append(out.Channels, toProfileResponse(p))
	}
	c.JSON(http.StatusOK, out)
}

// feed: GET /api/v1/me/feed?page_size=&page_token=
// Video terbaru dari semua channel yang saya ikuti.
//
// Cara ini disebut "fan-out on read": daftar channel diambil dari
// UserService, lalu VideoService mencari video milik channel-channel itu
// saat feed dibuka. Untuk skala sangat besar (jutaan subscriber), aplikasi
// seperti Twitter memakai "fan-out on write": saat video baru terbit, id-nya
// langsung ditulis ke kotak feed setiap subscriber.
func (h *userHandler) feed(c *gin.Context) {
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

	subs, err := h.users.ListSubscribedChannelIds(ctx, &userv1.ListSubscribedChannelIdsRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	if len(subs.GetChannelIds()) == 0 {
		// Belum mengikuti channel apa pun. Jangan panggil ListVideos: daftar
		// owner yang kosong di sana berarti "semua user".
		c.JSON(http.StatusOK, toListResponse(nil, ""))
		return
	}
	resp, err := h.videos.client.ListVideos(ctx, &videov1.ListVideosRequest{
		OwnerIds:  subs.GetChannelIds(),
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
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

// listVideos: GET /api/v1/users/:username/videos?page_size=&page_token=
// Video READY milik channel ini, terbaru lebih dulu.
func (h *userHandler) listVideos(c *gin.Context) {
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	// ?sort=popular: tab Terpopuler (langkah 24a). Bawaan: terbaru.
	sort, ok := enumQuery(c, "sort", channelSortParams, "newest, popular")
	if !ok {
		return
	}
	p, ok := h.lookup(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.videos.client.ListVideos(ctx, &videov1.ListVideosRequest{
		OwnerId:   p.GetId(),
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
		Sort:      sort,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	// Semua video milik user yang sama, jadi pemiliknya tidak perlu dicari lagi.
	owner := toOwnerResponse(p)
	body := toListResponse(resp.GetVideos(), resp.GetNextPageToken())
	for i := range body.Videos {
		body.Videos[i].Owner = owner
	}
	c.JSON(http.StatusOK, body)
}

// lookup mencari profil dari :username, misalnya untuk mendapatkan id
// channel. Viewer selalu 0: yang dibutuhkan hanya data publik.
func (h *userHandler) lookup(c *gin.Context) (*userv1.Profile, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.users.GetProfile(ctx, &userv1.GetProfileRequest{
		User: &userv1.GetProfileRequest_Username{Username: c.Param("username")},
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return nil, false
	}
	return resp.GetProfile(), true
}

// me: GET /api/v1/me/profile (wajib login), termasuk email.
func (h *userHandler) me(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.users.GetProfile(ctx, &userv1.GetProfileRequest{
		User:     &userv1.GetProfileRequest_UserId{UserId: userID},
		ViewerId: userID,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	out := toProfileResponse(resp.GetProfile())
	if claims, ok := CurrentUser(c); ok {
		out.Role = claims.Role
	}
	c.JSON(http.StatusOK, gin.H{"user": out})
}

// update: PATCH /api/v1/me/profile {"display_name": "...", "bio": "..."}
// Kirim hanya field yang ingin diubah.
func (h *userHandler) update(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req updateProfileRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.users.UpdateProfile(ctx, &userv1.UpdateProfileRequest{
		RequesterId: userID,
		DisplayName: req.DisplayName,
		Bio:         req.Bio,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toProfileResponse(resp.GetProfile())})
}

// uploadAvatar: PUT /api/v1/me/avatar, body = isi file gambar apa adanya.
// Dari browser: fetch(url, {method: "PUT", headers: {Authorization: ...}, body: file}).
// UserService yang memeriksa isi file, jadi header Content-Type tidak
// dipercaya.
func (h *userHandler) uploadAvatar(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	// MaxBytesReader berhenti membaca setelah 2 MiB, jadi file raksasa
	// tidak pernah masuk ke memori gateway.
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarBodyBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		abortError(c, http.StatusRequestEntityTooLarge, "TOO_LARGE", "foto profil maksimal 2 MiB")
		return
	case err != nil:
		abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "gagal membaca file")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), avatarCallTimeout)
	defer cancel()
	resp, err := h.users.UploadAvatar(ctx, &userv1.UploadAvatarRequest{RequesterId: userID, Image: data})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toProfileResponse(resp.GetProfile())})
}

// removeAvatar: DELETE /api/v1/me/avatar.
func (h *userHandler) removeAvatar(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()
	resp, err := h.users.RemoveAvatar(ctx, &userv1.RemoveAvatarRequest{RequesterId: userID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toProfileResponse(resp.GetProfile())})
}

// lookupOwners mengambil info pemilik untuk sekumpulan video dengan SATU
// panggilan BatchGetProfiles, bukan satu panggilan per video (masalah
// "N+1"). Pola menggabungkan data dari beberapa service di gateway ini
// disebut API composition.
//
// Info pemilik hanya pelengkap: kalau UserService lambat atau mati, hasilnya
// kosong dan feed tetap tampil (frontend menampilkan nama default).
func lookupOwners(ctx context.Context, users userv1.UserServiceClient, logger *slog.Logger, videos []*videov1.Video) map[int64]*ownerResponse {
	ids := make([]int64, 0, len(videos))
	for _, v := range videos {
		ids = append(ids, v.GetOwnerId())
	}
	return lookupProfiles(ctx, users, logger, ids)
}

// lookupProfiles mengambil ringkasan profil untuk sekumpulan id user
// (pemilik video, penulis komentar) dengan satu panggilan BatchGetProfiles.
// Id yang sama cukup dikirim sekali; UserService juga membuang duplikat,
// tapi request yang lebih kecil lebih hemat. Hasilnya nil jika UserService
// gagal menjawab.
func lookupProfiles(ctx context.Context, users userv1.UserServiceClient, logger *slog.Logger, ids []int64) map[int64]*ownerResponse {
	if users == nil || len(ids) == 0 {
		return nil
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	resp, err := users.BatchGetProfiles(ctx, &userv1.BatchGetProfilesRequest{UserIds: ids})
	if err != nil {
		logger.WarnContext(ctx, "gagal mengambil info profil", slog.Any("error", err))
		return nil
	}
	owners := make(map[int64]*ownerResponse, len(resp.GetProfiles()))
	for _, p := range resp.GetProfiles() {
		owners[p.GetId()] = toOwnerResponse(p)
	}
	return owners
}

func toProfileResponse(p *userv1.Profile) profileResponse {
	return profileResponse{
		ID:          strconv.FormatInt(p.GetId(), 10),
		Username:    p.GetUsername(),
		DisplayName: p.GetDisplayName(),
		Bio:         p.GetBio(),
		AvatarURL:   p.GetAvatarUrl(),
		CreatedAt:   p.GetCreatedAt().AsTime(),
		Email:       p.GetEmail(),
		// Jumlah subscriber, sama untuk semua orang.
		SubscriberCount: p.GetSubscriberCount(),
	}
}

func toOwnerResponse(p *userv1.Profile) *ownerResponse {
	return &ownerResponse{
		ID:          strconv.FormatInt(p.GetId(), 10),
		Username:    p.GetUsername(),
		DisplayName: p.GetDisplayName(),
		AvatarURL:   p.GetAvatarUrl(),
	}
}
