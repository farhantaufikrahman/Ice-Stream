package video

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Langkah 28: halaman moderasi admin (migrasi V22).
//
// Admin meninjau video yang dilaporkan penonton (langkah 24b), lalu:
//   - BlockVideo ("Turunkan"): visibilitas video menjadi BLOCKED, jadi
//     video hilang dari semua tempat kecuali Studio pemiliknya. Semua
//     laporan OPEN video itu ditutup, dan pemilik diberi notifikasi.
//   - DismissReports ("Tolak laporan"): laporan ditutup, video tetap.
//   - UnblockVideo ("Pulihkan"): visibilitas kembali seperti sebelum
//     diturunkan.
//
// Service ini TIDAK memeriksa role. Hanya gateway yang memanggilnya, dan
// gateway hanya meneruskan request dari user dengan role ADMIN (role ada
// di schema AUTH_SVC, lihat httpserver/moderation.go). Ini sama dengan
// cara requester_id dipercaya di semua RPC lain.

// VisibilityBlocked: video diturunkan admin. Tidak pernah diterima dari
// pemilik video (cleanVideoVisibility menolaknya).
const VisibilityBlocked = "BLOCKED"

// Hasil peninjauan laporan (kolom video_reports.resolution).
const (
	ResolutionBlocked   = "BLOCKED"
	ResolutionDismissed = "DISMISSED"
)

// Jenis notifikasi untuk pemilik video (lihat notification.go).
const (
	NotificationVideoBlocked  = "VIDEO_BLOCKED"
	NotificationVideoRestored = "VIDEO_RESTORED"
)

var (
	// ErrAlreadyBlocked: BlockVideo untuk video yang sudah diturunkan.
	ErrAlreadyBlocked = errors.New("video sudah diturunkan")
	// ErrNotBlocked: UnblockVideo untuk video yang tidak sedang diturunkan.
	ErrNotBlocked = errors.New("video tidak sedang diturunkan")
)

// maxModerationNote sama dengan kolom video_moderation.note.
const maxModerationNote = 500

// maxVideoReports: batas laporan yang ditampilkan untuk satu video.
const maxVideoReports = 100

// Moderation adalah catatan penurunan satu video.
type Moderation struct {
	VideoID            int64
	PreviousVisibility string
	Reason             ReportReason
	Note               string
	ModeratorID        int64
	BlockedAt          time.Time
}

// BlockedVideo adalah video yang sedang diturunkan beserta catatannya.
type BlockedVideo struct {
	Video      Video
	Moderation Moderation
}

// ReasonCount adalah jumlah laporan OPEN untuk satu alasan.
type ReasonCount struct {
	Reason ReportReason
	Count  int64
}

// ReportedVideo adalah satu baris antrean laporan.
type ReportedVideo struct {
	Video           Video
	OpenReports     int64
	Reasons         []ReasonCount
	FirstReportedAt time.Time
	LastReportedAt  time.Time
}

// BlockInput adalah permintaan BlockVideo.
type BlockInput struct {
	ModeratorID int64
	VideoID     int64
	Reason      ReportReason
	Note        string
}

// ListReportedVideos membaca antrean laporan. page_token adalah posisi
// (offset) baris berikutnya. Antrean admin kecil dan urutannya berubah
// setiap kali laporan ditutup, jadi offset cukup; tidak perlu keyset.
func (s *Service) ListReportedVideos(ctx context.Context, pageSize int, pageToken string) ([]ReportedVideo, string, error) {
	limit, offset, err := offsetPage(pageSize, pageToken)
	if err != nil {
		return nil, "", err
	}
	items, err := s.repo.ListReportedVideos(ctx, offset, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = strconv.Itoa(offset + limit)
	}
	return items, next, nil
}

