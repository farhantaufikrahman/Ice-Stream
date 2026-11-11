package video

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Repository adalah semua akses database yang dibutuhkan Service.
// Implementasi Oracle ada di oracle_repository.go; test memakai versi palsu.
type Repository interface {
	// Create menyimpan video baru dengan status PENDING_UPLOAD.
	Create(ctx context.Context, v NewVideo) (Video, error)
	// FindByID mencari video yang belum dihapus. ErrNotFound jika tidak ada.
	FindByID(ctx context.Context, id int64) (Video, error)
	// MarkUploaded mengubah status PENDING_UPLOAD menjadi UPLOADED.
	// ErrNotUploading jika statusnya sudah bukan PENDING_UPLOAD.
	MarkUploaded(ctx context.Context, id int64, now time.Time) (Video, error)
	// ListByOwner mengambil video milik ownerID, id terbesar lebih dulu,
	// hanya yang id-nya lebih kecil dari beforeID (0 = tanpa batas).
	ListByOwner(ctx context.Context, ownerID, beforeID int64, limit int) ([]Video, error)
	// ListPublic mengambil video READY, processed_at lalu id terbesar lebih
	// dulu, hanya milik salah satu ownerIDs (kosong = semua user) dan hanya
	// yang posisinya setelah after (nil = dari awal).
	ListPublic(ctx context.Context, ownerIDs []int64, after *FeedCursor, limit int) ([]Video, error)
	// IncrementViews menambah view_count video READY sebanyak 1 dan
	// mengembalikan nilai barunya. ErrNotFound jika video tidak READY.
	IncrementViews(ctx context.Context, id int64) (int64, error)
	// Like mencatat like userID untuk video READY dan mengembalikan
	// like_count terbaru. Like kedua tidak menambah apa-apa.
	// ErrNotFound jika video tidak READY.
	Like(ctx context.Context, videoID, userID int64) (int64, error)
	// Unlike menghapus like userID (jika ada) dan mengembalikan like_count
	// terbaru. ErrNotFound jika video tidak READY.
	Unlike(ctx context.Context, videoID, userID int64) (int64, error)
	// IsLiked mengembalikan true jika userID sudah like videoID.
	IsLiked(ctx context.Context, videoID, userID int64) (bool, error)
	// MarkProcessing mengubah UPLOADED menjadi PROCESSING. Mengembalikan
	// false (tanpa error) jika statusnya sudah bukan UPLOADED.
	MarkProcessing(ctx context.Context, id int64, now time.Time) (bool, error)
	// MarkReady mengubah UPLOADED/PROCESSING menjadi READY dan menyimpan
	// hasil transcoding. false jika statusnya sudah bukan keduanya.
	MarkReady(ctx context.Context, id int64, r TranscodeResult, now time.Time) (bool, error)
	// MarkFailed mengubah UPLOADED/PROCESSING menjadi FAILED. false jika
	// statusnya sudah bukan keduanya.
	MarkFailed(ctx context.Context, id int64, reason string, now time.Time) (bool, error)
	// Search mengambil video READY yang punya kata berawalan SETIAP kata di
	// terms, urut skor (jumlah bobot kata yang cocok) lalu views terbanyak.
	Search(ctx context.Context, terms []string, offset, limit int) ([]Video, error)
	// ListUnindexed mengambil video yang kata-katanya belum disimpan
	// (dibuat sebelum fitur pencarian ada), id terkecil lebih dulu.
	ListUnindexed(ctx context.Context, limit int) ([]Video, error)
	// SaveSearchTerms menyimpan kata-kata video yang belum diindeks.
	// Mengembalikan false (tanpa error) jika video itu ternyata sudah
	// diindeks, misalnya oleh pod VideoService lain.
	SaveSearchTerms(ctx context.Context, videoID int64, terms []SearchTerm, now time.Time) (bool, error)
	// SaveProgress menyimpan posisi tonton userID untuk videoID: membuat
	// entri riwayat baru, atau memperbarui yang sudah ada.
	SaveProgress(ctx context.Context, userID, videoID, positionMs int64, now time.Time) error
	// GetProgress mengembalikan posisi tonton terakhir userID untuk
	// videoID. found = false jika video itu belum pernah ditonton.
	GetProgress(ctx context.Context, userID, videoID int64) (positionMs int64, found bool, err error)
	// ListHistory mengambil riwayat userID untuk video READY, watched_at
	// lalu video_id terbesar lebih dulu, hanya yang posisinya setelah after
	// (nil = dari awal).
	ListHistory(ctx context.Context, userID int64, after *HistoryCursor, limit int) ([]HistoryEntry, error)
	// DeleteHistory menghapus entri riwayat userID untuk videoID, atau
	// semua entri userID jika videoID = 0. Mengembalikan jumlah baris yang
	// dihapus.
	DeleteHistory(ctx context.Context, userID, videoID int64) (int64, error)
	// UpdateDetails mengganti judul, deskripsi, dan kata pencarian video
	// milik ownerID dalam satu transaksi. ErrNotFound jika video tidak ada,
	// sudah dihapus, atau bukan milik ownerID.
	UpdateDetails(ctx context.Context, id, ownerID int64, title, description string, terms []SearchTerm, now time.Time) (Video, error)
	// SoftDelete mengisi deleted_at video milik ownerID dan menghapus kata
	// pencarian-nya, hanya jika statusnya ada di allowed. ErrNotFound jika
	// video tidak ada, sudah dihapus, atau bukan milik ownerID;
	// ErrVideoBusy jika statusnya tidak ada di allowed.
	SoftDelete(ctx context.Context, id, ownerID int64, allowed []string, now time.Time) error
	// ListUnpurged mengambil video yang sudah dihapus tapi filenya belum
	// dihapus dari storage, id terkecil lebih dulu.
	ListUnpurged(ctx context.Context, limit int) ([]Video, error)
	// MarkPurged mengisi media_purged_at video yang sudah dihapus.
	MarkPurged(ctx context.Context, id int64, now time.Time) error
	// InsertNotifications menyimpan notifikasi kind untuk setiap userIDs,
	// kecuali user yang sudah punya notifikasi kind untuk videoID.
	// Mengembalikan jumlah baris baru.
	InsertNotifications(ctx context.Context, kind string, videoID, actorID int64, userIDs []int64, now time.Time) (int64, error)
	// ListNotifications mengambil notifikasi userID untuk video READY, id
	// terbesar lebih dulu, hanya yang id-nya lebih kecil dari beforeID
	// (0 = dari yang terbaru).
	ListNotifications(ctx context.Context, userID, beforeID int64, limit int) ([]Notification, error)
	// CountUnreadNotifications menghitung notifikasi userID yang belum
	// dibaca untuk video READY, maksimal limit.
	CountUnreadNotifications(ctx context.Context, userID int64, limit int) (int64, error)
	// MarkNotificationsRead mengisi read_at satu notifikasi userID
	// (notificationID > 0) atau semuanya (0), hanya yang belum dibaca.
	// Mengembalikan jumlah baris yang berubah.
	MarkNotificationsRead(ctx context.Context, userID, notificationID int64, now time.Time) (int64, error)
	// AddWatchLater menyimpan videoID ke Tonton nanti userID dengan waktu
	// now, kecuali sudah tersimpan. Mengembalikan added_at yang tersimpan
	// (waktu pertama kali disimpan).
	AddWatchLater(ctx context.Context, userID, videoID int64, now time.Time) (time.Time, error)
	// RemoveWatchLater menghapus videoID dari Tonton nanti userID (jika ada).
	RemoveWatchLater(ctx context.Context, userID, videoID int64) error
	// IsInWatchLater mengembalikan true jika userID sudah menyimpan videoID.
	IsInWatchLater(ctx context.Context, userID, videoID int64) (bool, error)
	// ListWatchLater mengambil Tonton nanti userID untuk video READY,
	// added_at lalu video_id terbesar lebih dulu, hanya yang posisinya
	// setelah after (nil = dari awal).
	ListWatchLater(ctx context.Context, userID int64, after *WatchLaterCursor, limit int) ([]WatchLaterEntry, error)

	// Playlist (playlist_repository.go).

	// CreatePlaylist menyimpan playlist baru tanpa isi.
	CreatePlaylist(ctx context.Context, p NewPlaylist) (Playlist, error)
	// CountPlaylists menghitung semua playlist milik ownerID.
	CountPlaylists(ctx context.Context, ownerID int64) (int64, error)
	// FindPlaylist mencari satu playlist (tanpa VideoCount dan
	// ThumbnailKey). ErrPlaylistNotFound jika tidak ada.
	FindPlaylist(ctx context.Context, id int64) (Playlist, error)
	// UpdatePlaylist menyimpan judul, deskripsi, visibilitas, dan
	// updated_at dari p.
	UpdatePlaylist(ctx context.Context, p Playlist) error
	// DeletePlaylist menghapus playlist beserta isinya.
	DeletePlaylist(ctx context.Context, id int64) error
	// ListPlaylists mengambil playlist ownerID dengan id lebih kecil dari
	// afterID (0 = dari awal), id terbesar lebih dulu. PRIVATE hanya ikut
	// jika includePrivate.
	ListPlaylists(ctx context.Context, ownerID int64, includePrivate bool, afterID int64, limit int) ([]Playlist, error)
	// PlaylistStats menghitung video READY dan thumbnail video pertama
	// untuk setiap playlist. Playlist kosong tidak ada di map.
	PlaylistStats(ctx context.Context, playlistIDs []int64) (map[int64]PlaylistStats, error)
	// PlaylistsContaining mengembalikan playlist (dari playlistIDs) yang
	// berisi videoID.
	PlaylistsContaining(ctx context.Context, playlistIDs []int64, videoID int64) (map[int64]bool, error)
	// IsInAnyPlaylist mengembalikan true jika videoID ada di salah satu
	// playlist milik ownerID.
	IsInAnyPlaylist(ctx context.Context, ownerID, videoID int64) (bool, error)
	// PlaylistItemAddedAt membaca waktu videoID ditambahkan ke playlist.
	// found = false jika belum ada.
	PlaylistItemAddedAt(ctx context.Context, playlistID, videoID int64) (addedAt time.Time, found bool, err error)
	// CountPlaylistItems menghitung semua baris isi playlist.
	CountPlaylistItems(ctx context.Context, playlistID int64) (int64, error)
	// AddPlaylistItem menambahkan videoID ke playlist dengan waktu now
	// (kecuali sudah ada) dan memperbarui updated_at playlist. Mengembalikan
	// added_at yang tersimpan.
	AddPlaylistItem(ctx context.Context, playlistID, videoID int64, now time.Time) (time.Time, error)
	// RemovePlaylistItem mengeluarkan videoID dari playlist (jika ada) dan
	// memperbarui updated_at playlist.
	RemovePlaylistItem(ctx context.Context, playlistID, videoID int64, now time.Time) error
	// ListPlaylistItems mengambil isi playlist untuk video READY, added_at
	// lalu video_id terkecil lebih dulu, hanya yang posisinya setelah after
	// (nil = dari awal).
	ListPlaylistItems(ctx context.Context, playlistID int64, after *PlaylistEntryCursor, limit int) ([]PlaylistEntry, error)
}

