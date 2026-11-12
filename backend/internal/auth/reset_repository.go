package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// resetTokenRow memetakan tabel PASSWORD_RESET_TOKENS (migrasi V6).
type resetTokenRow struct {
	ID        int64      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64      `gorm:"column:user_id"`
	TokenHash string     `gorm:"column:token_hash"`
	RequestIP *string    `gorm:"column:request_ip"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (resetTokenRow) TableName() string { return "password_reset_tokens" }

var _ ResetRepository = (*OracleRepository)(nil)

// RecentResetTokens menghitung token milik user sejak waktu tertentu.
// Query ini memakai index password_reset_tokens_user_ix (user_id, created_at).
func (r *OracleRepository) RecentResetTokens(ctx context.Context, userID int64, since time.Time) (int, time.Time, error) {
	var out struct {
		Count  int
		Latest sql.NullTime
	}
	err := r.db.WithContext(ctx).Model(&resetTokenRow{}).
		Select("COUNT(*) AS count, MAX(created_at) AS latest").
		Where("user_id = ? AND created_at >= ?", userID, since).
		Scan(&out).Error
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("hitung token reset: %w", err)
	}
	var latest time.Time
	if out.Latest.Valid {
		latest = out.Latest.Time.UTC()
	}
	return out.Count, latest, nil
}

// CreateResetToken menyimpan token baru dan mematikan token lama, supaya
// hanya tautan terbaru di kotak masuk user yang bisa dipakai.
func (r *OracleRepository) CreateResetToken(ctx context.Context, t PasswordResetToken) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&resetTokenRow{}).
			Where("user_id = ? AND used_at IS NULL", t.UserID).
			Update("used_at", t.CreatedAt).Error
		if err != nil {
			return fmt.Errorf("matikan token reset lama: %w", err)
		}
		row := resetTokenRow{
			UserID:    t.UserID,
			TokenHash: t.TokenHash,
			RequestIP: nilIfEmpty(t.RequestIP),
			ExpiresAt: t.ExpiresAt,
			CreatedAt: t.CreatedAt,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("simpan token reset: %w", err)
		}
		return nil
	})
}

// ResetPassword mengganti password dalam satu transaksi. SELECT ... FOR
// UPDATE mengunci baris token, jadi jika tautan yang sama dikirim dua kali
// bersamaan, hanya satu yang berhasil (sama seperti RotateRefreshToken).
func (r *OracleRepository) ResetPassword(ctx context.Context, tokenHash, passwordHash string, now time.Time) (int64, error) {
	var userID int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tok resetTokenRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", tokenHash).
			Take(&tok).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidResetToken
		}
		if err != nil {
			return fmt.Errorf("select token reset: %w", err)
		}
		if tok.UsedAt != nil || !now.Before(tok.ExpiresAt) {
			return ErrInvalidResetToken
		}

		// GORM menambahkan "deleted_at IS NULL", jadi akun yang sudah
		// dihapus tidak ikut ter-update (RowsAffected = 0).
		res := tx.Model(&userRow{}).
			Where("id = ? AND status = ?", tok.UserID, StatusActive).
			Updates(map[string]any{
				"password_hash": passwordHash,
				"updated_at":    now,
				// Tautan reset hanya sampai ke pemilik email, jadi emailnya
				// sekarang terbukti (dipakai login Google/GitHub, langkah 26).
				"email_verified_at": gorm.Expr("COALESCE(email_verified_at, ?)", now),
			})
		if res.Error != nil {
			return fmt.Errorf("update password: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrInvalidResetToken
		}

		if err := tx.Model(&tok).Update("used_at", now).Error; err != nil {
			return fmt.Errorf("tandai token reset terpakai: %w", err)
		}
		// Password lama mungkin diketahui orang lain, jadi semua sesi
		// yang sedang login dicabut.
		err = tx.Model(&refreshTokenRow{}).
			Where("user_id = ? AND revoked_at IS NULL", tok.UserID).
			Update("revoked_at", now).Error
		if err != nil {
			return fmt.Errorf("cabut refresh token: %w", err)
		}
		userID = tok.UserID
		return nil
	})
	return userID, err
}
