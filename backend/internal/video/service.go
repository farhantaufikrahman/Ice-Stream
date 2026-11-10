package video

import (
	"context"
	"crypto/rand"
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
	// MarkProcessing mengubah UPLOADED menjadi PROCESSING. Mengembalikan
	// false (tanpa error) jika statusnya sudah bukan UPLOADED.
	MarkProcessing(ctx context.Context, id int64, now time.Time) (bool, error)
	// MarkReady mengubah UPLOADED/PROCESSING menjadi READY dan menyimpan
	// hasil transcoding. false jika statusnya sudah bukan keduanya.
	MarkReady(ctx context.Context, id int64, r TranscodeResult, now time.Time) (bool, error)
	// MarkFailed mengubah UPLOADED/PROCESSING menjadi FAILED. false jika
	// statusnya sudah bukan keduanya.
	MarkFailed(ctx context.Context, id int64, reason string, now time.Time) (bool, error)
}

// Publisher mengirim event ke service lain. Implementasi NATS ada di
// nats.go; test memakai versi palsu.
type Publisher interface {
	// PublishUploaded memberi tahu transcoder bahwa file video siap diproses.
	// Boleh dipanggil lebih dari sekali untuk video yang sama.
	PublishUploaded(ctx context.Context, v Video) error
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
	opts   Options
	now    func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository, store ObjectStore, events Publisher, opts Options) *Service {
	return &Service{
		repo:   repo,
		store:  store,
		events: events,
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
	title := strings.TrimSpace(in.Title)
	if !utf8.ValidString(title) || title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
		return NewVideo{}, &ValidationError{"title", fmt.Sprintf("wajib diisi, maksimal %d karakter", maxTitleRunes)}
	}
	desc := strings.TrimSpace(in.Description)
	if !utf8.ValidString(desc) || utf8.RuneCountInString(desc) > maxDescriptionRunes {
		return NewVideo{}, &ValidationError{"description", fmt.Sprintf("maksimal %d karakter", maxDescriptionRunes)}
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

// MarkReady dipanggil setelah transcoder selesai membuat file HLS.
func (s *Service) MarkReady(ctx context.Context, videoID int64, r TranscodeResult) error {
	if _, err := s.repo.FindByID(ctx, videoID); err != nil {
		return err
	}
	if r.PlaylistKey == "" || r.DurationMs <= 0 || len(r.Renditions) == 0 {
		return &ValidationError{"transcode_result", "playlist, durasi, dan resolusi wajib diisi"}
	}
	_, err := s.repo.MarkReady(ctx, videoID, r, s.now())
	return err
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
	if !isOwner || v.Status == StatusPendingUpload {
		return VideoDetail{Video: v}, nil
	}
	req, err := s.store.PresignGet(ctx, v.ObjectKey, s.opts.PlaybackURLTTL)
	if err != nil {
		return VideoDetail{}, err
	}
	return VideoDetail{Video: v, Original: req}, nil
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