// Publisher mengirim event ke service lain. Implementasi NATS ada di
// nats.go; test memakai versi palsu.
type Publisher interface {
	// PublishUploaded memberi tahu transcoder bahwa file video siap diproses.
	// Boleh dipanggil lebih dari sekali untuk video yang sama.
	PublishUploaded(ctx context.Context, v Video) error
	// PublishPublished memberi tahu bahwa video sudah READY (bisa ditonton).
	// Boleh dipanggil lebih dari sekali untuk video yang sama.
	PublishPublished(ctx context.Context, v Video) error
}

// ViewDeduper mencatat penonton yang baru saja menonton sebuah video,
// supaya refresh berulang tidak menambah views. Implementasi NATS ada di
// views.go; test memakai versi palsu.
type ViewDeduper interface {
	// Claim mengembalikan true jika ini view pertama viewerKey untuk
	// videoID dalam window, lalu menandainya. false jika sudah ditandai.
	Claim(ctx context.Context, videoID int64, viewerKey string) (bool, error)
	// Release menghapus tanda dari Claim.
	Release(ctx context.Context, videoID int64, viewerKey string) error
}

// allowedContentTypes adalah tipe file yang boleh diunggah. Nanti semua
// format ini diubah menjadi HLS oleh proses transcoding.
var allowedContentTypes = map[string]bool{
	"video/mp4":        true,
	"video/webm":       true,
	"video/quicktime":  true, // .mov
	"video/x-matroska": true, // .mkv
}

