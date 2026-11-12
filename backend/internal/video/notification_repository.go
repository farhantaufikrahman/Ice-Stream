package video

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// notificationRow memetakan tabel NOTIFICATIONS (migrasi V11).
type notificationRow struct {
	ID        int64      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64      `gorm:"column:user_id"`
	Kind      string     `gorm:"column:kind"`
	VideoID   int64      `gorm:"column:video_id"`
	ActorID   int64      `gorm:"column:actor_id"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	ReadAt    *time.Time `gorm:"column:read_at"`
	// Hanya diisi query ListNotifications (LEFT JOIN video_moderation).
	ModerationReason *string `gorm:"column:moderation_reason;<-:false"`
}

func (notificationRow) TableName() string { return "notifications" }

// insertNotificationsSQL menyimpan banyak notifikasi dalam satu perintah.
// Id penerima dikirim sebagai satu teks JSON, misalnya "[8,9,12]", lalu
// JSON_TABLE mengubahnya menjadi baris-baris:
//
//	user_id
//	-------
//	      8
//	      9
//	     12
//
// NOT EXISTS melewati user yang sudah punya notifikasi untuk video ini,
// jadi event yang terkirim dua kali tidak membuat notifikasi dobel.
// Padanannya di PostgreSQL: INSERT ... SELECT unnest($1::bigint[]) ...
// ON CONFLICT DO NOTHING.
const insertNotificationsSQL = `INSERT INTO notifications (user_id, kind, video_id, actor_id, created_at)
SELECT j.user_id, ?, ?, ?, ?
  FROM JSON_TABLE(?, '$[*]' COLUMNS (user_id NUMBER(19) PATH '$')) j
 WHERE NOT EXISTS (
       SELECT 1 FROM notifications n
        WHERE n.user_id = j.user_id AND n.video_id = ? AND n.kind = ?)`

// InsertNotifications menjalankan insertNotificationsSQL.
//
// Sama seperti MERGE di SaveProgress, NOT EXISTS tidak sepenuhnya aman dari
// balapan: kalau dua proses menyimpan notifikasi video yang sama
// bersamaan, salah satunya bisa gagal dengan ORA-00001 (constraint
// notifications_once_uq). Cukup ulangi sekali: baris yang sudah disimpan
// proses lain sekarang terlihat dan dilewati.
func (r *OracleRepository) InsertNotifications(ctx context.Context, kind string, videoID, actorID int64, userIDs []int64, now time.Time) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	ids, err := json.Marshal(userIDs)
	if err != nil {
		return 0, fmt.Errorf("encode user ids: %w", err)
	}
	args := []any{kind, videoID, actorID, now, string(ids), videoID, kind}
	res := r.db.WithContext(ctx).Exec(insertNotificationsSQL, args...)
	if isUniqueViolation(res.Error) {
		res = r.db.WithContext(ctx).Exec(insertNotificationsSQL, args...)
	}
	if res.Error != nil {
		return 0, fmt.Errorf("insert notifications: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ListNotifications mengambil notifikasi dalam dua query, seperti
// ListHistory. Query pertama:
//
//	SELECT n.id, n.kind, n.video_id, n.created_at, n.read_at
//	  FROM notifications n JOIN videos v ON v.id = n.video_id
//	 WHERE n.user_id = 7 AND v.status = 'READY' AND v.draft = 0
//	   AND v.visibility = 'PUBLIC' AND v.deleted_at IS NULL
//	   AND n.id < :cursor
//	 ORDER BY n.id DESC
//	 FETCH NEXT 21 ROWS ONLY
//
// Oracle membaca index notifications_user_ix (user_id, id) dari belakang
// dan berhenti di baris ke-21. JOIN membuat notifikasi untuk video yang
// sudah dihapus langsung tersaring, jadi jumlah per halaman tetap pas.
//
// Query kedua mengambil data lengkap video-video itu.
func (r *OracleRepository) ListNotifications(ctx context.Context, userID, beforeID int64, limit int) ([]Notification, error) {
	// Alias dengan tanda kutip supaya cocok dengan tag GORM (lihat Search).
	q := r.db.WithContext(ctx).Table("notifications n").
		Select(`n.id AS "id", n.kind AS "kind", n.video_id AS "video_id", n.created_at AS "created_at", n.read_at AS "read_at", `+
			`CASE WHEN n.kind = '`+NotificationVideoBlocked+`' THEN m.reason END AS "moderation_reason"`).
		Joins("JOIN videos v ON v.id = n.video_id").
		Joins("LEFT JOIN video_moderation m ON m.video_id = n.video_id").
		Where("n.user_id = ? AND "+notificationVisibleSQL+" AND v.deleted_at IS NULL", userID)
	if beforeID > 0 {
		q = q.Where("n.id < ?", beforeID)
	}
	var rows []notificationRow
	if err := q.Order("n.id DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("select notifications: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(rows))
	for i, n := range rows {
		ids[i] = n.VideoID
	}
	var videos []videoRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&videos).Error; err != nil {
		return nil, fmt.Errorf("select notification videos: %w", err)
	}
	byID := make(map[int64]videoRow, len(videos))
	for _, v := range videos {
		byID[v.ID] = v
	}
	out := make([]Notification, 0, len(rows))
	for _, n := range rows {
		v, ok := byID[n.VideoID]
		if !ok {
			continue // video dihapus di antara dua query
		}
		var readAt *time.Time
		if n.ReadAt != nil {
			t := n.ReadAt.UTC()
			readAt = &t
		}
		item := Notification{
			ID:        n.ID,
			Kind:      n.Kind,
			Video:     v.toVideo(),
			CreatedAt: n.CreatedAt.UTC(),
			ReadAt:    readAt,
		}
		if n.ModerationReason != nil {
			item.ModerationReason = ReportReason(*n.ModerationReason)
		}
		out = append(out, item)
	}
	return out, nil
}

// notificationVisibleSQL: notifikasi NEW_VIDEO hanya tampil selama
// videonya publik. Notifikasi moderasi (langkah 28) untuk pemilik video
// selalu tampil, walau videonya diturunkan atau pribadi.
const notificationVisibleSQL = "(" + listedSQLv + " OR (n.kind IN ('" + NotificationVideoBlocked + "', '" +
	NotificationVideoRestored + "') AND v.owner_id = n.user_id))"

// countUnreadSQL menghitung yang belum dibaca, tapi berhenti setelah limit
// baris (FETCH FIRST di dalam subquery).
//
//   - SYS_EXTRACT_UTC(n.read_at) IS NULL: ekspresi yang sama dengan isi
//     index notifications_unread_ix (lihat migrasi V11), jadi Oracle hanya
//     membaca notifikasi yang belum dibaca, langsung dari index.
//   - LEADING(n) USE_NL(v) adalah "hint": saran untuk Oracle supaya mulai
//     dari notifikasi user ini lalu mencari videonya satu per satu lewat
//     primary key. Tanpa hint, saat tabel videos masih kecil Oracle kadang
//     memilih membaca SELURUH tabel videos (hash join). Itu murah untuk
//     5.000 video tapi sangat mahal untuk jutaan video.
const countUnreadSQL = `SELECT COUNT(*) FROM (
  SELECT /*+ LEADING(n) USE_NL(v) */ 1
    FROM notifications n JOIN videos v ON v.id = n.video_id
   WHERE n.user_id = ? AND SYS_EXTRACT_UTC(n.read_at) IS NULL
     AND ` + notificationVisibleSQL + ` AND v.deleted_at IS NULL
   FETCH FIRST ? ROWS ONLY)`

// CountUnreadNotifications menjalankan countUnreadSQL.
func (r *OracleRepository) CountUnreadNotifications(ctx context.Context, userID int64, limit int) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(countUnreadSQL, userID, limit).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

// MarkNotificationsRead mengisi read_at. Syarat "belum dibaca" membuat
// waktu baca pertama tidak tertimpa, dan jumlah yang dikembalikan hanya
// notifikasi yang benar-benar baru dibaca. SYS_EXTRACT_UTC dipakai supaya
// "tandai semua" juga bisa memakai index notifications_unread_ix.
func (r *OracleRepository) MarkNotificationsRead(ctx context.Context, userID, notificationID int64, now time.Time) (int64, error) {
	q := r.db.WithContext(ctx).Model(&notificationRow{}).Where("user_id = ? AND SYS_EXTRACT_UTC(read_at) IS NULL", userID)
	if notificationID > 0 {
		q = q.Where("id = ?", notificationID)
	}
	res := q.Update("read_at", now)
	if res.Error != nil {
		return 0, fmt.Errorf("update notifications read_at: %w", res.Error)
	}
	return res.RowsAffected, nil
}
