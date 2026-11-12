package livestream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godror/godror"
	"gorm.io/gorm"
)

// channelRow memetakan tabel LIVE_CHANNELS (migrasi V18).
type channelRow struct {
	OwnerID            int64      `gorm:"column:owner_id;primaryKey"`
	Title              string     `gorm:"column:title"`
	Category           *string    `gorm:"column:category"`
	StreamKeyHash      *string    `gorm:"column:stream_key_hash"`
	StreamKeyCreatedAt *time.Time `gorm:"column:stream_key_created_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (channelRow) TableName() string { return "live_channels" }

func (r channelRow) toChannel() Channel {
	c := Channel{OwnerID: r.OwnerID, Title: r.Title, Category: deref(r.Category), StreamKeyHash: deref(r.StreamKeyHash)}
	if r.StreamKeyCreatedAt != nil {
		c.StreamKeyCreatedAt = r.StreamKeyCreatedAt.UTC()
	}
	return c
}

// streamRow memetakan tabel LIVE_STREAMS (migrasi V18).
type streamRow struct {
	ID          int64      `gorm:"column:id;primaryKey"`
	OwnerID     int64      `gorm:"column:owner_id"`
	Title       string     `gorm:"column:title"`
	Category    *string    `gorm:"column:category"`
	Status      string     `gorm:"column:status"`
	StartedAt   time.Time  `gorm:"column:started_at"`
	LastSeenAt  time.Time  `gorm:"column:last_seen_at"`
	EndedAt     *time.Time `gorm:"column:ended_at"`
	ViewerCount int64      `gorm:"column:viewer_count"`
	PeakViewers int64      `gorm:"column:peak_viewers"`
}

func (streamRow) TableName() string { return "live_streams" }

func (r streamRow) toStream() Stream {
	s := Stream{
		ID: r.ID, OwnerID: r.OwnerID, Title: r.Title, Category: deref(r.Category), Status: Status(r.Status),
		StartedAt: r.StartedAt.UTC(), ViewerCount: r.ViewerCount, PeakViewers: r.PeakViewers,
	}
	if r.EndedAt != nil {
		s.EndedAt = r.EndedAt.UTC()
	}
	return s
}

// OracleRepository menyimpan data LiveService di schema VIDEO_SVC.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// createChannelSQL membuat baris pengaturan jika belum ada. MERGE dengan
// hanya WHEN NOT MATCHED aman dipanggil berkali-kali (idempotent), mirip
// INSERT ... ON CONFLICT DO NOTHING di PostgreSQL.
const createChannelSQL = `
MERGE INTO live_channels c
USING (SELECT ? AS owner_id FROM dual) s
ON (c.owner_id = s.owner_id)
WHEN NOT MATCHED THEN
  INSERT (owner_id, title, created_at, updated_at)
  VALUES (s.owner_id, ?, ?, ?)`

// GetOrCreateChannel mengambil atau membuat pengaturan siaran.
func (r *OracleRepository) GetOrCreateChannel(ctx context.Context, ownerID int64, now time.Time) (Channel, error) {
	err := r.db.WithContext(ctx).Exec(createChannelSQL, ownerID, DefaultTitle, now, now).Error
	if err != nil && !isUniqueViolation(err) {
		// ORA-00001 bisa terjadi jika dua request pertama datang bersamaan;
		// barisnya tetap ada, jadi lanjut membaca.
		return Channel{}, fmt.Errorf("buat live channel: %w", err)
	}
	return r.FindChannel(ctx, ownerID)
}

// FindChannel mengambil pengaturan siaran.
func (r *OracleRepository) FindChannel(ctx context.Context, ownerID int64) (Channel, error) {
	var row channelRow
	err := r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("select live channel: %w", err)
	}
	return row.toChannel(), nil
}

// UpdateChannel mengubah judul dan kategori, termasuk siaran yang sedang
// LIVE, dalam satu transaksi.
func (r *OracleRepository) UpdateChannel(ctx context.Context, ownerID int64, title, category string, now time.Time) (Channel, error) {
	cat := nilIfEmpty(category)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&channelRow{}).Where("owner_id = ?", ownerID).
			Updates(map[string]any{"title": title, "category": cat, "updated_at": now})
		if res.Error != nil {
			return fmt.Errorf("update live channel: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		err := tx.Model(&streamRow{}).Where("owner_id = ? AND status = ?", ownerID, string(StatusLive)).
			Updates(map[string]any{"title": title, "category": cat}).Error
		if err != nil {
			return fmt.Errorf("update siaran yang sedang live: %w", err)
		}
		return nil
	})
	if err != nil {
		return Channel{}, err
	}
	return r.FindChannel(ctx, ownerID)
}

// SetStreamKey menyimpan hash stream key baru.
func (r *OracleRepository) SetStreamKey(ctx context.Context, ownerID int64, hash string, now time.Time) (Channel, error) {
	res := r.db.WithContext(ctx).Model(&channelRow{}).Where("owner_id = ?", ownerID).
		Updates(map[string]any{"stream_key_hash": hash, "stream_key_created_at": now, "updated_at": now})
	if res.Error != nil {
		return Channel{}, fmt.Errorf("simpan stream key: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return Channel{}, ErrNotFound
	}
	return r.FindChannel(ctx, ownerID)
}

// CurrentStream mengambil siaran LIVE milik user. Index unik
// live_streams_one_live_ux menjamin paling banyak satu baris.
func (r *OracleRepository) CurrentStream(ctx context.Context, ownerID int64) (Stream, error) {
	var row streamRow
	err := r.db.WithContext(ctx).Where("owner_id = ? AND status = ?", ownerID, string(StatusLive)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Stream{}, ErrNotFound
	}
	if err != nil {
		return Stream{}, fmt.Errorf("select siaran live: %w", err)
	}
	return row.toStream(), nil
}

// FindStream mengambil satu siaran.
func (r *OracleRepository) FindStream(ctx context.Context, id int64) (Stream, error) {
	var row streamRow
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Stream{}, ErrNotFound
	}
	if err != nil {
		return Stream{}, fmt.Errorf("select siaran: %w", err)
	}
	return row.toStream(), nil
}

// ListLive mengambil siaran LIVE, terbaru dulu. Memakai index
// live_streams_status_ix (status, started_at).
func (r *OracleRepository) ListLive(ctx context.Context, f ListFilter) ([]Stream, error) {
	q := r.db.WithContext(ctx).Where("status = ?", string(StatusLive))
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if len(f.OwnerIDs) > 0 {
		q = q.Where("owner_id IN ?", f.OwnerIDs)
	}
	var rows []streamRow
	if err := q.Order("started_at DESC, id DESC").Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select siaran live: %w", err)
	}
	out := make([]Stream, len(rows))
	for i, row := range rows {
		out[i] = row.toStream()
	}
	return out, nil
}

// insertLiveSQL membuat siaran baru. Judul dan kategori diambil dari
// live_channels saat itu; LEFT JOIN supaya tetap jalan walau barisnya
// tidak ada (seharusnya selalu ada, karena siaran butuh stream key).
const insertLiveSQL = `
INSERT INTO live_streams (owner_id, title, category, status, started_at, last_seen_at, viewer_count, peak_viewers)
SELECT ?, NVL(c.title, ?), c.category, 'LIVE', ?, ?, ?, ?
FROM dual LEFT JOIN live_channels c ON c.owner_id = ?`

// Sync menyamakan tabel live_streams dengan daftar stream dari MediaMTX
// dalam satu transaksi.
func (r *OracleRepository) Sync(ctx context.Context, online []Online, now, staleBefore time.Time) (int, int, error) {
	var started, ended int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, o := range online {
			// Lanjutkan siaran yang masih dalam batas tenggang.
			res := tx.Exec(`UPDATE live_streams
				SET last_seen_at = ?, viewer_count = ?, peak_viewers = GREATEST(peak_viewers, ?)
				WHERE owner_id = ? AND status = 'LIVE' AND last_seen_at >= ?`,
				now, o.Viewers, o.Viewers, o.OwnerID, staleBefore)
			if res.Error != nil {
				return fmt.Errorf("perbarui siaran live: %w", res.Error)
			}
			if res.RowsAffected > 0 {
				continue
			}
			// Siaran lama yang sudah lewat batas tenggang (misalnya service
			// sempat mati) ditutup dulu, baru siaran baru dibuat.
			res = tx.Exec(`UPDATE live_streams SET status = 'ENDED', ended_at = last_seen_at, viewer_count = 0
				WHERE owner_id = ? AND status = 'LIVE'`, o.OwnerID)
			if res.Error != nil {
				return fmt.Errorf("tutup siaran lama: %w", res.Error)
			}
			ended += int(res.RowsAffected)

			startedAt := o.ReadySince
			if startedAt.IsZero() || startedAt.After(now) {
				startedAt = now
			}
			err := tx.Exec(insertLiveSQL, o.OwnerID, DefaultTitle, startedAt, now, o.Viewers, o.Viewers, o.OwnerID).Error
			switch {
			case err == nil:
				started++
			case isUniqueViolation(err):
				// Replika VideoService lain baru saja membuatnya. Oracle hanya
				// membatalkan statement ini, transaksi tetap jalan.
			default:
				return fmt.Errorf("buat siaran live: %w", err)
			}
		}
		res := tx.Exec(`UPDATE live_streams SET status = 'ENDED', ended_at = last_seen_at, viewer_count = 0
			WHERE status = 'LIVE' AND last_seen_at < ?`, staleBefore)
		if res.Error != nil {
			return fmt.Errorf("tutup siaran yang sudah berhenti: %w", res.Error)
		}
		ended += int(res.RowsAffected)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return started, ended, nil
}

func isUniqueViolation(err error) bool {
	oerr, ok := godror.AsOraErr(err)
	return ok && oerr.Code() == 1
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