const (
	maxTitleRunes       = 100
	maxDescriptionRunes = 1000
	defaultPageSize     = 20
	maxPageSize         = 50
	maxViewerKeyLen     = 64
	// maxFeedOwners sama dengan batas subscription per user di UserService,
	// dan juga batas jumlah nilai di "IN (...)" pada Oracle (1000).
	maxFeedOwners = 1000
	// viewClaimTimeout membatasi waktu menunggu NATS saat mencatat view.
	// Views tidak penting sampai harus membuat penonton menunggu: kalau NATS
	// lambat atau mati, lebih baik cepat menjawab UNAVAILABLE.
	viewClaimTimeout = time.Second
)

// Options mengatur batas-batas Service.
type Options struct {
	MaxUploadBytes int64
	UploadURLTTL   time.Duration
	PlaybackURLTTL time.Duration
	// MediaBaseURL adalah alamat publik folder bucket, misalnya
	// "http://media.ice-stream.localhost:4000/videos" (tanpa "/" di akhir).
	MediaBaseURL string
}

// Service berisi aturan bisnis VideoService, terpisah dari gRPC, Oracle,
// dan storage supaya mudah diuji.
type Service struct {
	repo   Repository
	store  ObjectStore
	events Publisher
	views  ViewDeduper
	opts   Options
	now    func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository, store ObjectStore, events Publisher, views ViewDeduper, opts Options) *Service {
	return &Service{
		repo:   repo,
		store:  store,
		events: events,
		views:  views,
		opts:   opts,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// CreateUploadInput adalah data dari form upload.
type CreateUploadInput struct {
	OwnerID     int64
	Title       string
	Description string
	ContentType string
	SizeBytes   int64
}

// Upload adalah hasil CreateUpload: video baru dan cara mengunggah filenya.
type Upload struct {
	Video   Video
	Request PresignedRequest
}

// CreateUpload mencatat video baru lalu membuat URL presigned untuk PUT.
//
// File tidak lewat gateway maupun VideoService: browser mengirimnya
// langsung ke storage. Server hanya memberi "izin sementara" berupa URL
// bertanda tangan. Cara ini juga dipakai YouTube, Bilibili, dan aplikasi
// yang memakai S3, karena server tidak perlu menahan file berukuran GB.
func (s *Service) CreateUpload(ctx context.Context, in CreateUploadInput) (Upload, error) {
	nv, err := s.validateCreate(in)
	if err != nil {
		return Upload{}, err
	}
	nv.SearchTerms = BuildSearchTerms(nv.Title, nv.Description)
	nv.ObjectKey, err = newObjectKey()
	if err != nil {
		return Upload{}, err
	}
	v, err := s.repo.Create(ctx, nv)
	if err != nil {
		return Upload{}, err
	}
	req, err := s.store.PresignPut(ctx, v.ObjectKey, v.ContentType, v.SizeBytes, s.opts.UploadURLTTL)
	if err != nil {
		return Upload{}, err
	}
	return Upload{Video: v, Request: req}, nil
}

func (s *Service) validateCreate(in CreateUploadInput) (NewVideo, error) {
	if in.OwnerID <= 0 {
		// Gateway selalu mengisi owner dari access token, jadi ini bug.
		return NewVideo{}, errors.New("owner id kosong")
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return NewVideo{}, err
	}
	desc, err := cleanDescription(in.Description)
	if err != nil {
		return NewVideo{}, err
	}
	contentType := strings.ToLower(strings.TrimSpace(in.ContentType))
	if !allowedContentTypes[contentType] {
		return NewVideo{}, &ValidationError{"content_type", "hanya video/mp4, video/webm, video/quicktime, atau video/x-matroska"}
	}
	if in.SizeBytes <= 0 || in.SizeBytes > s.opts.MaxUploadBytes {
		return NewVideo{}, &ValidationError{"size_bytes", fmt.Sprintf("harus antara 1 byte dan %d MB", s.opts.MaxUploadBytes>>20)}
	}
	return NewVideo{
		OwnerID:     in.OwnerID,
		Title:       title,
		Description: desc,
		ContentType: contentType,
		SizeBytes:   in.SizeBytes,
	}, nil
}

// cleanTitle merapikan judul (spasi di awal/akhir dibuang) lalu
// memeriksanya: wajib diisi, maksimal 100 karakter. Dipakai saat membuat
// dan mengedit video.
func cleanTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if !utf8.ValidString(title) || title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
		return "", &ValidationError{"title", fmt.Sprintf("wajib diisi, maksimal %d karakter", maxTitleRunes)}
	}
	return title, nil
}

