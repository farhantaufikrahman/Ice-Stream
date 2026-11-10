package video

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// videoRow memetakan tabel VIDEOS (schema VIDEO_SVC).
type videoRow struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	OwnerID     int64      `gorm:"column:owner_id"`
	Title       string     `gorm:"column:title"`
	Description *string    `gorm:"column:description"`
	Status      string     `gorm:"column:status;default:PENDING_UPLOAD"`
	ObjectKey   string     `gorm:"column:object_key"`
	ContentType string     `gorm:"column:content_type"`
	SizeBytes   int64      `gorm:"column:size_bytes"`
	UploadedAt  *time.Time `gorm:"column:uploaded_at"`
	// Kolom hasil transcoding (migrasi V2). NULL sampai status READY/FAILED.
	PlaylistKey   *string    `gorm:"column:playlist_key"`
	ThumbnailKey  *string    `gorm:"column:thumbnail_key"`
	DurationMs    *int64     `gorm:"column:duration_ms"`
	Renditions    *string    `gorm:"column:renditions"`
	FailureReason *string    `gorm:"column:failure_reason"`
	ProcessedAt   *time.Time `gorm:"column:processed_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
	// Belum ada fitur hapus, tapi kolomnya sudah ada di tabel. Dengan field
	// ini GORM otomatis menyaring baris yang sudah dihapus.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (videoRow) TableName() string { return "videos" }

func (r videoRow) toVideo() Video {
	v := Video{
		ID:          r.ID,
		OwnerID:     r.OwnerID,
		Title:       r.Title,
		Status:      r.Status,
		ObjectKey:   r.ObjectKey,
		ContentType: r.ContentType,
		SizeBytes:   r.SizeBytes,
		CreatedAt:   r.CreatedAt.UTC(),
	}
	if r.Description != nil {
		v.Description = *r.Description
	}
	if r.UploadedAt != nil {
		t := r.UploadedAt.UTC()
		v.UploadedAt = &t
	}
	if r.PlaylistKey != nil {
		v.PlaylistKey = *r.PlaylistKey
	}
	if r.ThumbnailKey != nil {
		v.ThumbnailKey = *r.ThumbnailKey
	}
	if r.DurationMs != nil {
		v.DurationMs = *r.DurationMs
	}
	if r.Renditions != nil && *r.Renditions != "" {
		v.Renditions = strings.Split(*r.Renditions, ",")
	}
	if r.FailureReason != nil {
		v.FailureReason = *r.FailureReason
	}
	if r.ProcessedAt != nil {
		t := r.ProcessedAt.UTC()
		v.ProcessedAt = &t
	}
	return v
}

// OracleRepository menyimpan data VideoService di schema VIDEO_SVC memakai GORM.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// Create menyimpan video baru. GORM mengisi ID dan Status dari Oracle.
func (r *OracleRepository) Create(ctx context.Context, v NewVideo) (Video, error) {
	row := videoRow{
		OwnerID:     v.OwnerID,
		Title:       v.Title,
		ObjectKey:   v.ObjectKey,
		ContentType: v.ContentType,
		SizeBytes:   v.SizeBytes,
	}
	// String kosong disimpan sebagai NULL (Oracle memang menganggap '' = NULL).
	if v.Description != "" {
		row.Description = &v.Description
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Video{}, fmt.Errorf("insert video: %w", err)
	}
	return row.toVideo(), nil
}

// FindByID mencari video berdasarkan id.
func (r *OracleRepository) FindByID(ctx context.Context, id int64) (Video, error) {
	var row videoRow
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, fmt.Errorf("select video: %w", err)
	}
	return row.toVideo(), nil
}

// MarkUploaded mengubah status PENDING_UPLOAD menjadi UPLOADED. Syarat
// status ada di WHERE, jadi kalau dua request datang bersamaan, hanya satu
// yang benar-benar mengubah baris (yang lain mendapat ErrNotUploading).
func (r *OracleRepository) MarkUploaded(ctx context.Context, id int64, now time.Time) (Video, error) {
	res := r.db.WithContext(ctx).Model(&videoRow{}).
		Where("id = ? AND status = ?", id, StatusPendingUpload).
		Updates(map[string]any{"status": StatusUploaded, "uploaded_at": now, "updated_at": now})
	if res.Error != nil {
		return Video{}, fmt.Errorf("update video status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return Video{}, ErrNotUploading
	}
	return r.FindByID(ctx, id)
}

// ListByOwner mengambil maksimal limit video milik ownerID dengan id lebih
// kecil dari beforeID (0 = mulai dari yang terbaru). Cara ini disebut
// keyset pagination: lebih cepat dari OFFSET untuk tabel besar, dan tidak
// melompati data saat ada video baru masuk.
func (r *OracleRepository) ListByOwner(ctx context.Context, ownerID, beforeID int64, limit int) ([]Video, error) {
	q := r.db.WithContext(ctx).Where("owner_id = ?", ownerID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []videoRow
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select videos by owner: %w", err)
	}
	videos := make([]Video, len(rows))
	for i, row := range rows {
		videos[i] = row.toVideo()
	}
	return videos, nil
}

// MarkProcessing mengubah UPLOADED menjadi PROCESSING.
func (r *OracleRepository) MarkProcessing(ctx context.Context, id int64, now time.Time) (bool, error) {
	return r.transition(ctx, id, []string{StatusUploaded}, map[string]any{
		"status": StatusProcessing, "updated_at": now,
	})
}

// MarkReady mengubah UPLOADED/PROCESSING menjadi READY. UPLOADED ikut
// diterima karena event "started" bisa saja datang terlambat.
func (r *OracleRepository) MarkReady(ctx context.Context, id int64, res TranscodeResult, now time.Time) (bool, error) {
	return r.transition(ctx, id, []string{StatusUploaded, StatusProcessing}, map[string]any{
		"status":         StatusReady,
		"playlist_key":   res.PlaylistKey,
		"thumbnail_key":  res.ThumbnailKey,
		"duration_ms":    res.DurationMs,
		"renditions":     strings.Join(res.Renditions, ","),
		"failure_reason": nil,
		"processed_at":   now,
		"updated_at":     now,
	})
}

// MarkFailed mengubah UPLOADED/PROCESSING menjadi FAILED.
func (r *OracleRepository) MarkFailed(ctx context.Context, id int64, reason string, now time.Time) (bool, error) {
	return r.transition(ctx, id, []string{StatusUploaded, StatusProcessing}, map[string]any{
		"status": StatusFailed, "failure_reason": reason, "processed_at": now, "updated_at": now,
	})
}

// transition mengubah baris hanya jika statusnya sekarang ada di from.
// Syarat ini ada di WHERE, jadi tetap benar walau event datang dobel atau
// tidak berurutan: video READY tidak akan kembali menjadi PROCESSING.
func (r *OracleRepository) transition(ctx context.Context, id int64, from []string, updates map[string]any) (bool, error) {
	res := r.db.WithContext(ctx).Model(&videoRow{}).
		Where("id = ? AND status IN ?", id, from).
		Updates(updates)
	if res.Error != nil {
		return false, fmt.Errorf("update video %d ke %v: %w", id, updates["status"], res.Error)
	}
	return res.RowsAffected > 0, nil
}
