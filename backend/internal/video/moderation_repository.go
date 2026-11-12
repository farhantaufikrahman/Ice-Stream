package video

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Bagian Oracle untuk moderasi admin (langkah 28, migrasi V22).

// moderationRow memetakan tabel video_moderation.
type moderationRow struct {
	VideoID            int64      `gorm:"column:video_id;primaryKey"`
	PreviousVisibility string     `gorm:"column:previous_visibility"`
	Reason             string     `gorm:"column:reason"`
	Note               *string    `gorm:"column:note"`
	ModeratorID        int64      `gorm:"column:moderator_id"`
	BlockedAt          time.Time  `gorm:"column:blocked_at"`
	RestoredAt         *time.Time `gorm:"column:restored_at"`
	RestoredBy         *int64     `gorm:"column:restored_by"`
}

func (moderationRow) TableName() string { return "video_moderation" }

func (m moderationRow) toModeration() Moderation {
	out := Moderation{
		VideoID:            m.VideoID,
		PreviousVisibility: m.PreviousVisibility,
		Reason:             ReportReason(m.Reason),
		ModeratorID:        m.ModeratorID,
		BlockedAt:          m.BlockedAt.UTC(),
	}
	if m.Note != nil {
		out.Note = *m.Note
	}
	return out
}

// reportedVideoRow adalah satu baris hasil GROUP BY antrean laporan.
type reportedVideoRow struct {
	VideoID     int64     `gorm:"column:video_id"`
	OpenReports int64     `gorm:"column:open_reports"`
	FirstAt     time.Time `gorm:"column:first_at"`
	LastAt      time.Time `gorm:"column:last_at"`
}

// ListReportedVideos:
//
//	SELECT r.video_id, COUNT(*), MIN(r.reported_at), MAX(r.reported_at)
//	  FROM video_reports r JOIN videos v ON v.id = r.video_id
//	 WHERE r.status = 'OPEN' AND v.deleted_at IS NULL
//	 GROUP BY r.video_id
//	 ORDER BY COUNT(*) DESC, MIN(r.reported_at), r.video_id
//	 OFFSET ? ROWS FETCH NEXT ? ROWS ONLY
//
// Index video_reports_queue_ix (status, reported_at) dari V17 membuat
// Oracle hanya membaca laporan OPEN. Lalu dua query kecil: rincian alasan
// dan data videonya.
func (r *OracleRepository) ListReportedVideos(ctx context.Context, offset, limit int) ([]ReportedVideo, error) {
	var rows []reportedVideoRow
	err := r.db.WithContext(ctx).Raw(`SELECT r.video_id AS "video_id", COUNT(*) AS "open_reports", `+
		`MIN(r.reported_at) AS "first_at", MAX(r.reported_at) AS "last_at" `+
		`FROM video_reports r JOIN videos v ON v.id = r.video_id `+
		`WHERE r.status = 'OPEN' AND v.deleted_at IS NULL `+
		`GROUP BY r.video_id ORDER BY COUNT(*) DESC, MIN(r.reported_at), r.video_id `+
		`OFFSET ? ROWS FETCH NEXT ? ROWS ONLY`, offset, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select report queue: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.VideoID
	}

	var reasons []struct {
		VideoID int64  `gorm:"column:video_id"`
		Reason  string `gorm:"column:reason"`
		Count   int64  `gorm:"column:cnt"`
	}
	err = r.db.WithContext(ctx).Raw(`SELECT video_id AS "video_id", reason AS "reason", COUNT(*) AS "cnt" `+
		`FROM video_reports WHERE status = 'OPEN' AND video_id IN ? `+
		`GROUP BY video_id, reason ORDER BY COUNT(*) DESC, reason`, ids).Scan(&reasons).Error
	if err != nil {
		return nil, fmt.Errorf("select report reasons: %w", err)
	}
	byVideo := make(map[int64][]ReasonCount, len(rows))
	for _, rc := range reasons {
		byVideo[rc.VideoID] = append(byVideo[rc.VideoID], ReasonCount{Reason: ReportReason(rc.Reason), Count: rc.Count})
	}

	videos, err := r.findByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ReportedVideo, 0, len(rows))
	for _, row := range rows {
		v, ok := videos[row.VideoID]
		if !ok {
			continue // dihapus di antara dua query
		}
		out = append(out, ReportedVideo{
			Video:           v,
			OpenReports:     row.OpenReports,
			Reasons:         byVideo[row.VideoID],
			FirstReportedAt: row.FirstAt.UTC(),
			LastReportedAt:  row.LastAt.UTC(),
		})
	}
	return out, nil
}