// cleanDescription merapikan deskripsi; boleh kosong, maksimal 1000 karakter.
func cleanDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if !utf8.ValidString(desc) || utf8.RuneCountInString(desc) > maxDescriptionRunes {
		return "", &ValidationError{"description", fmt.Sprintf("maksimal %d karakter", maxDescriptionRunes)}
	}
	return desc, nil
}

// newObjectKey membuat nama file acak, misalnya "raw/3f9c.../source".
// Acak (bukan id video) supaya nama file tidak bisa ditebak.
func newObjectKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "raw/" + hex.EncodeToString(b) + "/source", nil
}

// CompleteUpload memeriksa file di storage, menandai video UPLOADED, lalu
// mengirim event videos.uploaded ke transcoder.
//
// Aman dipanggil ulang. Kalau event gagal dikirim (ErrEventsUnavailable),
// status sudah UPLOADED; panggilan berikutnya akan mencoba mengirim event
// lagi. Event yang sama tidak diproses dua kali karena JetStream membuang
// duplikat berdasarkan Msg-Id.
func (s *Service) CompleteUpload(ctx context.Context, ownerID, videoID int64) (Video, error) {
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return Video{}, err
	}
	if v.OwnerID != ownerID {
		return Video{}, ErrNotFound
	}
	switch v.Status {
	case StatusUploaded:
		return s.publishUploaded(ctx, v)
	case StatusProcessing, StatusReady, StatusFailed:
		// Sudah diterima transcoder, tidak ada yang perlu dilakukan.
		return v, nil
	}

	info, err := s.store.Stat(ctx, v.ObjectKey)
	if errors.Is(err, ErrObjectNotFound) {
		return Video{}, ErrUploadIncomplete
	}
	if err != nil {
		return Video{}, err
	}
	// URL presigned sudah mengunci ukuran dan tipe file, tapi tetap dicek
	// di sini: jangan percaya begitu saja pada data yang diunggah user.
	if info.Size != v.SizeBytes || !strings.EqualFold(info.ContentType, v.ContentType) {
		return Video{}, ErrUploadIncomplete
	}

	updated, err := s.repo.MarkUploaded(ctx, v.ID, s.now())
	if errors.Is(err, ErrNotUploading) {
		// Request lain baru saja menyelesaikannya; ambil versi terbaru.
		return s.repo.FindByID(ctx, v.ID)
	}
	if err != nil {
		return Video{}, err
	}
	return s.publishUploaded(ctx, updated)
}

