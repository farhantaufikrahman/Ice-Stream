package video

import (
	"context"
	"fmt"
	"time"
)

// incrementDailyViewsSQL menambah 1 tayangan pada (video, tanggal), atau
// membuat barisnya jika ini tayangan pertama hari itu. Padanannya di
// PostgreSQL: INSERT ... ON CONFLICT (video_id, view_date) DO UPDATE SET
// views = views + 1; di MySQL: INSERT ... ON DUPLICATE KEY UPDATE.
//
// Tanggal dikirim sebagai teks lalu diubah TO_DATE, supaya driver tidak
// menggeser jamnya ke zona waktu sesi database.
const incrementDailyViewsSQL = `MERGE INTO video_daily_views d
USING (SELECT ? AS video_id, TO_DATE(?, 'YYYY-MM-DD') AS view_date FROM dual) s
   ON (d.video_id = s.video_id AND d.view_date = s.view_date)
 WHEN MATCHED THEN
   UPDATE SET d.views = d.views + 1
 WHEN NOT MATCHED THEN
   INSERT (video_id, view_date, views)
   VALUES (s.video_id, s.view_date, 1)`

// channelDailyViewsSQL: lihat komentar di migrasi V14. Video yang sudah
// dihapus (deleted_at terisi) sengaja tetap dihitung, jadi query ini
// memakai Raw, bukan model GORM yang otomatis menyaring deleted_at.
const channelDailyViewsSQL = `SELECT TO_CHAR(d.view_date, 'YYYY-MM-DD') AS "view_day", SUM(d.views) AS "views"
  FROM video_daily_views d
  JOIN videos v ON v.id = d.video_id
 WHERE v.owner_id = ? AND (? = 0 OR v.id = ?)
   AND d.view_date BETWEEN TO_DATE(?, 'YYYY-MM-DD') AND TO_DATE(?, 'YYYY-MM-DD')
 GROUP BY d.view_date`

// ChannelDailyViews menjalankan channelDailyViewsSQL. videoID 0 = semua
// video channel; selain itu hanya video itu (halaman Analitik, langkah 26).
// "(? = 0 OR v.id = ?)" membuat satu teks SQL untuk dua kasus, jadi Oracle
// cukup menyimpan satu rencana eksekusi.
func (r *OracleRepository) ChannelDailyViews(ctx context.Context, ownerID, videoID int64, from, to string) (map[string]int64, error) {
	var rows []struct {
		ViewDay string `gorm:"column:view_day"`
		Views   int64  `gorm:"column:views"`
	}
	if err := r.db.WithContext(ctx).Raw(channelDailyViewsSQL, ownerID, videoID, videoID, from, to).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("select video_daily_views: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.ViewDay] = row.Views
	}
	return out, nil
}

// channelActivitySQL menghitung dua periode sekaligus. Setiap tabel dibaca
// sekali untuk seluruh rentang [previousStart, end), lalu COUNT(CASE ...)
// memilah mana yang masuk periode ini dan mana periode sebelumnya. COUNT
// tidak menghitung NULL, dan CASE tanpa ELSE bernilai NULL.
//
// Hasilnya dua baris: kind 'D' untuk danmaku dan 'C' untuk komentar.
// Parameter setiap bagian: currentStart, currentStart, owner, videoID,
// videoID, previousStart, end.
const channelActivitySQL = `SELECT 'D' AS "kind",
       COUNT(CASE WHEN d.created_at >= ? THEN 1 END) AS "current_count",
       COUNT(CASE WHEN d.created_at < ? THEN 1 END) AS "previous_count"
  FROM danmaku d
 WHERE d.video_id IN (SELECT id FROM videos WHERE owner_id = ? AND (? = 0 OR id = ?))
   AND d.created_at >= ? AND d.created_at < ?
UNION ALL
SELECT 'C' AS kind,
       COUNT(CASE WHEN c.created_at >= ? THEN 1 END),
       COUNT(CASE WHEN c.created_at < ? THEN 1 END)
  FROM comments c
 WHERE c.video_id IN (SELECT id FROM videos WHERE owner_id = ? AND (? = 0 OR id = ?))
   AND c.deleted_at IS NULL
   AND c.created_at >= ? AND c.created_at < ?`

// ChannelActivity menjalankan channelActivitySQL. videoID sama dengan di
// ChannelDailyViews.
func (r *OracleRepository) ChannelActivity(ctx context.Context, ownerID, videoID int64, previousStart, currentStart, end time.Time) (ChannelTotals, ChannelTotals, error) {
	var rows []struct {
		Kind          string `gorm:"column:kind"`
		CurrentCount  int64  `gorm:"column:current_count"`
		PreviousCount int64  `gorm:"column:previous_count"`
	}
	part := []any{currentStart, currentStart, ownerID, videoID, videoID, previousStart, end}
	args := append(append([]any{}, part...), part...)
	if err := r.db.WithContext(ctx).Raw(channelActivitySQL, args...).Scan(&rows).Error; err != nil {
		return ChannelTotals{}, ChannelTotals{}, fmt.Errorf("select channel activity: %w", err)
	}
	var current, previous ChannelTotals
	for _, row := range rows {
		switch row.Kind {
		case "D":
			current.Danmaku, previous.Danmaku = row.CurrentCount, row.PreviousCount
		case "C":
			current.Comments, previous.Comments = row.CurrentCount, row.PreviousCount
		}
	}
	return current, previous, nil
}

// topVideosSQL: video dengan tayangan terbanyak dalam satu rentang tanggal.
// Berbeda dengan grafik, video yang sudah dihapus tidak ikut karena tabel
// ini menautkan ke halaman videonya.
const topVideosSQL = `SELECT v.id AS "id", v.title AS "title", v.thumbnail_key AS "thumbnail_key",
       v.status AS "status", SUM(d.views) AS "views"
  FROM video_daily_views d
  JOIN videos v ON v.id = d.video_id
 WHERE v.owner_id = ? AND v.deleted_at IS NULL
   AND d.view_date BETWEEN TO_DATE(?, 'YYYY-MM-DD') AND TO_DATE(?, 'YYYY-MM-DD')
 GROUP BY v.id, v.title, v.thumbnail_key, v.status
 ORDER BY "views" DESC, v.id DESC
 FETCH FIRST ? ROWS ONLY`

// TopVideos menjalankan topVideosSQL.
func (r *OracleRepository) TopVideos(ctx context.Context, ownerID int64, from, to string, limit int) ([]TopVideo, error) {
	var rows []struct {
		ID           int64   `gorm:"column:id"`
		Title        string  `gorm:"column:title"`
		ThumbnailKey *string `gorm:"column:thumbnail_key"`
		Status       string  `gorm:"column:status"`
		Views        int64   `gorm:"column:views"`
	}
	if err := r.db.WithContext(ctx).Raw(topVideosSQL, ownerID, from, to, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("select top videos: %w", err)
	}
	out := make([]TopVideo, len(rows))
	for i, row := range rows {
		out[i] = TopVideo{VideoID: row.ID, Title: row.Title, Views: row.Views}
		if row.ThumbnailKey != nil && row.Status == StatusReady {
			out[i].ThumbnailKey = *row.ThumbnailKey
		}
	}
	return out, nil
}
