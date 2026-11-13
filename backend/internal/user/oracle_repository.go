package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// profileRow memetakan kolom profil di tabel USERS (schema AUTH_SVC).
// Kolom password_hash, role, dan lainnya sengaja tidak dipetakan: package
// ini tidak boleh membacanya.
type profileRow struct {
	ID          int64          `gorm:"column:id;primaryKey"`
	Username    string         `gorm:"column:username"`
	Email       string         `gorm:"column:email"`
	DisplayName string         `gorm:"column:display_name"`
	Bio         *string        `gorm:"column:bio"`
	AvatarKey   *string        `gorm:"column:avatar_key"`
	Status      string         `gorm:"column:status"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
	// Migrasi V4. "<-:update" = hanya diubah lewat UPDATE di Subscribe dan
	// Unsubscribe.
	SubscriberCount int64 `gorm:"column:subscriber_count;<-:update"`
}

func (profileRow) TableName() string { return "users" }

func (r profileRow) toProfile() Profile {
	p := Profile{
		ID:          r.ID,
		Username:    r.Username,
		DisplayName: r.DisplayName,
		Email:       r.Email,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt.UTC(),
	}
	p.SubscriberCount = r.SubscriberCount
	if r.Bio != nil {
		p.Bio = *r.Bio
	}
	if r.AvatarKey != nil {
		p.AvatarKey = *r.AvatarKey
	}
	return p
}

// OracleRepository membaca dan mengubah profil di tabel USERS memakai GORM.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// FindByID mencari user yang belum dihapus berdasarkan id.
func (r *OracleRepository) FindByID(ctx context.Context, id int64) (Profile, error) {
	return r.findOne(ctx, "id = ?", id)
}

// FindByUsername mencari user berdasarkan username (huruf kecil).
func (r *OracleRepository) FindByUsername(ctx context.Context, username string) (Profile, error) {
	return r.findOne(ctx, "username = ?", username)
}

func (r *OracleRepository) findOne(ctx context.Context, query string, arg any) (Profile, error) {
	var row profileRow
	err := r.db.WithContext(ctx).Where(query, arg).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("select user: %w", err)
	}
	return row.toProfile(), nil
}

// FindByIDs mengambil banyak user sekaligus dengan satu query
// (WHERE id IN (...)), bukan satu query per id. Id yang tidak ada dilewati.
func (r *OracleRepository) FindByIDs(ctx context.Context, ids []int64) ([]Profile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []profileRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select users: %w", err)
	}
	out := make([]Profile, len(rows))
	for i, row := range rows {
		out[i] = row.toProfile()
	}
	return out, nil
}

// searchUsersWhere menulis WHERE yang sama untuk SearchUsers dan
// CountSearchUsers, lalu mengembalikan nilai untuk tanda ?-nya.
func searchUsersWhere(sql *strings.Builder, terms []string) []any {
	sql.WriteString("\n WHERE deleted_at IS NULL AND status <> ?")
	args := []any{statusBanned}
	for _, t := range terms {
		sql.WriteString("\n   AND (username LIKE ? OR LOWER(display_name) LIKE ?)")
		args = append(args, "%"+t+"%", "%"+t+"%")
	}
	return args
}

// CountSearchUsers menghitung hasil SearchUsers, paling banyak limit
// (langkah 27, angka di tab "Kreator"):
//
//	SELECT COUNT(*) FROM (SELECT 1 FROM users WHERE ... FETCH FIRST ? ROWS ONLY)
//
// FETCH FIRST di dalam membuat Oracle berhenti membaca setelah limit
// baris, jadi kata yang sangat umum tidak menghitung seluruh tabel.
func (r *OracleRepository) CountSearchUsers(ctx context.Context, terms []string, limit int) (int64, error) {
	if len(terms) == 0 {
		return 0, nil
	}
	var sql strings.Builder
	sql.WriteString("SELECT COUNT(*) FROM (SELECT 1 FROM users")
	args := searchUsersWhere(&sql, terms)
	sql.WriteString("\n FETCH FIRST ? ROWS ONLY)")
	args = append(args, limit)
	var n int64
	if err := r.db.WithContext(ctx).Raw(sql.String(), args...).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count search users: %w", err)
	}
	return n, nil
}

// UpdateProfile mengubah field yang tidak nil. String kosong disimpan
// sebagai NULL (Oracle memang menganggap ” = NULL).
func (r *OracleRepository) UpdateProfile(ctx context.Context, id int64, u ProfileUpdate, now time.Time) (Profile, error) {
	updates := map[string]any{"updated_at": now}
	if u.DisplayName != nil {
		updates["display_name"] = *u.DisplayName
	}
	if u.Bio != nil {
		updates["bio"] = nilIfEmpty(*u.Bio)
	}
	res := r.db.WithContext(ctx).Model(&profileRow{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return Profile{}, fmt.Errorf("update profil: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return Profile{}, ErrNotFound
	}
	return r.FindByID(ctx, id)
}

// SetAvatarKey mengganti avatar_key (kosong = hapus) dan mengembalikan key
// lama, supaya file lamanya bisa dihapus dari storage.
//
// SELECT ... FOR UPDATE mengunci baris user ini sampai transaksi selesai.
// Jadi kalau dua upload datang bersamaan, yang kedua menunggu dan membaca
// key milik upload pertama, dan tidak ada file lama yang terlupa dihapus.
func (r *OracleRepository) SetAvatarKey(ctx context.Context, id int64, key string, now time.Time) (string, error) {
	var old string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row profileRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "avatar_key").Where("id = ?", id).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select avatar: %w", err)
		}
		if row.AvatarKey != nil {
			old = *row.AvatarKey
		}
		return tx.Model(&profileRow{}).Where("id = ?", id).
			Updates(map[string]any{"avatar_key": nilIfEmpty(key), "updated_at": now}).Error
	})
	if err != nil {
		return "", err
	}
	return old, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// subscriptionRow memetakan tabel SUBSCRIPTIONS (migrasi V4).
type subscriptionRow struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	SubscriberID int64     `gorm:"column:subscriber_id"`
	ChannelID    int64     `gorm:"column:channel_id"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (subscriptionRow) TableName() string { return "subscriptions" }

// Subscribe mencatat subscription dalam satu transaksi:
//  1. kunci baris channel dengan SELECT ... FOR UPDATE (channel yang
//     diblokir dianggap tidak ada);
//  2. cek apakah sudah subscribe, lalu cek batas maxSubs;
//  3. simpan subscription dan tambah subscriber_count.
//
// Unsubscribe mengunci baris yang sama lebih dulu, jadi subscribe dan
// unsubscribe untuk channel yang sama berjalan bergantian dan tidak bisa
// deadlock. Batas maxSubs sengaja tidak mengunci baris requester: dua
// request bersamaan dari user yang sama paling buruk membuatnya mengikuti
// 1001 channel, dan itu tidak berbahaya.
func (r *OracleRepository) Subscribe(ctx context.Context, subscriberID, channelID int64, maxSubs int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ch, err := lockChannel(tx, channelID, true)
		if err != nil {
			return err
		}
		count = ch.SubscriberCount
		exists, err := isSubscribed(tx, subscriberID, channelID)
		if err != nil || exists {
			return err
		}
		var total int64
		if err := tx.Model(&subscriptionRow{}).Where("subscriber_id = ?", subscriberID).Count(&total).Error; err != nil {
			return fmt.Errorf("hitung subscription: %w", err)
		}
		if total >= int64(maxSubs) {
			return ErrTooManySubscriptions
		}
		if err := tx.Create(&subscriptionRow{SubscriberID: subscriberID, ChannelID: channelID}).Error; err != nil {
			return fmt.Errorf("insert subscription: %w", err)
		}
		if err := addSubscribers(tx, channelID, 1); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Unsubscribe menghapus subscription (jika ada), dengan urutan kunci yang
// sama dengan Subscribe. Channel yang sudah diblokir tetap bisa
// di-unsubscribe.
func (r *OracleRepository) Unsubscribe(ctx context.Context, subscriberID, channelID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ch, err := lockChannel(tx, channelID, false)
		if err != nil {
			return err
		}
		count = ch.SubscriberCount
		res := tx.Where("subscriber_id = ? AND channel_id = ?", subscriberID, channelID).Delete(&subscriptionRow{})
		if res.Error != nil {
			return fmt.Errorf("delete subscription: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil // memang belum subscribe
		}
		if err := addSubscribers(tx, channelID, -1); err != nil {
			return err
		}
		count--
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// lockChannel mengunci baris user channelID sampai transaksi selesai.
func lockChannel(tx *gorm.DB, channelID int64, activeOnly bool) (profileRow, error) {
	q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status", "subscriber_count").Where("id = ?", channelID)
	if activeOnly {
		q = q.Where("status <> ?", statusBanned)
	}
	var row profileRow
	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return profileRow{}, ErrNotFound
	}
	if err != nil {
		return profileRow{}, fmt.Errorf("select channel for update: %w", err)
	}
	return row, nil
}

func addSubscribers(tx *gorm.DB, channelID, delta int64) error {
	err := tx.Model(&profileRow{}).Where("id = ?", channelID).
		UpdateColumn("subscriber_count", gorm.Expr("subscriber_count + ?", delta)).Error
	if err != nil {
		return fmt.Errorf("update subscriber_count: %w", err)
	}
	return nil
}

func isSubscribed(db *gorm.DB, subscriberID, channelID int64) (bool, error) {
	var n int64
	err := db.Model(&subscriptionRow{}).
		Where("subscriber_id = ? AND channel_id = ?", subscriberID, channelID).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("select subscription: %w", err)
	}
	return n > 0, nil
}

// IsSubscribed mengembalikan true jika subscriberID mengikuti channelID.
func (r *OracleRepository) IsSubscribed(ctx context.Context, subscriberID, channelID int64) (bool, error) {
	return isSubscribed(r.db.WithContext(ctx), subscriberID, channelID)
}

// ListSubscriptions mengambil subscription milik subscriberID, id terbesar
// (terbaru) lebih dulu, hanya yang id-nya lebih kecil dari beforeID
// (0 = dari yang terbaru).
func (r *OracleRepository) ListSubscriptions(ctx context.Context, subscriberID, beforeID int64, limit int) ([]Subscription, error) {
	q := r.db.WithContext(ctx).Where("subscriber_id = ?", subscriberID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []subscriptionRow
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select subscriptions: %w", err)
	}
	out := make([]Subscription, len(rows))
	for i, row := range rows {
		out[i] = Subscription{ID: row.ID, ChannelID: row.ChannelID}
	}
	return out, nil
}

// ListSubscribedChannelIDs mengambil maksimal limit id channel yang diikuti
// subscriberID. Hanya kolom channel_id yang dibaca.
func (r *OracleRepository) ListSubscribedChannelIDs(ctx context.Context, subscriberID int64, limit int) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&subscriptionRow{}).
		Where("subscriber_id = ?", subscriberID).
		Order("id DESC").Limit(limit).
		Pluck("channel_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("select channel ids: %w", err)
	}
	return ids, nil
}

