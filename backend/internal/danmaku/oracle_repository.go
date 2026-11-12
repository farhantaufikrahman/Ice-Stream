package danmaku

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// statusReady sama dengan video.StatusReady. Ditulis ulang di sini supaya
// package danmaku tidak bergantung pada package video.
const statusReady = "READY"

// danmakuRow memetakan tabel DANMAKU (schema VIDEO_SVC).
type danmakuRow struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	VideoID    int64  `gorm:"column:video_id"`
	UserID     int64  `gorm:"column:user_id"`
	PositionMs int64  `gorm:"column:position_ms"`
	Body       string `gorm:"column:body"`
	Color      uint32 `gorm:"column:color"`
	// Kolomnya display_mode, karena MODE adalah reserved word di Oracle 23ai.
	Mode string `gorm:"column:display_mode"`
	// Diisi GORM dengan jam aplikasi saat INSERT (field bernama CreatedAt).
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (danmakuRow) TableName() string { return "danmaku" }

func (r danmakuRow) toDanmaku() Danmaku {
	return Danmaku{
		ID:         r.ID,
		VideoID:    r.VideoID,
		UserID:     r.UserID,
		PositionMs: r.PositionMs,
		Body:       r.Body,
		Color:      r.Color,
		Mode:       Mode(r.Mode),
		CreatedAt:  r.CreatedAt.UTC(),
	}
}

// videoRef hanya memetakan kolom tabel VIDEOS yang dibutuhkan danmaku.
// Package ini hanya membaca tabel videos, tidak pernah mengubahnya.
type videoRef struct {
	ID         int64          `gorm:"column:id;primaryKey;->"`
	DurationMs *int64         `gorm:"column:duration_ms;->"`
	DeletedAt  gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (videoRef) TableName() string { return "videos" }

// OracleRepository menyimpan danmaku di schema VIDEO_SVC memakai GORM.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// VideoDuration mengambil durasi video READY.
func (r *OracleRepository) VideoDuration(ctx context.Context, videoID int64) (int64, error) {
	var v videoRef
	err := r.db.WithContext(ctx).Select("id", "duration_ms").
		Where("id = ? AND status = ?", videoID, statusReady).
		Take(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrVideoNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("select video: %w", err)
	}
	if v.DurationMs == nil {
		return 0, nil
	}
	return *v.DurationMs, nil
}

// CountSince memakai index danmaku_user_time_ix (user_id, created_at).
func (r *OracleRepository) CountSince(ctx context.Context, userID int64, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&danmakuRow{}).
		Where("user_id = ? AND created_at > ?", userID, since).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("count danmaku: %w", err)
	}
	return n, nil
}

// Create menyimpan danmaku dan menambah videos.danmaku_count (migrasi V16)
// dalam satu transaksi, sama seperti komentar dan comment_count. Jumlah ini
// dipakai urutan "Danmaku terbanyak" di pencarian dan angka danmaku di
// halaman tonton. FOREIGN KEY memastikan videonya ada.
func (r *OracleRepository) Create(ctx context.Context, d Danmaku) (Danmaku, error) {
	row := danmakuRow{
		VideoID:    d.VideoID,
		UserID:     d.UserID,
		PositionMs: d.PositionMs,
		Body:       d.Body,
		Color:      d.Color,
		Mode:       string(d.Mode),
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert danmaku: %w", err)
		}
		// "danmaku_count + 1" dihitung Oracle sendiri, jadi aman walau
		// banyak danmaku masuk bersamaan.
		err := tx.Table("videos").Where("id = ?", d.VideoID).
			UpdateColumn("danmaku_count", gorm.Expr("danmaku_count + 1")).Error
		if err != nil {
			return fmt.Errorf("update danmaku_count: %w", err)
		}
		return nil
	})
	if err != nil {
		return Danmaku{}, err
	}
	return row.toDanmaku(), nil
}

// List memakai index danmaku_video_pos_ix (video_id, position_ms, id).
// Keyset pagination dengan dua kolom: lanjut dari danmaku sesudah
// (after.PositionMs, after.ID).
func (r *OracleRepository) List(ctx context.Context, videoID, fromMs, toMs int64, after Cursor, limit int) ([]Danmaku, error) {
	q := r.db.WithContext(ctx).
		Where("video_id = ? AND position_ms BETWEEN ? AND ?", videoID, fromMs, toMs)
	if !after.IsZero() {
		q = q.Where("(position_ms > ? OR (position_ms = ? AND id > ?))", after.PositionMs, after.PositionMs, after.ID)
	}
	var rows []danmakuRow
	if err := q.Order("position_ms, id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select danmaku: %w", err)
	}
	out := make([]Danmaku, len(rows))
	for i, row := range rows {
		out[i] = row.toDanmaku()
	}
	return out, nil
}
