package video

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// playlistRow memetakan tabel PLAYLISTS (migrasi V13).
type playlistRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OwnerID     int64     `gorm:"column:owner_id"`
	Title       string    `gorm:"column:title"`
	Description *string   `gorm:"column:description"`
	Visibility  string    `gorm:"column:visibility"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (playlistRow) TableName() string { return "playlists" }

func (r playlistRow) toPlaylist() Playlist {
	p := Playlist{
		ID:         r.ID,
		OwnerID:    r.OwnerID,
		Title:      r.Title,
		Visibility: r.Visibility,
		CreatedAt:  r.CreatedAt.UTC(),
		UpdatedAt:  r.UpdatedAt.UTC(),
	}
	// Oracle menyimpan string kosong sebagai NULL.
	if r.Description != nil {
		p.Description = *r.Description
	}
	return p
}

// playlistItemRow memetakan tabel PLAYLIST_ITEMS (migrasi V13).
type playlistItemRow struct {
	PlaylistID int64     `gorm:"column:playlist_id;primaryKey"`
	VideoID    int64     `gorm:"column:video_id;primaryKey"`
	AddedAt    time.Time `gorm:"column:added_at"`
}

func (playlistItemRow) TableName() string { return "playlist_items" }

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreatePlaylist menyimpan baris baru. Id diisi Oracle (IDENTITY), dan GORM
// membacanya kembali lewat RETURNING, sama seperti Create video.
func (r *OracleRepository) CreatePlaylist(ctx context.Context, p NewPlaylist) (Playlist, error) {
	row := playlistRow{
		OwnerID:     p.OwnerID,
		Title:       p.Title,
		Description: nullableString(p.Description),
		Visibility:  p.Visibility,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Playlist{}, fmt.Errorf("insert playlist: %w", err)
	}
	// created_at dan updated_at diisi DEFAULT SYSTIMESTAMP di database, jadi
	// baris dibaca ulang supaya nilainya sama persis dengan yang tersimpan.
	return r.FindPlaylist(ctx, row.ID)
}

// CountPlaylists memakai index playlists_owner_ix.
func (r *OracleRepository) CountPlaylists(ctx context.Context, ownerID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&playlistRow{}).Where("owner_id = ?", ownerID).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count playlists: %w", err)
	}
	return n, nil
}

func (r *OracleRepository) FindPlaylist(ctx context.Context, id int64) (Playlist, error) {
	var row playlistRow
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Playlist{}, ErrPlaylistNotFound
	}
	if err != nil {
		return Playlist{}, fmt.Errorf("select playlist: %w", err)
	}
	return row.toPlaylist(), nil
}

func (r *OracleRepository) UpdatePlaylist(ctx context.Context, p Playlist) error {
	res := r.db.WithContext(ctx).Model(&playlistRow{}).Where("id = ?", p.ID).Updates(map[string]any{
		"title":       p.Title,
		"description": nullableString(p.Description),
		"visibility":  p.Visibility,
		"updated_at":  p.UpdatedAt,
	})
	if res.Error != nil {
		return fmt.Errorf("update playlist: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrPlaylistNotFound // dihapus di antara FindPlaylist dan UPDATE
	}
	return nil
}

// DeletePlaylist cukup menghapus baris playlists: isinya ikut terhapus
// lewat ON DELETE CASCADE di playlist_items_playlist_fk.
func (r *OracleRepository) DeletePlaylist(ctx context.Context, id int64) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&playlistRow{}).Error; err != nil {
		return fmt.Errorf("delete playlist: %w", err)
	}
	return nil
}

func (r *OracleRepository) ListPlaylists(ctx context.Context, ownerID int64, includePrivate bool, afterID int64, limit int) ([]Playlist, error) {
	q := r.db.WithContext(ctx).Where("owner_id = ?", ownerID)
	if !includePrivate {
		q = q.Where("visibility = ?", VisibilityPublic)
	}
	if afterID > 0 {
		q = q.Where("id < ?", afterID)
	}
	var rows []playlistRow
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select playlists: %w", err)
	}
	out := make([]Playlist, len(rows))
	for i, row := range rows {
		out[i] = row.toPlaylist()
	}
	return out, nil
}

// playlistStatsSQL menghitung, dalam satu query untuk banyak playlist:
//   - jumlah video READY per playlist (COUNT ... OVER);
//   - thumbnail video READY pertama (ROW_NUMBER ... = 1).
//
// Fungsi analitik (window function) seperti ini juga ada di PostgreSQL dan
// MySQL 8 dengan sintaks yang sama.
const playlistStatsSQL = `SELECT playlist_id AS "playlist_id", video_count AS "video_count", thumbnail_key AS "thumbnail_key"
  FROM (SELECT pi.playlist_id,
               v.thumbnail_key,
               COUNT(*) OVER (PARTITION BY pi.playlist_id) AS video_count,
               ROW_NUMBER() OVER (PARTITION BY pi.playlist_id
                                  ORDER BY SYS_EXTRACT_UTC(pi.added_at), pi.video_id) AS rn
          FROM playlist_items pi
          JOIN videos v ON v.id = pi.video_id
         WHERE pi.playlist_id IN ?
           AND ` + watchableSQLv + `
           AND v.deleted_at IS NULL)
 WHERE rn = 1`

func (r *OracleRepository) PlaylistStats(ctx context.Context, playlistIDs []int64) (map[int64]PlaylistStats, error) {
	if len(playlistIDs) == 0 {
		return map[int64]PlaylistStats{}, nil
	}
	var rows []struct {
		PlaylistID   int64   `gorm:"column:playlist_id"`
		VideoCount   int64   `gorm:"column:video_count"`
		ThumbnailKey *string `gorm:"column:thumbnail_key"`
	}
	if err := r.db.WithContext(ctx).Raw(playlistStatsSQL, playlistIDs).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("select playlist stats: %w", err)
	}
	out := make(map[int64]PlaylistStats, len(rows))
	for _, row := range rows {
		st := PlaylistStats{VideoCount: row.VideoCount}
		if row.ThumbnailKey != nil {
			st.ThumbnailKey = *row.ThumbnailKey
		}
		out[row.PlaylistID] = st
	}
	return out, nil
}

// PlaylistsContaining memakai index playlist_items_video_ix.
func (r *OracleRepository) PlaylistsContaining(ctx context.Context, playlistIDs []int64, videoID int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(playlistIDs) == 0 {
		return out, nil
	}
	var ids []int64
	err := r.db.WithContext(ctx).Model(&playlistItemRow{}).
		Where("video_id = ? AND playlist_id IN ?", videoID, playlistIDs).
		Pluck("playlist_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("select playlist_items: %w", err)
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// IsInAnyPlaylist dimulai dari index playlist_items_video_ix (baris untuk
// video ini biasanya sedikit), lalu memeriksa pemilik lewat primary key
// playlists. FETCH FIRST 1 ROW: cukup tahu ada atau tidak.
func (r *OracleRepository) IsInAnyPlaylist(ctx context.Context, ownerID, videoID int64) (bool, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Table("playlist_items pi").
		Joins("JOIN playlists p ON p.id = pi.playlist_id").
		Where("pi.video_id = ? AND p.owner_id = ?", videoID, ownerID).
		Limit(1).Pluck("pi.playlist_id", &ids).Error
	if err != nil {
		return false, fmt.Errorf("select playlist_items: %w", err)
	}
	return len(ids) > 0, nil
}

func (r *OracleRepository) PlaylistItemAddedAt(ctx context.Context, playlistID, videoID int64) (time.Time, bool, error) {
	var row playlistItemRow
	err := r.db.WithContext(ctx).Select("added_at").
		Where("playlist_id = ? AND video_id = ?", playlistID, videoID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("select playlist_items: %w", err)
	}
	return row.AddedAt.UTC(), true, nil
}

func (r *OracleRepository) CountPlaylistItems(ctx context.Context, playlistID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&playlistItemRow{}).Where("playlist_id = ?", playlistID).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count playlist_items: %w", err)
	}
	return n, nil
}

// insertPlaylistItemSQL sama seperti insertWatchLaterSQL: hanya cabang
// WHEN NOT MATCHED, jadi video yang sudah ada tidak berubah posisi.
const insertPlaylistItemSQL = `MERGE INTO playlist_items i
USING (SELECT ? AS playlist_id, ? AS video_id FROM dual) s
   ON (i.playlist_id = s.playlist_id AND i.video_id = s.video_id)
 WHEN NOT MATCHED THEN
   INSERT (playlist_id, video_id, added_at)
   VALUES (s.playlist_id, s.video_id, ?)`

// AddPlaylistItem menambahkan video dan memperbarui updated_at playlist
// dalam satu transaksi. Seperti AddWatchLater, ORA-00001 dari dua request
// bersamaan berarti "sudah ada" dan tidak dianggap error.
//
// Jika playlist dihapus di tengah jalan, INSERT gagal dengan ORA-02291
// (foreign key) dan dijawab ErrPlaylistNotFound.
func (r *OracleRepository) AddPlaylistItem(ctx context.Context, playlistID, videoID int64, now time.Time) (time.Time, error) {
	var addedAt time.Time
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(insertPlaylistItemSQL, playlistID, videoID, now).Error; err != nil && !isUniqueViolation(err) {
			if isForeignKeyViolation(err) {
				return ErrPlaylistNotFound
			}
			return fmt.Errorf("merge playlist_items: %w", err)
		}
		if err := tx.Model(&playlistRow{}).Where("id = ?", playlistID).Update("updated_at", now).Error; err != nil {
			return fmt.Errorf("update playlist: %w", err)
		}
		var row playlistItemRow
		if err := tx.Select("added_at").Where("playlist_id = ? AND video_id = ?", playlistID, videoID).Take(&row).Error; err != nil {
			return fmt.Errorf("select playlist_items: %w", err)
		}
		addedAt = row.AddedAt.UTC()
		return nil
	})
	return addedAt, err
}

// RemovePlaylistItem hanya memperbarui updated_at jika memang ada baris
// yang dihapus.
func (r *OracleRepository) RemovePlaylistItem(ctx context.Context, playlistID, videoID int64, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("playlist_id = ? AND video_id = ?", playlistID, videoID).Delete(&playlistItemRow{})
		if res.Error != nil {
			return fmt.Errorf("delete playlist_items: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if err := tx.Model(&playlistRow{}).Where("id = ?", playlistID).Update("updated_at", now).Error; err != nil {
			return fmt.Errorf("update playlist: %w", err)
		}
		return nil
	})
}

// ListPlaylistItems memakai dua query, sama seperti ListWatchLater, tapi
// urutannya dari yang paling lama ditambahkan (index
// playlist_items_order_ix, lihat migrasi V13).
func (r *OracleRepository) ListPlaylistItems(ctx context.Context, playlistID int64, after *PlaylistEntryCursor, limit int) ([]PlaylistEntry, error) {
	q := r.db.WithContext(ctx).Table("playlist_items i").
		Select(`i.video_id AS "video_id", i.added_at AS "added_at"`).
		Joins("JOIN videos v ON v.id = i.video_id").
		Where("i.playlist_id = ? AND "+watchableSQLv+" AND v.deleted_at IS NULL", playlistID)
	if after != nil {
		q = q.Where("(SYS_EXTRACT_UTC(i.added_at) > SYS_EXTRACT_UTC(?) OR "+
			"(SYS_EXTRACT_UTC(i.added_at) = SYS_EXTRACT_UTC(?) AND i.video_id > ?))",
			after.AddedAt, after.AddedAt, after.VideoID)
	}
	q = q.Order("SYS_EXTRACT_UTC(i.added_at), i.video_id").Limit(limit)
	var items []playlistItemRow
	if err := q.Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("select playlist_items: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.VideoID
	}
	var rows []videoRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select playlist videos: %w", err)
	}
	byID := make(map[int64]videoRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	entries := make([]PlaylistEntry, 0, len(items))
	for _, it := range items {
		row, ok := byID[it.VideoID]
		if !ok {
			continue // video dihapus di antara dua query
		}
		entries = append(entries, PlaylistEntry{Video: row.toVideo(), AddedAt: it.AddedAt.UTC()})
	}
	return entries, nil
}
