package video

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Playlist: kumpulan video yang disusun user sendiri. Mirip Tonton nanti
// (watchlater.go), tapi satu user boleh punya banyak playlist, masing-masing
// dengan judul, deskripsi, dan visibilitas.

// Visibilitas playlist, sama dengan nilai kolom playlists.visibility.
const (
	VisibilityPublic  = "PUBLIC"
	VisibilityPrivate = "PRIVATE"
)

const (
	// maxPlaylistsPerUser dan maxPlaylistVideos membatasi isi tabel dari
	// satu akun (misalnya skrip yang membuat playlist terus-menerus).
	// Untuk perbandingan, YouTube membatasi 5000 video per playlist.
	maxPlaylistsPerUser = 200
	maxPlaylistVideos   = 1000
)

// ErrPlaylistNotFound dipakai untuk playlist yang tidak ada, PRIVATE milik
// orang lain, atau milik orang lain saat akan diubah. Ketiganya sengaja
// dibuat sama supaya orang lain tidak bisa menebak playlist privat.
var ErrPlaylistNotFound = errors.New("playlist tidak ditemukan")

// Playlist adalah satu playlist beserta ringkasan isinya.
type Playlist struct {
	ID          int64
	OwnerID     int64
	Title       string
	Description string
	Visibility  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// VideoCount dan ThumbnailKey hanya menghitung video READY. Diisi
	// lewat Repository.PlaylistStats.
	VideoCount   int64
	ThumbnailKey string
	// ContainsVideo hanya diisi ListPlaylists yang memakai videoID.
	ContainsVideo bool
}

// NewPlaylist adalah data playlist baru yang sudah divalidasi.
type NewPlaylist struct {
	OwnerID     int64
	Title       string
	Description string
	Visibility  string
}

// PlaylistStats adalah ringkasan isi satu playlist.
type PlaylistStats struct {
	VideoCount   int64
	ThumbnailKey string
}

// PlaylistEntry adalah satu video di playlist.
type PlaylistEntry struct {
	Video   Video
	AddedAt time.Time
}

// PlaylistEntryCursor menandai entri terakhir di halaman sebelumnya. Isi
// playlist diurutkan added_at lalu video_id dari kecil ke besar.
type PlaylistEntryCursor struct {
	AddedAt time.Time
	VideoID int64
}

// CreatePlaylistInput adalah isi permintaan membuat playlist.
type CreatePlaylistInput struct {
	RequesterID int64
	Title       string
	Description string
	// "" = PRIVATE.
	Visibility string
}

// UpdatePlaylistInput: field nil = tidak diubah (seperti PATCH).
type UpdatePlaylistInput struct {
	RequesterID int64
	PlaylistID  int64
	Title       *string
	Description *string
	Visibility  *string
}

// ListPlaylistsInput adalah isi permintaan daftar playlist satu user.
type ListPlaylistsInput struct {
	// 0 = pengunjung belum login.
	ViewerID int64
	OwnerID  int64
	// Opsional: isi Playlist.ContainsVideo. Hanya untuk playlist milik
	// viewer sendiri (dialog Simpan di halaman tonton).
	VideoID   int64
	PageSize  int
	PageToken string
}

// CreatePlaylist membuat playlist kosong milik requester.
func (s *Service) CreatePlaylist(ctx context.Context, in CreatePlaylistInput) (Playlist, error) {
	if in.RequesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return Playlist{}, errors.New("requester id kosong")
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return Playlist{}, err
	}
	desc, err := cleanDescription(in.Description)
	if err != nil {
		return Playlist{}, err
	}
	visibility := VisibilityPrivate
	if in.Visibility != "" {
		if visibility, err = cleanVisibility(in.Visibility); err != nil {
			return Playlist{}, err
		}
	}
	n, err := s.repo.CountPlaylists(ctx, in.RequesterID)
	if err != nil {
		return Playlist{}, err
	}
	if n >= maxPlaylistsPerUser {
		return Playlist{}, &ValidationError{"playlist", fmt.Sprintf("maksimal %d playlist per akun", maxPlaylistsPerUser)}
	}
	return s.repo.CreatePlaylist(ctx, NewPlaylist{
		OwnerID:     in.RequesterID,
		Title:       title,
		Description: desc,
		Visibility:  visibility,
	})
}

// UpdatePlaylist mengubah judul, deskripsi, dan/atau visibilitas playlist
// milik requester.
func (s *Service) UpdatePlaylist(ctx context.Context, in UpdatePlaylistInput) (Playlist, error) {
	if in.RequesterID <= 0 {
		return Playlist{}, errors.New("requester id kosong")
	}
	if in.Title == nil && in.Description == nil && in.Visibility == nil {
		return Playlist{}, &ValidationError{"title", "isi minimal title, description, atau visibility"}
	}
	p, err := s.ownedPlaylist(ctx, in.RequesterID, in.PlaylistID)
	if err != nil {
		return Playlist{}, err
	}
	if in.Title != nil {
		if p.Title, err = cleanTitle(*in.Title); err != nil {
			return Playlist{}, err
		}
	}
	if in.Description != nil {
		if p.Description, err = cleanDescription(*in.Description); err != nil {
			return Playlist{}, err
		}
	}
	if in.Visibility != nil {
		if p.Visibility, err = cleanVisibility(*in.Visibility); err != nil {
			return Playlist{}, err
		}
	}
	p.UpdatedAt = s.now().Truncate(time.Microsecond)
	if err := s.repo.UpdatePlaylist(ctx, p); err != nil {
		return Playlist{}, err
	}
	return s.withStats(ctx, p)
}

