package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godror/godror"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// userRow memetakan tabel USERS. Struct tag gorm:"..." sama fungsinya dengan
// @Column di JPA atau definisi model di schema Prisma. Struktur tabelnya
// sendiri dibuat oleh migrasi Flyway, bukan oleh GORM AutoMigrate.
type userRow struct {
	ID           int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string `gorm:"column:username"`
	Email        string `gorm:"column:email"`
	PasswordHash string `gorm:"column:password_hash"`
	DisplayName  string `gorm:"column:display_name"`
	// default:... memberi tahu GORM bahwa nilainya diisi Oracle, jadi GORM
	// mengambilnya kembali dengan RETURNING setelah INSERT.
	Role      string         `gorm:"column:role;default:USER"`
	Status    string         `gorm:"column:status;default:ACTIVE"`
	CreatedAt time.Time      `gorm:"column:created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (userRow) TableName() string { return "users" }

func (r userRow) toUser() User {
	return User{
		ID:          r.ID,
		Username:    r.Username,
		Email:       r.Email,
		DisplayName: r.DisplayName,
		Role:        r.Role,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt.UTC(),
	}
}

// refreshTokenRow memetakan tabel REFRESH_TOKENS.
type refreshTokenRow struct {
	ID        int64      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64      `gorm:"column:user_id"`
	TokenHash string     `gorm:"column:token_hash"`
	UserAgent *string    `gorm:"column:user_agent"`
	IPAddress *string    `gorm:"column:ip_address"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (refreshTokenRow) TableName() string { return "refresh_tokens" }

func newRefreshTokenRow(t RefreshToken) refreshTokenRow {
	return refreshTokenRow{
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		UserAgent: nilIfEmpty(t.UserAgent),
		IPAddress: nilIfEmpty(t.IPAddress),
		ExpiresAt: t.ExpiresAt,
	}
}

// OracleRepository menyimpan data AuthService di schema AUTH_SVC memakai GORM.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// CreateUser menyimpan user baru. GORM mengisi ID, Role, Status dari Oracle.
func (r *OracleRepository) CreateUser(ctx context.Context, u NewUser) (User, error) {
	row := userRow{
		Username:     u.Username,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		DisplayName:  u.DisplayName,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		switch uniqueConstraint(err) {
		case "USERS_USERNAME_UQ":
			return User{}, ErrUsernameTaken
		case "USERS_EMAIL_UQ":
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	return row.toUser(), nil
}

// FindUserByLogin mencari berdasarkan username ATAU email. GORM otomatis
// menambahkan "deleted_at IS NULL" karena ada field gorm.DeletedAt.
func (r *OracleRepository) FindUserByLogin(ctx context.Context, login string) (User, string, error) {
	var row userRow
	err := r.db.WithContext(ctx).
		Where("username = ? OR email = ?", login, login).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("select user by login: %w", err)
	}
	return row.toUser(), row.PasswordHash, nil
}

// FindUserByID mencari user berdasarkan id.
func (r *OracleRepository) FindUserByID(ctx context.Context, id int64) (User, error) {
	var row userRow
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("select user by id: %w", err)
	}
	return row.toUser(), nil
}

// CreateRefreshToken menyimpan refresh token baru.
func (r *OracleRepository) CreateRefreshToken(ctx context.Context, t RefreshToken) error {
	row := newRefreshTokenRow(t)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// RotateRefreshToken mencabut token lama dan menyimpan penggantinya dalam
// satu transaksi. clause.Locking membuat GORM menulis SELECT ... FOR UPDATE,
// yang mengunci baris token lama: jika dua request refresh datang bersamaan
// dengan token yang sama, hanya satu yang berhasil.
func (r *OracleRepository) RotateRefreshToken(ctx context.Context, oldHash string, next RefreshToken, now time.Time) (int64, error) {
	var (
		userID int64
		reused bool
	)
	// db.Transaction otomatis COMMIT jika fungsi mengembalikan nil, dan
	// ROLLBACK jika mengembalikan error. Mirip @Transactional di Spring.
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old refreshTokenRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", oldHash).
			Take(&old).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("select refresh token: %w", err)
		}

		if old.RevokedAt != nil {
			// Token yang sudah dirotasi dipakai lagi: kemungkinan besar dicuri.
			// Cabut semua sesi milik user ini, jadi pencuri dan pemilik
			// sama-sama harus login ulang. Perubahan ini harus di-COMMIT, jadi
			// kita kembalikan nil dan menandai reused.
			reused = true
			return tx.Model(&refreshTokenRow{}).
				Where("user_id = ? AND revoked_at IS NULL", old.UserID).
				Update("revoked_at", now).Error
		}
		if !now.Before(old.ExpiresAt) {
			return ErrInvalidRefreshToken
		}

		if err := tx.Model(&old).Update("revoked_at", now).Error; err != nil {
			return fmt.Errorf("revoke refresh token: %w", err)
		}
		next.UserID = old.UserID
		row := newRefreshTokenRow(next)
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert refresh token: %w", err)
		}
		userID = old.UserID
		return nil
	})
	if err != nil {
		return 0, err
	}
	if reused {
		return 0, ErrInvalidRefreshToken
	}
	return userID, nil
}

// RevokeRefreshToken mencabut token jika masih aktif.
func (r *OracleRepository) RevokeRefreshToken(ctx context.Context, hash string, now time.Time) error {
	err := r.db.WithContext(ctx).Model(&refreshTokenRow{}).
		Where("token_hash = ? AND revoked_at IS NULL", hash).
		Update("revoked_at", now).Error
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// uniqueConstraint mengembalikan nama constraint jika err adalah
// ORA-00001 (pelanggaran UNIQUE), misalnya "USERS_EMAIL_UQ".
func uniqueConstraint(err error) string {
	oerr, ok := godror.AsOraErr(err)
	if !ok || oerr.Code() != 1 {
		return ""
	}
	return constraintName(oerr.Message())
}

// constraintName mengambil nama constraint dari pesan seperti:
// ORA-00001: unique constraint (AUTH_SVC.USERS_EMAIL_UQ) violated ...
func constraintName(msg string) string {
	start := strings.Index(msg, "(")
	end := strings.Index(msg, ")")
	if start < 0 || end < start {
		return ""
	}
	name := msg[start+1 : end]
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}