func (s *Service) publishUploaded(ctx context.Context, v Video) (Video, error) {
	if err := s.events.PublishUploaded(ctx, v); err != nil {
		return Video{}, fmt.Errorf("%w: %w", ErrEventsUnavailable, err)
	}
	return v, nil
}

// MarkProcessing dipanggil saat transcoder mulai mengerjakan video.
// Event yang datang terlambat atau dobel diabaikan (bukan error).
func (s *Service) MarkProcessing(ctx context.Context, videoID int64) error {
	if _, err := s.repo.FindByID(ctx, videoID); err != nil {
		return err
	}
	_, err := s.repo.MarkProcessing(ctx, videoID, s.now())
	return err
}

// MarkReady dipanggil setelah transcoder selesai membuat file HLS. Setelah
// status menjadi READY, event videos.published dikirim supaya subscriber
// channel mendapat notifikasi.
//
// Event dikirim juga jika video ternyata SUDAH READY. Itu terjadi kalau
// panggilan sebelumnya berhasil mengubah status tapi gagal mengirim event:
// error-nya membuat event transcode dicoba lagi, dan percobaan kedua ini
// yang mengirim event-nya. Event dobel tidak masalah (lihat Notifier).
func (s *Service) MarkReady(ctx context.Context, videoID int64, r TranscodeResult) error {
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return err
	}
	if r.PlaylistKey == "" || r.DurationMs <= 0 || len(r.Renditions) == 0 {
		return &ValidationError{"transcode_result", "playlist, durasi, dan resolusi wajib diisi"}
	}
	changed, err := s.repo.MarkReady(ctx, videoID, r, s.now())
	if err != nil {
		return err
	}
	if !changed && v.Status != StatusReady {
		// Misalnya sudah FAILED: video ini tidak dipublikasikan.
		return nil
	}
	if err := s.events.PublishPublished(ctx, v); err != nil {
		return fmt.Errorf("%w: %w", ErrEventsUnavailable, err)
	}
	return nil
}

