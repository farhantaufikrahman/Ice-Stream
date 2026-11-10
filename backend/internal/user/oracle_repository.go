package user

import (
	"context"
	"errors"
	"fmt"
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
