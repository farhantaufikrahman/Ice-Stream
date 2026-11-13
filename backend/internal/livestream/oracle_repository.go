package livestream

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// Kolom token siaran browser (migrasi V19).
	PublishTokenHash      *string    `gorm:"column:publish_token_hash"`
	PublishTokenExpiresAt *time.Time `gorm:"column:publish_token_expires_at"`
	// Pilihan siaran (migrasi V21), NUMBER(1): 0 atau 1.
	RecordEnabled int       `gorm:"column:record_enabled"`
	SlowMode      int       `gorm:"column:slow_mode"`
	FollowersOnly int       `gorm:"column:followers_only"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (channelRow) TableName() string { return "live_channels" }

func (r channelRow) toChannel() Channel {
	c := Channel{OwnerID: r.OwnerID, Title: r.Title, Category: deref(r.Category), StreamKeyHash: deref(r.StreamKeyHash)}
	if r.StreamKeyCreatedAt != nil {
		c.StreamKeyCreatedAt = r.StreamKeyCreatedAt.UTC()
	}
	c.PublishTokenHash = deref(r.PublishTokenHash)
	if r.PublishTokenExpiresAt != nil {
		c.PublishTokenExpiresAt = r.PublishTokenExpiresAt.UTC()
	}
	c.RecordEnabled, c.SlowMode, c.FollowersOnly = r.RecordEnabled == 1, r.SlowMode == 1, r.FollowersOnly == 1
	return c
}

// chatRow memetakan tabel LIVE_CHAT_MESSAGES (migrasi V19).
type chatRow struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	StreamID  int64     `gorm:"column:stream_id"`
	UserID    int64     `gorm:"column:user_id"`
	Body      string    `gorm:"column:body"`
	Color     int64     `gorm:"column:color"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (chatRow) TableName() string { return "live_chat_messages" }