// MarkFailed dipanggil jika transcoder menyerah memproses video.
func (s *Service) MarkFailed(ctx context.Context, videoID int64, reason string) error {
	if _, err := s.repo.FindByID(ctx, videoID); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "transcoding gagal"
	}
	// Kolom failure_reason maksimal 500 karakter.
	if r := []rune(reason); len(r) > 500 {
		reason = string(r[:500])
	}
	_, err := s.repo.MarkFailed(ctx, videoID, reason, s.now())
	return err
}

// VideoDetail adalah video beserta URL file aslinya.
type VideoDetail struct {
	Video Video
	// Original adalah URL presigned untuk mengunduh file asli. Hanya terisi
	// untuk pemilik video, setelah file selesai diunggah.
	Original PresignedRequest
	// ViewerLiked bernilai true jika viewer sudah like video ini.
	ViewerLiked bool
	// ResumePositionMs adalah detik (milidetik) tempat viewer berhenti
	// menonton terakhir kali (lihat ResumePosition). 0 = mulai dari awal.
	ResumePositionMs int64
	// ViewerSaved bernilai true jika viewer sudah menyimpan video ini ke
	// Tonton nanti.
	ViewerSaved bool
	// ViewerInPlaylist bernilai true jika video ini ada di salah satu
	// playlist milik viewer. Selalu false tanpa login.
	ViewerInPlaylist bool
}

// GetVideo mengambil video. Video yang belum READY hanya terlihat oleh
// pemiliknya; untuk orang lain hasilnya ErrNotFound. viewerID 0 berarti
// pengunjung yang belum login.
//
// Video READY diputar lewat file HLS (lihat MediaURL), yang bisa dibuka
// siapa saja. File asli (sebelum di-transcode) hanya untuk pemiliknya.
func (s *Service) GetVideo(ctx context.Context, viewerID, videoID int64) (VideoDetail, error) {
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return VideoDetail{}, err
	}
	isOwner := viewerID != 0 && viewerID == v.OwnerID
	if v.Status != StatusReady && !isOwner {
		return VideoDetail{}, ErrNotFound
	}
	d := VideoDetail{Video: v}
	if viewerID != 0 && v.Status == StatusReady {
		if d.ViewerLiked, err = s.repo.IsLiked(ctx, v.ID, viewerID); err != nil {
			return VideoDetail{}, err
		}
		if d.ViewerSaved, err = s.repo.IsInWatchLater(ctx, viewerID, v.ID); err != nil {
			return VideoDetail{}, err
		}
		if d.ViewerInPlaylist, err = s.repo.IsInAnyPlaylist(ctx, viewerID, v.ID); err != nil {
			return VideoDetail{}, err
		}
		pos, found, err := s.repo.GetProgress(ctx, viewerID, v.ID)
		if err != nil {
			return VideoDetail{}, err
		}
		if found {
			d.ResumePositionMs = ResumePosition(pos, v.DurationMs)
		}
	}
	if !isOwner || v.Status == StatusPendingUpload {
		return d, nil
	}
	if d.Original, err = s.store.PresignGet(ctx, v.ObjectKey, s.opts.PlaybackURLTTL); err != nil {
		return VideoDetail{}, err
	}
	return d, nil
}