// ListSubscriberIDs membaca satu halaman subscriber channelID:
//
//	SELECT subscriber_id FROM subscriptions
//	 WHERE channel_id = ? AND subscriber_id > ?
//	 ORDER BY subscriber_id FETCH NEXT ? ROWS ONLY
//
// Cukup dari index subscriptions_channel_ix (channel_id, subscriber_id)
// tanpa membaca tabelnya (lihat migrasi V5).
func (r *OracleRepository) ListSubscriberIDs(ctx context.Context, channelID, afterID int64, limit int) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&subscriptionRow{}).
		Where("channel_id = ? AND subscriber_id > ?", channelID, afterID).
		Order("subscriber_id").Limit(limit).
		Pluck("subscriber_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("select subscriber ids: %w", err)
	}
	return ids, nil
}

// countNewSubscribersSQL memakai index subscriptions_channel_ix untuk
// mencari baris channel ini, lalu COUNT(CASE ...) memilah dua periode
// dalam satu kali baca (CASE tanpa ELSE bernilai NULL, dan COUNT tidak
// menghitung NULL).
const countNewSubscribersSQL = `SELECT
       COUNT(CASE WHEN created_at >= ? THEN 1 END) AS "current_count",
       COUNT(CASE WHEN created_at < ? THEN 1 END) AS "previous_count"
  FROM subscriptions
 WHERE channel_id = ? AND created_at >= ? AND created_at < ?`

