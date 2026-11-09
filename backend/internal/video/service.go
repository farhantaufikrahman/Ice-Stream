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
}

// Service berisi aturan bisnis VideoService, terpisah dari gRPC, Oracle,
// dan storage supaya mudah diuji.
type Service struct {
	repo  Repository
	store ObjectStore
	opts  Options
	now   func() time.Time
}

// NewService membuat Service.
func NewService(repo Repository, store ObjectStore, opts Options) *Service {
	return &Service{
		repo:  repo,
		store: store,
		opts:  opts,
		now:   func() time.Time { return time.Now().UTC() },
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

// CompleteUpload memeriksa file di storage, lalu menandai video UPLOADED.
// Aman dipanggil ulang: video yang sudah UPLOADED dikembalikan apa adanya.
func (s *Service) CompleteUpload(ctx context.Context, ownerID, videoID int64) (Video, error) {
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return Video{}, err
	}
	if v.OwnerID != ownerID {
		return Video{}, ErrNotFound
	}
	if v.Status == StatusUploaded {
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
	return updated, err
}

// Playback adalah video beserta URL untuk memutarnya.
type Playback struct {
	Video Video
	// Request kosong selama video belum UPLOADED.
	Request PresignedRequest
}

// GetVideo mengambil video. Video yang belum UPLOADED hanya terlihat oleh
// pemiliknya; untuk orang lain hasilnya ErrNotFound. viewerID 0 berarti
// pengunjung yang belum login.
func (s *Service) GetVideo(ctx context.Context, viewerID, videoID int64) (Playback, error) {
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return Playback{}, err
	}
	if v.Status != StatusUploaded {
		if viewerID == 0 || viewerID != v.OwnerID {
			return Playback{}, ErrNotFound
		}
		return Playback{Video: v}, nil
	}
	req, err := s.store.PresignGet(ctx, v.ObjectKey, s.opts.PlaybackURLTTL)
	if err != nil {
		return Playback{}, err
	}
	return Playback{Video: v, Request: req}, nil
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