// ListVideoReports memakai primary key video_reports (video_id, reporter_id).
func (r *OracleRepository) ListVideoReports(ctx context.Context, videoID int64, limit int) ([]Report, error) {
	var rows []struct {
		ReporterID int64     `gorm:"column:reporter_id"`
		Reason     string    `gorm:"column:reason"`
		Details    *string   `gorm:"column:details"`
		ReportedAt time.Time `gorm:"column:reported_at"`
	}
	err := r.db.WithContext(ctx).Table("video_reports").
		Select(`reporter_id AS "reporter_id", reason AS "reason", details AS "details", reported_at AS "reported_at"`).
		Where("video_id = ? AND status = 'OPEN'", videoID).
		Order("reported_at DESC, reporter_id").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select video reports: %w", err)
	}
	out := make([]Report, len(rows))
	for i, row := range rows {
		out[i] = Report{
			VideoID: videoID, ReporterID: row.ReporterID, Reason: ReportReason(row.Reason),
			ReportedAt: row.ReportedAt.UTC(),
		}
		if row.Details != nil {
			out[i].Details = *row.Details
		}
	}
	return out, nil
}

// mergeModerationSQL: video yang pernah dipulihkan lalu diturunkan lagi
// memakai baris yang sama.
const mergeModerationSQL = `MERGE INTO video_moderation m
USING (SELECT ? AS video_id FROM dual) s
   ON (m.video_id = s.video_id)
 WHEN MATCHED THEN UPDATE SET
   m.previous_visibility = ?, m.reason = ?, m.note = ?, m.moderator_id = ?,
   m.blocked_at = ?, m.restored_at = NULL, m.restored_by = NULL
 WHEN NOT MATCHED THEN
   INSERT (video_id, previous_visibility, reason, note, moderator_id, blocked_at)
   VALUES (s.video_id, ?, ?, ?, ?, ?)`

// BlockVideo mengunci baris video (SELECT ... FOR UPDATE) supaya dua admin
// yang menekan "Turunkan" bersamaan tidak sama-sama berhasil, dan supaya
// edit pemilik yang berjalan bersamaan tidak menimpa visibilitasnya (lihat
// UpdateDetails).
func (r *OracleRepository) BlockVideo(ctx context.Context, in BlockInput, now time.Time) (BlockedVideo, int64, error) {
	var closed int64
	var prev videoRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", in.VideoID).Take(&prev).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select video for update: %w", err)
		}
		if prev.Visibility == VisibilityBlocked {
			return ErrAlreadyBlocked
		}
		note := nullIfEmpty(in.Note)
		reason := string(in.Reason)
		err = tx.Exec(mergeModerationSQL, in.VideoID,
			prev.Visibility, reason, note, in.ModeratorID, now,
			prev.Visibility, reason, note, in.ModeratorID, now).Error
		if err != nil {
			return fmt.Errorf("merge video_moderation: %w", err)
		}
		err = tx.Model(&videoRow{}).Where("id = ?", in.VideoID).
			Updates(map[string]any{"visibility": VisibilityBlocked, "updated_at": now}).Error
		if err != nil {
			return fmt.Errorf("update video visibility: %w", err)
		}
		if closed, err = resolveReports(tx, in.VideoID, in.ModeratorID, ResolutionBlocked, now); err != nil {
			return err
		}
		return notifyOwner(tx, prev.OwnerID, in.VideoID, in.ModeratorID, NotificationVideoBlocked, now)
	})
	if err != nil {
		return BlockedVideo{}, 0, err
	}
	v, err := r.FindByID(ctx, in.VideoID)
	if err != nil {
		return BlockedVideo{}, 0, err
	}
	m := Moderation{
		VideoID: in.VideoID, PreviousVisibility: prev.Visibility, Reason: in.Reason,
		Note: in.Note, ModeratorID: in.ModeratorID, BlockedAt: now.UTC(),
	}
	v.Moderation = &m
	return BlockedVideo{Video: v, Moderation: m}, closed, nil
}

// ResolveReports menutup laporan OPEN satu video.
func (r *OracleRepository) ResolveReports(ctx context.Context, videoID, moderatorID int64, resolution string, now time.Time) (int64, error) {
	var closed int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Video yang sudah dihapus dijawab ErrNotFound, sama seperti RPC lain.
		var row videoRow
		err := tx.Select("id").Where("id = ?", videoID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select video: %w", err)
		}
		closed, err = resolveReports(tx, videoID, moderatorID, resolution, now)
		return err
	})
	return closed, err
}