func (r chatRow) toMessage() ChatMessage {
	return ChatMessage{ID: r.ID, StreamID: r.StreamID, UserID: r.UserID, Body: r.Body, Color: uint32(r.Color), CreatedAt: r.CreatedAt.UTC()}
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
	// Rekaman (migrasi V21).
	RecordingStatus  *string `gorm:"column:recording_status"`
	RecordingVideoID *int64  `gorm:"column:recording_video_id"`
	RecordingError   *string `gorm:"column:recording_error"`
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
	if r.RecordingVideoID != nil {
		s.RecordingVideoID = *r.RecordingVideoID
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

// UpdateChannel mengubah judul, kategori, dan pilihan siaran, termasuk
// judul siaran yang sedang LIVE, dalam satu transaksi.
func (r *OracleRepository) UpdateChannel(ctx context.Context, ownerID int64, u ChannelUpdate, now time.Time) (Channel, error) {
	title, cat := u.Title, nilIfEmpty(u.Category)
	cols := map[string]any{"title": title, "category": cat, "updated_at": now}
	for col, v := range map[string]*bool{"record_enabled": u.RecordEnabled, "slow_mode": u.SlowMode, "followers_only": u.FollowersOnly} {
		if v != nil {
			cols[col] = boolToInt(*v)
		}
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&channelRow{}).Where("owner_id = ?", ownerID).Updates(cols)
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
	if f.Query != "" {
		// % dan _ di teks pencarian dianggap huruf biasa, bukan wildcard.
		q = q.Where(`UPPER(title) LIKE UPPER(?) ESCAPE '\'`, "%"+escapeLike(f.Query)+"%")
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

// endStreamSetSQL menutup siaran (tabel live_streams diberi alias s).
// Jika pilihan rekaman kanal aktif pada saat itu, rekamannya ditandai
// PENDING untuk diimpor RecordingImporter (langkah 27). Subquery mengambil
// record_enabled dari live_channels; NULL (tidak direkam) jika tidak aktif.
const endStreamSetSQL = `s.status = 'ENDED', s.ended_at = s.last_seen_at, s.viewer_count = 0,
	s.recording_status = (SELECT CASE WHEN c.record_enabled = 1 THEN 'PENDING' END
	                        FROM live_channels c WHERE c.owner_id = s.owner_id)`

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
			res = tx.Exec(`UPDATE live_streams s SET `+endStreamSetSQL+`
				WHERE s.owner_id = ? AND s.status = 'LIVE'`, o.OwnerID)
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
		res := tx.Exec(`UPDATE live_streams s SET `+endStreamSetSQL+`
			WHERE s.status = 'LIVE' AND s.last_seen_at < ?`, staleBefore)
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

// SetPublishToken menyimpan hash token siaran browser.
func (r *OracleRepository) SetPublishToken(ctx context.Context, ownerID int64, hash string, expiresAt, now time.Time) error {
	res := r.db.WithContext(ctx).Model(&channelRow{}).Where("owner_id = ?", ownerID).
		Updates(map[string]any{"publish_token_hash": hash, "publish_token_expires_at": expiresAt, "updated_at": now})
	if res.Error != nil {
		return fmt.Errorf("simpan token siaran: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateChat menyimpan satu pesan chat. ID diisi Oracle (identity column)
// lewat RETURNING, yang dibuat GORM secara otomatis.
func (r *OracleRepository) CreateChat(ctx context.Context, m ChatMessage) (ChatMessage, error) {
	row := chatRow{StreamID: m.StreamID, UserID: m.UserID, Body: m.Body, Color: int64(m.Color), CreatedAt: m.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ChatMessage{}, fmt.Errorf("simpan chat: %w", err)
	}
	return row.toMessage(), nil
}

// ListChat mengambil limit pesan terakhir (index live_chat_stream_ix),
// lalu membaliknya supaya urut dari yang paling lama.
func (r *OracleRepository) ListChat(ctx context.Context, streamID int64, limit int) ([]ChatMessage, error) {
	var rows []chatRow
	err := r.db.WithContext(ctx).Where("stream_id = ?", streamID).Order("id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select chat: %w", err)
	}
	out := make([]ChatMessage, len(rows))
	for i, row := range rows {
		out[len(rows)-1-i] = row.toMessage()
	}
	return out, nil
}

// CountChatSince menghitung pesan userID sesudah since (index
// live_chat_user_time_ix).
func (r *OracleRepository) CountChatSince(ctx context.Context, userID int64, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&chatRow{}).Where("user_id = ? AND created_at > ?", userID, since).Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("hitung chat: %w", err)
	}
	return n, nil
}

// LastRecording mengambil rekaman siaran terakhir milik ownerID (index
// live_streams_owner_ix). ErrNotFound jika belum pernah ada.
func (r *OracleRepository) LastRecording(ctx context.Context, ownerID int64) (Recording, error) {
	var row streamRow
	err := r.db.WithContext(ctx).Where("owner_id = ? AND recording_status IS NOT NULL", ownerID).
		Order("started_at DESC, id DESC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Recording{}, ErrNotFound
	}
	if err != nil {
		return Recording{}, fmt.Errorf("select rekaman terakhir: %w", err)
	}
	rec := Recording{StreamID: row.ID, Status: deref(row.RecordingStatus), Error: deref(row.RecordingError)}
	if row.RecordingVideoID != nil {
		rec.VideoID = *row.RecordingVideoID
	}
	if row.EndedAt != nil {
		rec.EndedAt = row.EndedAt.UTC()
	}
	return rec, nil
}

// ClaimRecording mengambil satu rekaman yang menunggu (PENDING), atau yang
// macet di IMPORTING sejak sebelum staleBefore (pod mati di tengah impor),
// lalu menandainya IMPORTING. ErrNotFound jika tidak ada.
//
// Dua langkah: SELECT kandidat, lalu UPDATE dengan syarat status yang sama.
// Jika replika lain lebih dulu mengambilnya, UPDATE tidak mengubah baris
// apa pun dan kandidat berikutnya dicoba. Cara ini mirip
// SELECT ... FOR UPDATE SKIP LOCKED, tetapi tanpa transaksi yang lama.
func (r *OracleRepository) ClaimRecording(ctx context.Context, now, staleBefore time.Time) (Stream, error) {
	var rows []streamRow
	err := r.db.WithContext(ctx).
		Where("status = ? AND (recording_status = ? OR (recording_status = ? AND recording_claimed_at < ?))",
			string(StatusEnded), RecordingPending, RecordingImporting, staleBefore).
		Order("ended_at, id").Limit(5).Find(&rows).Error
	if err != nil {
		return Stream{}, fmt.Errorf("cari rekaman: %w", err)
	}
	for _, row := range rows {
		res := r.db.WithContext(ctx).Model(&streamRow{}).
			Where("id = ? AND recording_status = ?", row.ID, deref(row.RecordingStatus)).
			Updates(map[string]any{"recording_status": RecordingImporting, "recording_claimed_at": now, "recording_error": nil})
		if res.Error != nil {
			return Stream{}, fmt.Errorf("ambil rekaman: %w", res.Error)
		}
		if res.RowsAffected == 1 {
			return row.toStream(), nil
		}
	}
	return Stream{}, ErrNotFound
}

// SetRecordingVideo menyimpan id video hasil rekaman (status tetap
// IMPORTING sampai event ke transcoder terkirim).
func (r *OracleRepository) SetRecordingVideo(ctx context.Context, streamID, videoID int64) error {
	err := r.db.WithContext(ctx).Model(&streamRow{}).Where("id = ?", streamID).
		Update("recording_video_id", videoID).Error
	if err != nil {
		return fmt.Errorf("simpan video rekaman: %w", err)
	}
	return nil
}

// FinishRecording menandai rekaman selesai (DONE).
func (r *OracleRepository) FinishRecording(ctx context.Context, streamID int64) error {
	err := r.db.WithContext(ctx).Model(&streamRow{}).Where("id = ?", streamID).
		Updates(map[string]any{"recording_status": RecordingDone, "recording_error": nil}).Error
	if err != nil {
		return fmt.Errorf("tandai rekaman selesai: %w", err)
	}
	return nil
}

// FailRecording menandai rekaman gagal beserta alasannya.
func (r *OracleRepository) FailRecording(ctx context.Context, streamID int64, reason string) error {
	if runes := []rune(reason); len(runes) > 200 {
		reason = string(runes[:200])
	}
	err := r.db.WithContext(ctx).Model(&streamRow{}).Where("id = ?", streamID).
		Updates(map[string]any{"recording_status": RecordingFailed, "recording_error": reason}).Error
	if err != nil {
		return fmt.Errorf("tandai rekaman gagal: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// escapeLike menandai \, %, dan _ supaya dibaca sebagai huruf biasa di LIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
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