// CountNewSubscribers menjalankan countNewSubscribersSQL.
func (r *OracleRepository) CountNewSubscribers(ctx context.Context, channelID int64, previousStart, currentStart, end time.Time) (int64, int64, error) {
	var row struct {
		CurrentCount  int64 `gorm:"column:current_count"`
		PreviousCount int64 `gorm:"column:previous_count"`
	}
	err := r.db.WithContext(ctx).
		Raw(countNewSubscribersSQL, currentStart, currentStart, channelID, previousStart, end).
		Scan(&row).Error
	if err != nil {
		return 0, 0, fmt.Errorf("count new subscribers: %w", err)
	}
	return row.CurrentCount, row.PreviousCount, nil
}

// SearchUsers menyusun query seperti ini (satu baris AND per kata):
//
//	SELECT ... FROM users
//	 WHERE deleted_at IS NULL AND status <> 'BANNED'
//	   AND (username LIKE '%budi%' OR LOWER(display_name) LIKE '%budi%')
//	 ORDER BY <relevansi kata pertama>, subscriber_count DESC, id
//	OFFSET ? ROWS FETCH NEXT ? ROWS ONLY
//
// Nama kolom diberi alias huruf kecil dalam tanda kutip (AS "bio") supaya
// cocok dengan tag gorm di profileRow; tanpa itu Oracle menjawab nama
// kolom huruf besar.
//
// username selalu huruf kecil (lihat auth.Register), display_name tidak,
// jadi hanya display_name yang perlu LOWER. LIKE '%kata%' tidak bisa
// memakai index, jadi Oracle membaca seluruh tabel users; untuk jumlah
// user saat ini itu cepat. Jika user sudah jutaan, penggantinya adalah
// Oracle Text atau mesin pencari terpisah (Elasticsearch/Meilisearch).
func (r *OracleRepository) SearchUsers(ctx context.Context, terms []string, offset, limit int) ([]Profile, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	var sql strings.Builder
	sql.WriteString(`SELECT id AS "id", username AS "username", email AS "email", display_name AS "display_name", bio AS "bio", avatar_key AS "avatar_key", status AS "status", created_at AS "created_at", updated_at AS "updated_at", subscriber_count AS "subscriber_count"
  FROM users`)
	args := searchUsersWhere(&sql, terms)
	first := terms[0]
	sql.WriteString(`
 ORDER BY CASE WHEN username = ? THEN 0
               WHEN username LIKE ? OR LOWER(display_name) LIKE ? THEN 1
               ELSE 2 END,
          subscriber_count DESC, id
OFFSET ? ROWS FETCH NEXT ? ROWS ONLY`)
	args = append(args, first, first+"%", first+"%", offset, limit)

	var rows []profileRow
	if err := r.db.WithContext(ctx).Raw(sql.String(), args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	out := make([]Profile, len(rows))
	for i, row := range rows {
		out[i] = row.toProfile()
	}
	return out, nil
}
