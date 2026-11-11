package httpserver

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// maxPlaylistBodyBytes cukup untuk judul 100 karakter dan deskripsi 1000
// karakter (maksimal 4 byte per karakter UTF-8) beserta JSON-nya.
const maxPlaylistBodyBytes = 8 << 10 // 8 KiB

// playlistResponse adalah satu playlist di JSON.
type playlistResponse struct {
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// "PUBLIC" atau "PRIVATE".
	Visibility string `json:"visibility"`
	// Jumlah video yang bisa diputar (READY).
	VideoCount int64 `json:"video_count"`
	// Thumbnail video pertama. "" jika playlist masih kosong.
	ThumbnailURL string    `json:"thumbnail_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// Hanya ada di GET /me/playlists?video_id=...: true jika video itu ada
	// di playlist ini (centang di dialog Simpan).
	ContainsVideo *bool `json:"contains_video,omitempty"`
	// Hanya ada di GET /playlists/:id. Kosong jika UserService gagal.
	Owner *ownerResponse `json:"owner,omitempty"`
}

type listPlaylistsResponse struct {
	Playlists []playlistResponse `json:"playlists"`
	// Kosong jika sudah halaman terakhir.
	NextPageToken string `json:"next_page_token"`
}

type playlistEntryResponse struct {
	Video   videoResponse `json:"video"`
	AddedAt time.Time     `json:"added_at"`
}

type listPlaylistVideosResponse struct {
	Entries       []playlistEntryResponse `json:"entries"`
	NextPageToken string                  `json:"next_page_token"`
}

// savePlaylistVideoResponse adalah jawaban PUT dan DELETE
// /playlists/:id/videos/:videoId, bentuknya sama dengan Tonton nanti.
type savePlaylistVideoResponse struct {
	Saved   bool       `json:"saved"`
	AddedAt *time.Time `json:"added_at,omitempty"`
}

type createPlaylistRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// "PUBLIC" atau "PRIVATE". Kosong = PRIVATE.
	Visibility string `json:"visibility"`
}

// updatePlaylistRequest memakai *string: nil = tidak diubah (sama seperti
// updateVideoRequest).
type updatePlaylistRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Visibility  *string `json:"visibility"`
}

// createPlaylist: POST /api/v1/playlists. Wajib login. Menjawab 201.
func (h *videoHandler) createPlaylist(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req createPlaylistRequest
	if !bindJSON(c, &req) {
		return
	}
	visibility, ok := visibilityParam(c, req.Visibility)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.CreatePlaylist(ctx, &videov1.CreatePlaylistRequest{
		RequesterId: userID,
		Title:       req.Title,
		Description: req.Description,
		Visibility:  visibility,
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"playlist": toPlaylistResponse(resp.GetPlaylist())})
}

// getPlaylist: GET /api/v1/playlists/:id. Boleh tanpa login; playlist
// PRIVATE hanya untuk pemiliknya (orang lain mendapat 404).
func (h *videoHandler) getPlaylist(c *gin.Context) {
	playlistID, ok := playlistIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.GetPlaylist(ctx, &videov1.GetPlaylistRequest{ViewerId: optionalUserID(c), PlaylistId: playlistID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	p := resp.GetPlaylist()
	body := toPlaylistResponse(p)
	body.Owner = lookupProfiles(c.Request.Context(), h.users, h.logger, []int64{p.GetOwnerId()})[p.GetOwnerId()]
	c.JSON(http.StatusOK, gin.H{"playlist": body})
}

// updatePlaylist: PATCH /api/v1/playlists/:id. Wajib login, hanya pemilik.
func (h *videoHandler) updatePlaylist(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	playlistID, ok := playlistIDParam(c)
	if !ok {
		return
	}
	var req updatePlaylistRequest
	if !bindJSON(c, &req) {
		return
	}
	in := &videov1.UpdatePlaylistRequest{
		RequesterId: userID,
		PlaylistId:  playlistID,
		Title:       req.Title,
		Description: req.Description,
	}
	if req.Visibility != nil {
		// Dikirim tapi kosong juga dianggap salah: UNSPECIFIED di gRPC
		// berarti "tidak diubah", bukan nilai yang boleh disimpan.
		if strings.TrimSpace(*req.Visibility) == "" {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "visibility: harus PUBLIC atau PRIVATE")
			return
		}
		if in.Visibility, ok = visibilityParam(c, *req.Visibility); !ok {
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.UpdatePlaylist(ctx, in)
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"playlist": toPlaylistResponse(resp.GetPlaylist())})
}

// deletePlaylist: DELETE /api/v1/playlists/:id. Wajib login, hanya
// pemilik. Menjawab 204 (tanpa body).
func (h *videoHandler) deletePlaylist(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	playlistID, ok := playlistIDParam(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	if _, err := h.client.DeletePlaylist(ctx, &videov1.DeletePlaylistRequest{RequesterId: userID, PlaylistId: playlistID}); err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// playlistVideos: GET /api/v1/playlists/:id/videos?page_size=20&page_token=...
// Boleh tanpa login (aturan PRIVATE sama dengan getPlaylist).
func (h *videoHandler) playlistVideos(c *gin.Context) {
	playlistID, ok := playlistIDParam(c)
	if !ok {
		return
	}
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListPlaylistVideos(ctx, &videov1.ListPlaylistVideosRequest{
		ViewerId:   optionalUserID(c),
		PlaylistId: playlistID,
		PageSize:   pageSize,
		PageToken:  c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	entries := resp.GetEntries()
	videos := make([]*videov1.Video, len(entries))
	for i, e := range entries {
		videos[i] = e.GetVideo()
	}
	owners := lookupOwners(c.Request.Context(), h.users, h.logger, videos)

	body := listPlaylistVideosResponse{Entries: make([]playlistEntryResponse, 0, len(entries)), NextPageToken: resp.GetNextPageToken()}
	for _, e := range entries {
		v := toVideoResponse(e.GetVideo())
		v.Owner = owners[e.GetVideo().GetOwnerId()]
		body.Entries = append(body.Entries, playlistEntryResponse{Video: v, AddedAt: e.GetAddedAt().AsTime()})
	}
	c.JSON(http.StatusOK, body)
}

// addPlaylistVideo: PUT /api/v1/playlists/:id/videos/:videoId (tanpa
// body). Wajib login, hanya pemilik playlist. Aman diulang.
func (h *videoHandler) addPlaylistVideo(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	playlistID, videoID, ok := playlistVideoParams(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.AddToPlaylist(ctx, &videov1.AddToPlaylistRequest{RequesterId: userID, PlaylistId: playlistID, VideoId: videoID})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	addedAt := resp.GetAddedAt().AsTime()
	c.JSON(http.StatusOK, savePlaylistVideoResponse{Saved: true, AddedAt: &addedAt})
}

// removePlaylistVideo: DELETE /api/v1/playlists/:id/videos/:videoId.
// Wajib login, hanya pemilik playlist. Aman diulang.
func (h *videoHandler) removePlaylistVideo(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	playlistID, videoID, ok := playlistVideoParams(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	if _, err := h.client.RemoveFromPlaylist(ctx, &videov1.RemoveFromPlaylistRequest{RequesterId: userID, PlaylistId: playlistID, VideoId: videoID}); err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, savePlaylistVideoResponse{Saved: false})
}

// myPlaylists: GET /api/v1/me/playlists?video_id=...&page_size=...
// Wajib login, termasuk playlist PRIVATE. Dengan video_id, setiap
// playlist membawa contains_video (dialog Simpan).
func (h *videoHandler) myPlaylists(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var videoID int64
	if raw := c.Query("video_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "video_id harus berupa angka")
			return
		}
		videoID = id
	}
	h.listPlaylists(c, userID, userID, videoID)
}

// listPlaylists menjawab daftar playlist milik ownerID yang boleh dilihat
// viewerID. Dipakai /me/playlists dan /users/:username/playlists.
func (h *videoHandler) listPlaylists(c *gin.Context, viewerID, ownerID, videoID int64) {
	pageSize, ok := pageSizeQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), videoCallTimeout)
	defer cancel()

	resp, err := h.client.ListPlaylists(ctx, &videov1.ListPlaylistsRequest{
		ViewerId:  viewerID,
		OwnerId:   ownerID,
		VideoId:   videoID,
		PageSize:  pageSize,
		PageToken: c.Query("page_token"),
	})
	if err != nil {
		writeGRPCError(c, h.logger, err)
		return
	}
	body := listPlaylistsResponse{Playlists: make([]playlistResponse, 0, len(resp.GetPlaylists())), NextPageToken: resp.GetNextPageToken()}
	for _, p := range resp.GetPlaylists() {
		out := toPlaylistResponse(p)
		if videoID != 0 {
			contains := p.GetContainsVideo()
			out.ContainsVideo = &contains
		}
		body.Playlists = append(body.Playlists, out)
	}
	c.JSON(http.StatusOK, body)
}

// listPlaylists: GET /api/v1/users/:username/playlists. Boleh tanpa login;
// playlist PRIVATE hanya ikut jika yang membuka adalah pemilik kanal.
func (h *userHandler) listPlaylists(c *gin.Context) {
	p, ok := h.lookup(c)
	if !ok {
		return
	}
	h.videos.listPlaylists(c, optionalUserID(c), p.GetId(), 0)
}

// playlistIDParam membaca :id dari URL. Id yang bukan angka dijawab 404.
func playlistIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "playlist tidak ditemukan")
		return 0, false
	}
	return id, true
}

func playlistVideoParams(c *gin.Context) (playlistID, videoID int64, ok bool) {
	if playlistID, ok = playlistIDParam(c); !ok {
		return 0, 0, false
	}
	videoID, err := strconv.ParseInt(c.Param("videoId"), 10, 64)
	if err != nil || videoID <= 0 {
		abortError(c, http.StatusNotFound, "NOT_FOUND", "video tidak ditemukan")
		return 0, 0, false
	}
	return playlistID, videoID, true
}

// visibilityParam mengubah "PUBLIC"/"PRIVATE" (huruf besar/kecil sama
// saja) menjadi enum gRPC. "" = UNSPECIFIED. Nilai lain dijawab 400.
func visibilityParam(c *gin.Context, raw string) (videov1.PlaylistVisibility, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "":
		return videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_UNSPECIFIED, true
	case "PUBLIC":
		return videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PUBLIC, true
	case "PRIVATE":
		return videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PRIVATE, true
	}
	abortError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "visibility: harus PUBLIC atau PRIVATE")
	return 0, false
}

var visibilityNames = map[videov1.PlaylistVisibility]string{
	videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PUBLIC:  "PUBLIC",
	videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PRIVATE: "PRIVATE",
}

func toPlaylistResponse(p *videov1.Playlist) playlistResponse {
	visibility, ok := visibilityNames[p.GetVisibility()]
	if !ok {
		visibility = "PRIVATE" // tidak pernah terjadi; pilih yang paling aman
	}
	return playlistResponse{
		ID:           strconv.FormatInt(p.GetId(), 10),
		OwnerID:      strconv.FormatInt(p.GetOwnerId(), 10),
		Title:        p.GetTitle(),
		Description:  p.GetDescription(),
		Visibility:   visibility,
		VideoCount:   p.GetVideoCount(),
		ThumbnailURL: p.GetThumbnailUrl(),
		CreatedAt:    p.GetCreatedAt().AsTime(),
		UpdatedAt:    p.GetUpdatedAt().AsTime(),
	}
}