// ListVideoReports membaca laporan OPEN satu video, terbaru dulu.
func (s *Service) ListVideoReports(ctx context.Context, videoID int64) ([]Report, error) {
	if videoID <= 0 {
		return nil, ErrNotFound
	}
	return s.repo.ListVideoReports(ctx, videoID, maxVideoReports)
}

// BlockVideo menurunkan video. Alasan wajib; catatan wajib untuk "other".
func (s *Service) BlockVideo(ctx context.Context, in BlockInput) (BlockedVideo, int64, error) {
	if in.ModeratorID <= 0 {
		return BlockedVideo{}, 0, errors.New("moderator id kosong")
	}
	if in.VideoID <= 0 {
		return BlockedVideo{}, 0, ErrNotFound
	}
	if !in.Reason.valid() {
		return BlockedVideo{}, 0, &ValidationError{"reason", "tidak dikenal"}
	}
	note := strings.TrimSpace(in.Note)
	if !utf8.ValidString(note) {
		return BlockedVideo{}, 0, &ValidationError{"note", "bukan teks UTF-8 yang valid"}
	}
	if utf8.RuneCountInString(note) > maxModerationNote {
		return BlockedVideo{}, 0, &ValidationError{"note", fmt.Sprintf("maksimal %d karakter", maxModerationNote)}
	}
	if in.Reason == ReportOther && note == "" {
		return BlockedVideo{}, 0, &ValidationError{"note", "wajib diisi untuk alasan lainnya"}
	}
	in.Note = note
	return s.repo.BlockVideo(ctx, in, s.now().Truncate(time.Microsecond))
}

// DismissReports menutup laporan OPEN satu video tanpa mengubah videonya.
func (s *Service) DismissReports(ctx context.Context, moderatorID, videoID int64) (int64, error) {
	if moderatorID <= 0 {
		return 0, errors.New("moderator id kosong")
	}
	if videoID <= 0 {
		return 0, ErrNotFound
	}
	return s.repo.ResolveReports(ctx, videoID, moderatorID, ResolutionDismissed, s.now().Truncate(time.Microsecond))
}

// UnblockVideo memulihkan video ke visibilitas sebelum diturunkan.
func (s *Service) UnblockVideo(ctx context.Context, moderatorID, videoID int64) (Video, error) {
	if moderatorID <= 0 {
		return Video{}, errors.New("moderator id kosong")
	}
	if videoID <= 0 {
		return Video{}, ErrNotFound
	}
	return s.repo.UnblockVideo(ctx, videoID, moderatorID, s.now().Truncate(time.Microsecond))
}

// ListBlockedVideos membaca video yang sedang diturunkan, terbaru dulu.
func (s *Service) ListBlockedVideos(ctx context.Context, pageSize int, pageToken string) ([]BlockedVideo, string, error) {
	limit, offset, err := offsetPage(pageSize, pageToken)
	if err != nil {
		return nil, "", err
	}
	items, err := s.repo.ListBlockedVideos(ctx, offset, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = strconv.Itoa(offset + limit)
	}
	return items, next, nil
}

// attachModeration mengisi Video.Moderation untuk video BLOCKED, supaya
// pemiliknya tahu alasannya di Studio.
func (s *Service) attachModeration(ctx context.Context, videos []Video) error {
	var ids []int64
	for _, v := range videos {
		if v.Visibility == VisibilityBlocked {
			ids = append(ids, v.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	found, err := s.repo.FindModerations(ctx, ids)
	if err != nil {
		return err
	}
	for i := range videos {
		if m, ok := found[videos[i].ID]; ok {
			videos[i].Moderation = &m
		}
	}
	return nil
}

// offsetPage memeriksa page_size dan page_token (offset).
func offsetPage(pageSize int, pageToken string) (limit, offset int, err error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return 0, 0, &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	if pageToken != "" {
		n, err := strconv.Atoi(pageToken)
		if err != nil || n <= 0 {
			return 0, 0, &ValidationError{"page_token", "tidak valid"}
		}
		offset = n
	}
	return pageSize, offset, nil
}
