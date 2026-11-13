package video

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Langkah 24b: tombol "Laporkan" di halaman tonton.
//
// Laporan hanya disimpan (tabel video_reports, migrasi V17). Belum ada
// halaman moderasi: untuk sekarang laporan dibaca lewat SQL. Video yang
// dilaporkan tidak otomatis disembunyikan, karena itu memberi satu orang
// kuasa menurunkan video orang lain.

// ReportReason adalah alasan laporan. Nilainya sama dengan CHECK
// constraint di tabel video_reports.
type ReportReason string

const (
	ReportSpam       ReportReason = "spam"
	ReportSexual     ReportReason = "sexual"
	ReportViolence   ReportReason = "violence"
	ReportHate       ReportReason = "hate"
	ReportHarassment ReportReason = "harassment"
	ReportMisleading ReportReason = "misleading"
	ReportCopyright  ReportReason = "copyright"
	// ReportOther: keterangan wajib diisi.
	ReportOther ReportReason = "other"
)

// maxReportDetails sama dengan kolom details VARCHAR2(500 CHAR).
const maxReportDetails = 500

func (r ReportReason) valid() bool {
	switch r {
	case ReportSpam, ReportSexual, ReportViolence, ReportHate, ReportHarassment,
		ReportMisleading, ReportCopyright, ReportOther:
		return true
	}
	return false
}

// Report adalah satu laporan yang akan disimpan.
type Report struct {
	VideoID    int64
	ReporterID int64
	Reason     ReportReason
	// Details kosong disimpan sebagai NULL.
	Details    string
	ReportedAt time.Time
}

// ReportVideo menyimpan laporan requesterID atas videoID dan mengembalikan
// waktu laporan disimpan. Video harus READY dan bukan milik requester.
//
// Satu laporan per user per video: melapor lagi mengganti alasan dan
// keterangan, dan laporan yang sudah ditinjau dibuka lagi (OPEN).
func (s *Service) ReportVideo(ctx context.Context, requesterID, videoID int64, reason ReportReason, details string) (time.Time, error) {
	if requesterID <= 0 {
		// Gateway selalu mengisi requester dari access token, jadi ini bug.
		return time.Time{}, errors.New("requester id kosong")
	}
	if !reason.valid() {
		return time.Time{}, &ValidationError{"reason", "tidak dikenal"}
	}
	details = strings.TrimSpace(details)
	if !utf8.ValidString(details) {
		return time.Time{}, &ValidationError{"details", "bukan teks UTF-8 yang valid"}
	}
	if utf8.RuneCountInString(details) > maxReportDetails {
		return time.Time{}, &ValidationError{"details", "maksimal 500 karakter"}
	}
	if reason == ReportOther && details == "" {
		return time.Time{}, &ValidationError{"details", "wajib diisi untuk alasan lainnya"}
	}
	if videoID <= 0 {
		return time.Time{}, ErrNotFound
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return time.Time{}, err
	}
	if !v.IsWatchable() {
		return time.Time{}, ErrNotFound
	}
	if v.OwnerID == requesterID {
		return time.Time{}, &ValidationError{"video_id", "tidak bisa melaporkan video sendiri"}
	}
	// Dibulatkan ke mikrodetik, ketelitian TIMESTAMP bawaan Oracle.
	now := s.now().Truncate(time.Microsecond)
	err = s.repo.SaveReport(ctx, Report{
		VideoID: videoID, ReporterID: requesterID, Reason: reason, Details: details, ReportedAt: now,
	})
	if err != nil {
		return time.Time{}, err
	}
	return now, nil
}