// MediaURL mengubah key hasil transcoding (misalnya "hls/42/master.m3u8")
// menjadi URL publik yang bisa dibuka browser. Hanya key di PublicPrefix
// yang bisa dibuka tanpa login; untuk key lain hasilnya string kosong.
func (s *Service) MediaURL(key string) string {
	if !strings.HasPrefix(key, PublicPrefix) {
		return ""
	}
	return s.opts.MediaBaseURL + "/" + key
}

// ListMyVideos mengambil video milik ownerID. pageToken kosong = halaman
// pertama. Token berikutnya kosong jika sudah tidak ada data lagi.
func (s *Service) ListMyVideos(ctx context.Context, ownerID int64, pageSize int, pageToken string) ([]Video, string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	var beforeID int64
	if pageToken != "" {
		id, err := strconv.ParseInt(pageToken, 10, 64)
		if err != nil || id <= 0 {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		beforeID = id
	}
	// Ambil satu lebih banyak untuk tahu apakah masih ada halaman berikutnya.
	videos, err := s.repo.ListByOwner(ctx, ownerID, beforeID, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(videos) > pageSize {
		videos = videos[:pageSize]
		next = strconv.FormatInt(videos[pageSize-1].ID, 10)
	}
	return videos, next, nil
}

// ListVideos mengambil feed publik: video READY, yang paling baru selesai
// diproses lebih dulu. Siapa saja boleh memanggilnya, termasuk pengunjung
// yang belum login. ownerIDs membatasi ke video milik user-user itu: satu
// id untuk halaman channel, banyak id untuk feed subscription. Kosong =
// semua user.
//
// pageToken kosong = halaman pertama. Token berikutnya kosong jika sudah
// tidak ada data lagi. Isi token sengaja tidak perlu dipahami client
// (opaque), supaya cara pagination bisa diubah tanpa mengubah API.
func (s *Service) ListVideos(ctx context.Context, ownerIDs []int64, pageSize int, pageToken string) ([]Video, string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	ownerIDs = uniquePositive(ownerIDs)
	if len(ownerIDs) > maxFeedOwners {
		return nil, "", &ValidationError{"owner_ids", fmt.Sprintf("maksimal %d id", maxFeedOwners)}
	}
	var after *FeedCursor
	if pageToken != "" {
		c, err := decodeFeedCursor(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		after = &c
	}
	videos, err := s.repo.ListPublic(ctx, ownerIDs, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(videos) > pageSize {
		videos = videos[:pageSize]
		last := videos[pageSize-1]
		if last.ProcessedAt == nil {
			// Video READY selalu punya processed_at (lihat MarkReady).
			return nil, "", fmt.Errorf("video %d READY tanpa processed_at", last.ID)
		}
		next = encodeFeedCursor(FeedCursor{ProcessedAt: *last.ProcessedAt, ID: last.ID})
	}
	return videos, next, nil
}

// uniquePositive membuang id <= 0 dan id dobel, urutannya tetap.
func uniquePositive(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// encodeFeedCursor mengubah cursor menjadi teks seperti "MTczMTIy...",
// yaitu base64 dari "<processed_at dalam mikrodetik>.<id>". Mikrodetik
// adalah ketelitian TIMESTAMP bawaan Oracle.
func encodeFeedCursor(c FeedCursor) string {
	raw := strconv.FormatInt(c.ProcessedAt.UnixMicro(), 10) + "." + strconv.FormatInt(c.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeFeedCursor(token string) (FeedCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return FeedCursor{}, err
	}
	micros, id, ok := strings.Cut(string(raw), ".")
	if !ok {
		return FeedCursor{}, errors.New("format cursor salah")
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil || us <= 0 {
		return FeedCursor{}, errors.New("waktu cursor salah")
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return FeedCursor{}, errors.New("id cursor salah")
	}
	return FeedCursor{ProcessedAt: time.UnixMicro(us).UTC(), ID: n}, nil
}

// ViewResult adalah hasil RecordView.
type ViewResult struct {
	// Counted bernilai false jika penonton ini sudah dihitung dalam window.
	Counted bool
	// ViewCount adalah jumlah views setelah request ini.
	ViewCount int64
}

// RecordView menambah views video READY, maksimal sekali per penonton
// dalam satu window (lihat ViewDeduper).
//
// viewerKey dibuat gateway: "u-<id user>" untuk user yang login, atau
// "a-<hash>" untuk pengunjung tanpa login. VideoService tidak perlu tahu
// cara membuatnya, cukup memastikan bentuknya aman dipakai sebagai key.
//
// Urutannya: tandai dulu di NATS, baru tambah di Oracle. Jika Oracle gagal,
// tanda di NATS dihapus lagi supaya view bisa dicoba ulang.
func (s *Service) RecordView(ctx context.Context, videoID int64, viewerKey string) (ViewResult, error) {
	if !validViewerKey(viewerKey) {
		return ViewResult{}, &ValidationError{"viewer_key", "hanya boleh huruf, angka, - dan _, maksimal 64 karakter"}
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return ViewResult{}, err
	}
	if v.Status != StatusReady {
		return ViewResult{}, ErrNotFound
	}
	claimCtx, cancel := context.WithTimeout(ctx, viewClaimTimeout)
	first, err := s.views.Claim(claimCtx, videoID, viewerKey)
	cancel()
	if err != nil {
		return ViewResult{}, fmt.Errorf("%w: %w", ErrViewsUnavailable, err)
	}
	if !first {
		return ViewResult{Counted: false, ViewCount: v.ViewCount}, nil
	}
	count, err := s.repo.IncrementViews(ctx, videoID)
	if err != nil {
		// Pakai context baru: ctx mungkin sudah dibatalkan, tapi tanda di
		// NATS tetap harus dihapus.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if rerr := s.views.Release(releaseCtx, videoID, viewerKey); rerr != nil {
			err = errors.Join(err, fmt.Errorf("menghapus tanda view: %w", rerr))
		}
		return ViewResult{}, err
	}
	return ViewResult{Counted: true, ViewCount: count}, nil
}

// validViewerKey memeriksa viewerKey: 1 sampai 64 karakter, hanya huruf,
// angka, "-", dan "_". Karakter lain (misalnya "." atau "*") punya arti
// khusus di key NATS.
func validViewerKey(k string) bool {
	if k == "" || len(k) > maxViewerKeyLen {
		return false
	}
	for _, r := range k {
		ok := r == '-' || r == '_' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// LikeResult adalah hasil LikeVideo dan UnlikeVideo.
type LikeResult struct {
	Liked     bool
	LikeCount int64
}

// LikeVideo menandai video READY disukai requesterID. Aman dipanggil ulang
// (idempotent): tombol like yang diklik dua kali, atau request yang
// dikirim ulang karena jaringan putus, tidak menambah like_count dua kali.
func (s *Service) LikeVideo(ctx context.Context, requesterID, videoID int64) (LikeResult, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return LikeResult{}, errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return LikeResult{}, ErrNotFound
	}
	count, err := s.repo.Like(ctx, videoID, requesterID)
	if err != nil {
		return LikeResult{}, err
	}
	return LikeResult{Liked: true, LikeCount: count}, nil
}

// UnlikeVideo membatalkan like requesterID. Aman dipanggil ulang.
func (s *Service) UnlikeVideo(ctx context.Context, requesterID, videoID int64) (LikeResult, error) {
	if requesterID <= 0 {
		return LikeResult{}, errors.New("requester id kosong")
	}
	if videoID <= 0 {
		return LikeResult{}, ErrNotFound
	}
	count, err := s.repo.Unlike(ctx, videoID, requesterID)
	if err != nil {
		return LikeResult{}, err
	}
	return LikeResult{Liked: false, LikeCount: count}, nil
}
