package video

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// watchLaterRow memetakan tabel WATCH_LATER (migrasi V12).
type watchLaterRow struct {
	UserID  int64     `gorm:"column:user_id;primaryKey"`
	VideoID int64     `gorm:"column:video_id;primaryKey"`
	AddedAt time.Time `gorm:"column:added_at"`
}

func (watchLaterRow) TableName() string { return "watch_later" }

// insertWatchLaterSQL hanya punya cabang WHEN NOT MATCHED: jika baris
// (user, video) sudah ada, MERGE tidak melakukan apa-apa, jadi added_at
// tetap waktu pertama disimpan. Padanannya di PostgreSQL adalah
// INSERT ... ON CONFLICT DO NOTHING, di MySQL INSERT IGNORE.
const insertWatchLaterSQL = `MERGE INTO watch_later w
USING (SELECT ? AS user_id, ? AS video_id FROM dual) s
   ON (w.user_id = s.user_id AND w.video_id = s.video_id)
 WHEN NOT MATCHED THEN
   INSERT (user_id, video_id, added_at)
   VALUES (s.user_id, s.video_id, ?)`

// AddWatchLater menyimpan video lalu membaca added_at yang tersimpan.
//
// Seperti SaveProgress, dua request pertama yang datang bersamaan bisa
// sama-sama mencoba INSERT, dan salah satunya gagal dengan ORA-00001.
// Untuk Tonton nanti itu justru berarti "sudah tersimpan", jadi errornya
// diabaikan dan added_at dibaca dari baris yang menang.
func (r *OracleRepository) AddWatchLater(ctx context.Context, userID, videoID int64, now time.Time) (time.Time, error) {
	err := r.db.WithContext(ctx).Exec(insertWatchLaterSQL, userID, videoID, now).Error
	if err != nil && !isUniqueViolation(err) {
		return time.Time{}, fmt.Errorf("merge watch_later: %w", err)
	}
	var row watchLaterRow
	err = r.db.WithContext(ctx).Select("added_at").
		Where("user_id = ? AND video_id = ?", userID, videoID).Take(&row).Error
	if err != nil {
		return time.Time{}, fmt.Errorf("select watch_later: %w", err)
	}
	return row.AddedAt.UTC(), nil
}

// RemoveWatchLater menghapus satu baris lewat primary key.
func (r *OracleRepository) RemoveWatchLater(ctx context.Context, userID, videoID int64) error {
	err := r.db.WithContext(ctx).Where("user_id = ? AND video_id = ?", userID, videoID).
		Delete(&watchLaterRow{}).Error
	if err != nil {
		return fmt.Errorf("delete watch_later: %w", err)
	}
	return nil
}

// IsInWatchLater membaca satu baris lewat primary key.
func (r *OracleRepository) IsInWatchLater(ctx context.Context, userID, videoID int64) (bool, error) {
	var row watchLaterRow
	err := r.db.WithContext(ctx).Select("video_id").
		Where("user_id = ? AND video_id = ?", userID, videoID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select watch_later: %w", err)
	}
	return true, nil
}

// ListWatchLater memakai dua query, sama seperti ListHistory: pertama
// entri Tonton nanti yang videonya masih READY (urut index
// watch_later_user_time_ix, lihat migrasi V12), lalu data lengkap
// video-video itu.
func (r *OracleRepository) ListWatchLater(ctx context.Context, userID int64, after *WatchLaterCursor, limit int) ([]WatchLaterEntry, error) {
	q := r.db.WithContext(ctx).Table("watch_later w").
		Select(`w.video_id AS "video_id", w.added_at AS "added_at"`).
		Joins("JOIN videos v ON v.id = w.video_id").
		Where("w.user_id = ? AND v.deleted_at IS NULL AND v.status = 'READY' AND (v.owner_id = ? OR (v.draft = 0 AND v.visibility IN ('PUBLIC', 'UNLISTED')))", userID, userID)
	if after != nil {
		q = q.Where("(SYS_EXTRACT_UTC(w.added_at) < SYS_EXTRACT_UTC(?) OR "+
			"(SYS_EXTRACT_UTC(w.added_at) = SYS_EXTRACT_UTC(?) AND w.video_id < ?))",
			after.AddedAt, after.AddedAt, after.VideoID)
	}
	q = q.Order("SYS_EXTRACT_UTC(w.added_at) DESC, w.video_id DESC").Limit(limit)
	var saved []watchLaterRow
	if err := q.Scan(&saved).Error; err != nil {
		return nil, fmt.Errorf("select watch_later: %w", err)
	}
	if len(saved) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(saved))
	for i, w := range saved {
		ids[i] = w.VideoID
	}
	var rows []videoRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select watch_later videos: %w", err)
	}
	byID := make(map[int64]videoRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	entries := make([]WatchLaterEntry, 0, len(saved))
	for _, w := range saved {
		row, ok := byID[w.VideoID]
		if !ok {
			continue // video dihapus di antara dua query
		}
		entries = append(entries, WatchLaterEntry{Video: row.toVideo(), AddedAt: w.AddedAt.UTC()})
	}
	return entries, nil
}