// DeletePlaylist menghapus playlist milik requester beserta isinya.
// Video-videonya sendiri tidak ikut terhapus.
func (s *Service) DeletePlaylist(ctx context.Context, requesterID, playlistID int64) error {
	if requesterID <= 0 {
		return errors.New("requester id kosong")
	}
	if _, err := s.ownedPlaylist(ctx, requesterID, playlistID); err != nil {
		return err
	}
	return s.repo.DeletePlaylist(ctx, playlistID)
}

// GetPlaylist mengambil satu playlist yang boleh dilihat viewer.
func (s *Service) GetPlaylist(ctx context.Context, viewerID, playlistID int64) (Playlist, error) {
	p, err := s.visiblePlaylist(ctx, viewerID, playlistID)
	if err != nil {
		return Playlist{}, err
	}
	return s.withStats(ctx, p)
}

// ListPlaylists mengambil playlist milik in.OwnerID, terbaru lebih dulu.
// Playlist PRIVATE hanya ikut jika yang melihat adalah pemiliknya.
func (s *Service) ListPlaylists(ctx context.Context, in ListPlaylistsInput) ([]Playlist, string, error) {
	if in.OwnerID <= 0 {
		return nil, "", ErrNotFound
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	isOwner := in.ViewerID != 0 && in.ViewerID == in.OwnerID
	if in.VideoID != 0 && !isOwner {
		return nil, "", &ValidationError{"video_id", "hanya untuk playlist milik sendiri"}
	}
	var afterID int64
	if in.PageToken != "" {
		id, err := decodePlaylistCursor(in.PageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		afterID = id
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	list, err := s.repo.ListPlaylists(ctx, in.OwnerID, isOwner, afterID, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > pageSize {
		list = list[:pageSize]
		next = encodePlaylistCursor(list[pageSize-1].ID)
	}
	if len(list) == 0 {
		return list, next, nil
	}

	ids := make([]int64, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}
	stats, err := s.repo.PlaylistStats(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	var containing map[int64]bool
	if in.VideoID > 0 {
		if containing, err = s.repo.PlaylistsContaining(ctx, ids, in.VideoID); err != nil {
			return nil, "", err
		}
	}
	for i := range list {
		st := stats[list[i].ID]
		list[i].VideoCount = st.VideoCount
		list[i].ThumbnailKey = st.ThumbnailKey
		list[i].ContainsVideo = containing[list[i].ID]
	}
	return list, next, nil
}

// AddToPlaylist menambahkan video READY ke akhir playlist milik requester
// dan mengembalikan waktu video itu ditambahkan.
//
// Aman dipanggil berulang: jika video sudah ada, posisinya tidak berubah
// dan waktu yang dikembalikan tetap waktu pertama kali ditambahkan.
func (s *Service) AddToPlaylist(ctx context.Context, requesterID, playlistID, videoID int64) (time.Time, error) {
	if requesterID <= 0 {
		return time.Time{}, errors.New("requester id kosong")
	}
	if _, err := s.ownedPlaylist(ctx, requesterID, playlistID); err != nil {
		return time.Time{}, err
	}
	if videoID <= 0 {
		return time.Time{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return time.Time{}, err
	}
	// Isi playlist hanya menampilkan video yang boleh ditonton siapa saja
	// (lihat ListPlaylistItems), jadi video draf atau Pribadi ditolak,
	// termasuk milik sendiri.
	if !v.IsWatchable() {
		return time.Time{}, ErrNotFound
	}
	if addedAt, found, err := s.repo.PlaylistItemAddedAt(ctx, playlistID, videoID); err != nil || found {
		return addedAt, err
	}
	n, err := s.repo.CountPlaylistItems(ctx, playlistID)
	if err != nil {
		return time.Time{}, err
	}
	if n >= maxPlaylistVideos {
		return time.Time{}, &ValidationError{"playlist", fmt.Sprintf("maksimal %d video per playlist", maxPlaylistVideos)}
	}
	// Dibulatkan ke mikrodetik, ketelitian TIMESTAMP bawaan Oracle, supaya
	// nilai di cursor halaman sama persis dengan isi database.
	now := s.now().Truncate(time.Microsecond)
	return s.repo.AddPlaylistItem(ctx, playlistID, videoID, now)
}

// RemoveFromPlaylist mengeluarkan video dari playlist milik requester.
// Aman dipanggil walau video itu tidak ada di playlist.
func (s *Service) RemoveFromPlaylist(ctx context.Context, requesterID, playlistID, videoID int64) error {
	if requesterID <= 0 {
		return errors.New("requester id kosong")
	}
	if _, err := s.ownedPlaylist(ctx, requesterID, playlistID); err != nil {
		return err
	}
	if videoID <= 0 {
		return nil
	}
	return s.repo.RemovePlaylistItem(ctx, playlistID, videoID, s.now().Truncate(time.Microsecond))
}

// ListPlaylistVideos mengambil isi playlist yang boleh dilihat viewer,
// sesuai urutan ditambahkan. Video yang sudah tidak READY (misalnya
// dihapus pemiliknya) tidak ikut ditampilkan.
func (s *Service) ListPlaylistVideos(ctx context.Context, viewerID, playlistID int64, pageSize int, pageToken string) ([]PlaylistEntry, string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	var after *PlaylistEntryCursor
	if pageToken != "" {
		c, err := decodePlaylistEntryCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		after = &c
	}
	if _, err := s.visiblePlaylist(ctx, viewerID, playlistID); err != nil {
		return nil, "", err
	}
	entries, err := s.repo.ListPlaylistItems(ctx, playlistID, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(entries) > pageSize {
		entries = entries[:pageSize]
		last := entries[pageSize-1]
		next = encodePlaylistEntryCursor(PlaylistEntryCursor{AddedAt: last.AddedAt, VideoID: last.Video.ID})
	}
	return entries, next, nil
}

// visiblePlaylist mengambil playlist yang boleh dilihat viewer: PUBLIC,
// atau PRIVATE milik viewer sendiri.
func (s *Service) visiblePlaylist(ctx context.Context, viewerID, playlistID int64) (Playlist, error) {
	if playlistID <= 0 {
		return Playlist{}, ErrPlaylistNotFound
	}
	p, err := s.repo.FindPlaylist(ctx, playlistID)
	if err != nil {
		return Playlist{}, err
	}
	if p.Visibility != VisibilityPublic && (viewerID == 0 || viewerID != p.OwnerID) {
		return Playlist{}, ErrPlaylistNotFound
	}
	return p, nil
}

// ownedPlaylist mengambil playlist milik requester. Playlist orang lain
// dijawab ErrPlaylistNotFound, walaupun PUBLIC.
func (s *Service) ownedPlaylist(ctx context.Context, requesterID, playlistID int64) (Playlist, error) {
	if playlistID <= 0 {
		return Playlist{}, ErrPlaylistNotFound
	}
	p, err := s.repo.FindPlaylist(ctx, playlistID)
	if err != nil {
		return Playlist{}, err
	}
	if p.OwnerID != requesterID {
		return Playlist{}, ErrPlaylistNotFound
	}
	return p, nil
}

// withStats melengkapi satu playlist dengan jumlah video dan thumbnail.
func (s *Service) withStats(ctx context.Context, p Playlist) (Playlist, error) {
	stats, err := s.repo.PlaylistStats(ctx, []int64{p.ID})
	if err != nil {
		return Playlist{}, err
	}
	p.VideoCount = stats[p.ID].VideoCount
	p.ThumbnailKey = stats[p.ID].ThumbnailKey
	return p, nil
}

func cleanVisibility(raw string) (string, error) {
	switch v := strings.ToUpper(strings.TrimSpace(raw)); v {
	case VisibilityPublic, VisibilityPrivate:
		return v, nil
	}
	return "", &ValidationError{"visibility", "harus PUBLIC atau PRIVATE"}
}

// encodePlaylistCursor mengubah id playlist terakhir menjadi base64 dari
// "p<id>". Daftar playlist diurutkan id dari besar ke kecil (terbaru dulu).
func encodePlaylistCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("p" + strconv.FormatInt(id, 10)))
}

func decodePlaylistCursor(token string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	body, ok := strings.CutPrefix(string(raw), "p")
	if !ok {
		return 0, errors.New("bukan token playlist")
	}
	id, err := strconv.ParseInt(body, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("id cursor salah")
	}
	return id, nil
}

// encodePlaylistEntryCursor mengubah cursor menjadi base64 dari
// "i<added_at dalam mikrodetik>.<video_id>".
func encodePlaylistEntryCursor(c PlaylistEntryCursor) string {
	raw := "i" + strconv.FormatInt(c.AddedAt.UnixMicro(), 10) + "." + strconv.FormatInt(c.VideoID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodePlaylistEntryCursor(token string) (PlaylistEntryCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return PlaylistEntryCursor{}, err
	}
	body, ok := strings.CutPrefix(string(raw), "i")
	if !ok {
		return PlaylistEntryCursor{}, errors.New("bukan token isi playlist")
	}
	micros, id, ok := strings.Cut(body, ".")
	if !ok {
		return PlaylistEntryCursor{}, errors.New("format cursor salah")
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil || us <= 0 {
		return PlaylistEntryCursor{}, errors.New("waktu cursor salah")
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return PlaylistEntryCursor{}, errors.New("id cursor salah")
	}
	return PlaylistEntryCursor{AddedAt: time.UnixMicro(us).UTC(), VideoID: n}, nil
}