func resolveReports(tx *gorm.DB, videoID, moderatorID int64, resolution string, now time.Time) (int64, error) {
	res := tx.Table("video_reports").Where("video_id = ? AND status = 'OPEN'", videoID).
		Updates(map[string]any{
			"status": "RESOLVED", "resolution": resolution, "resolved_at": now, "resolved_by": moderatorID,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("resolve video reports: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// notifyOwner mengganti notifikasi moderasi lama untuk video yang sama
// (constraint notifications_once_uq: satu per user, video, dan jenis),
// supaya notifikasi baru tampil paling atas dan belum dibaca.
func notifyOwner(tx *gorm.DB, ownerID, videoID, actorID int64, kind string, now time.Time) error {
	err := tx.Where("user_id = ? AND video_id = ? AND kind = ?", ownerID, videoID, kind).Delete(&notificationRow{}).Error
	if err != nil {
		return fmt.Errorf("delete old notification: %w", err)
	}
	err = tx.Create(&notificationRow{UserID: ownerID, Kind: kind, VideoID: videoID, ActorID: actorID, CreatedAt: now}).Error
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// UnblockVideo mengunci baris video seperti BlockVideo.
func (r *OracleRepository) UnblockVideo(ctx context.Context, videoID, moderatorID int64, now time.Time) (Video, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row videoRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", videoID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select video for update: %w", err)
		}
		if row.Visibility != VisibilityBlocked {
			return ErrNotBlocked
		}
		var m moderationRow
		err = tx.Where("video_id = ?", videoID).Take(&m).Error
		if err != nil {
			return fmt.Errorf("select video_moderation: %w", err)
		}
		err = tx.Model(&videoRow{}).Where("id = ?", videoID).
			Updates(map[string]any{"visibility": m.PreviousVisibility, "updated_at": now}).Error
		if err != nil {
			return fmt.Errorf("restore video visibility: %w", err)
		}
		err = tx.Model(&moderationRow{}).Where("video_id = ?", videoID).
			Updates(map[string]any{"restored_at": now, "restored_by": moderatorID}).Error
		if err != nil {
			return fmt.Errorf("update video_moderation: %w", err)
		}
		return notifyOwner(tx, row.OwnerID, videoID, moderatorID, NotificationVideoRestored, now)
	})
	if err != nil {
		return Video{}, err
	}
	return r.FindByID(ctx, videoID)
}

// ListBlockedVideos memakai index video_moderation_active_ix
// (restored_at, blocked_at).
func (r *OracleRepository) ListBlockedVideos(ctx context.Context, offset, limit int) ([]BlockedVideo, error) {
	var rows []moderationRow
	err := r.db.WithContext(ctx).Table("video_moderation m").
		Select("m.*").
		Joins("JOIN videos v ON v.id = m.video_id").
		Where("m.restored_at IS NULL AND v.visibility = ? AND v.deleted_at IS NULL", VisibilityBlocked).
		Order("m.blocked_at DESC, m.video_id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select blocked videos: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.VideoID
	}
	videos, err := r.findByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]BlockedVideo, 0, len(rows))
	for _, row := range rows {
		v, ok := videos[row.VideoID]
		if !ok {
			continue
		}
		m := row.toModeration()
		v.Moderation = &m
		out = append(out, BlockedVideo{Video: v, Moderation: m})
	}
	return out, nil
}

// FindModerations membaca catatan moderasi yang masih berlaku.
func (r *OracleRepository) FindModerations(ctx context.Context, ids []int64) (map[int64]Moderation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []moderationRow
	err := r.db.WithContext(ctx).Where("video_id IN ? AND restored_at IS NULL", ids).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select video_moderation: %w", err)
	}
	out := make(map[int64]Moderation, len(rows))
	for _, row := range rows {
		out[row.VideoID] = row.toModeration()
	}
	return out, nil
}

// findByIDs membaca video yang belum dihapus, apa pun visibilitasnya.
func (r *OracleRepository) findByIDs(ctx context.Context, ids []int64) (map[int64]Video, error) {
	var rows []videoRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select videos by id: %w", err)
	}
	out := make(map[int64]Video, len(rows))
	for _, row := range rows {
		out[row.ID] = row.toVideo()
	}
	return out, nil
}
