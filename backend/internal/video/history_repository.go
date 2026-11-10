package video

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godror/godror"
	"gorm.io/gorm"
)

// historyRow memetakan tabel WATCH_HISTORY (migrasi V9).
type historyRow struct {
	UserID     int64     `gorm:"column:user_id;primaryKey"`
	VideoID    int64     `gorm:"column:video_id;primaryKey"`
	PositionMs int64     `gorm:"column:position_ms"`
	WatchedAt  time.Time `gorm:"column:watched_at"`
}

func (historyRow) TableName() string { return "watch_history" }

// mergeProgressSQL adalah "upsert" di Oracle: UPDATE jika barisnya sudah
// ada, INSERT jika belum, dalam satu perintah. Padanannya di PostgreSQL
// adalah INSERT ... ON CONFLICT DO UPDATE, di MySQL INSERT ... ON
// DUPLICATE KEY UPDATE.
const mergeProgressSQL = `MERGE INTO watch_history h
USING (SELECT ? AS user_id, ? AS video_id FROM dual) s
   ON (h.user_id = s.user_id AND h.video_id = s.video_id)
 WHEN MATCHED THEN
   UPDATE SET h.position_ms = ?, h.watched_at = ?
 WHEN NOT MATCHED THEN
   INSERT (user_id, video_id, position_ms, watched_at)
   VALUES (s.user_id, s.video_id, ?, ?)`

// SaveProgress menyimpan posisi tonton dengan MERGE.
//
// MERGE tidak sepenuhnya aman dari balapan: jika dua request PERTAMA untuk
// pasangan (user, video) yang sama datang bersamaan, keduanya bisa melihat
// "belum ada" lalu sama-sama INSERT, dan salah satunya gagal dengan
// ORA-00001 (primary key dobel). Kalau itu terjadi, cukup ulangi sekali:
// barisnya sekarang sudah ada, jadi MERGE kedua menjalankan UPDATE.
func (r *OracleRepository) SaveProgress(ctx context.Context, userID, videoID, positionMs int64, now time.Time) error {
	args := []any{userID, videoID, positionMs, now, positionMs, now}
	err := r.db.WithContext(ctx).Exec(mergeProgressSQL, args...).Error
	if isUniqueViolation(err) {
		err = r.db.WithContext(ctx).Exec(mergeProgressSQL, args...).Error
	}
	if err != nil {
		return fmt.Errorf("merge watch_history: %w", err)
	}
	return nil
}

// isUniqueViolation mengembalikan true untuk ORA-00001.
func isUniqueViolation(err error) bool {
	oerr, ok := godror.AsOraErr(err)
	return ok && oerr.Code() == 1
}

// GetProgress membaca satu baris riwayat lewat primary key.
func (r *OracleRepository) GetProgress(ctx context.Context, userID, videoID int64) (int64, bool, error) {
	var row historyRow
	err := r.db.WithContext(ctx).Select("position_ms").
		Where("user_id = ? AND video_id = ?", userID, videoID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("select watch_history: %w", err)
	}
	return row.PositionMs, true, nil
}

// ListHistory mengambil riwayat dalam dua query, seperti Search.
//
// Query pertama mengambil entri riwayat, digabung (JOIN) dengan videos
// supaya video yang tidak READY langsung tersaring dan jumlah per halaman
// tetap pas:
//
//	SELECT h.video_id, h.position_ms, h.watched_at
//	  FROM watch_history h JOIN videos v ON v.id = h.video_id
//	 WHERE h.user_id = 7 AND v.status = 'READY' AND v.deleted_at IS NULL
//	   AND (SYS_EXTRACT_UTC(h.watched_at) < SYS_EXTRACT_UTC(:cursor)
//	        OR (SYS_EXTRACT_UTC(h.watched_at) = SYS_EXTRACT_UTC(:cursor) AND h.video_id < :id))
//	 ORDER BY SYS_EXTRACT_UTC(h.watched_at) DESC, h.video_id DESC
//	 FETCH NEXT 21 ROWS ONLY
//
// SYS_EXTRACT_UTC mengubah waktu menjadi UTC. Index kolom TIMESTAMP WITH
// TIME ZONE disimpan Oracle dalam bentuk itu, jadi dengan ekspresi yang
// sama Oracle bisa membaca index watch_history_user_time_ix dari belakang
// dan berhenti di baris ke-21 (lihat migrasi V9). Tanpa SYS_EXTRACT_UTC,
// Oracle membaca semua riwayat user itu lalu mengurutkannya dulu.
//
// Query kedua mengambil data lengkap video-video itu (videoRow).
func (r *OracleRepository) ListHistory(ctx context.Context, userID int64, after *HistoryCursor, limit int) ([]HistoryEntry, error) {
	// Alias dengan tanda kutip supaya Oracle mengembalikan nama kolom huruf
	// kecil yang cocok dengan tag GORM di historyRow (lihat Search).
	q := r.db.WithContext(ctx).Table("watch_history h").
		Select(`h.video_id AS "video_id", h.position_ms AS "position_ms", h.watched_at AS "watched_at"`).
		Joins("JOIN videos v ON v.id = h.video_id").
		Where("h.user_id = ? AND v.status = ? AND v.deleted_at IS NULL", userID, StatusReady)
	if after != nil {
		q = q.Where("(SYS_EXTRACT_UTC(h.watched_at) < SYS_EXTRACT_UTC(?) OR "+
			"(SYS_EXTRACT_UTC(h.watched_at) = SYS_EXTRACT_UTC(?) AND h.video_id < ?))",
			after.WatchedAt, after.WatchedAt, after.VideoID)
	}
	q = q.Order("SYS_EXTRACT_UTC(h.watched_at) DESC, h.video_id DESC").Limit(limit)
	var hist []historyRow
	if err := q.Scan(&hist).Error; err != nil {
		return nil, fmt.Errorf("select watch_history: %w", err)
	}
	if len(hist) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(hist))
	for i, h := range hist {
		ids[i] = h.VideoID
	}
	var rows []videoRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select history videos: %w", err)
	}
	byID := make(map[int64]videoRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	entries := make([]HistoryEntry, 0, len(hist))
	for _, h := range hist {
		row, ok := byID[h.VideoID]
		if !ok {
			continue // video dihapus di antara dua query
		}
		entries = append(entries, HistoryEntry{
			Video:      row.toVideo(),
			PositionMs: h.PositionMs,
			WatchedAt:  h.WatchedAt.UTC(),
		})
	}
	return entries, nil
}

// DeleteHistory menghapus satu entri (videoID > 0) atau seluruh riwayat
// user (videoID = 0).
func (r *OracleRepository) DeleteHistory(ctx context.Context, userID, videoID int64) (int64, error) {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if videoID > 0 {
		q = q.Where("video_id = ?", videoID)
	}
	res := q.Delete(&historyRow{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete watch_history: %w", res.Error)
	}
	return res.RowsAffected, nil
}
