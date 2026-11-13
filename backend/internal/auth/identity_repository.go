package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// identityRow memetakan tabel USER_IDENTITIES (migrasi V7).
type identityRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      int64     `gorm:"column:user_id"`
	Provider    string    `gorm:"column:provider"`
	Subject     string    `gorm:"column:subject"`
	Email       *string   `gorm:"column:email"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	LastLoginAt time.Time `gorm:"column:last_login_at"`
}

func (identityRow) TableName() string { return "user_identities" }

func newIdentityRow(userID int64, id Identity, now time.Time) identityRow {
	return identityRow{
		UserID:      userID,
		Provider:    id.Provider,
		Subject:     id.Subject,
		Email:       nilIfEmpty(id.Email),
		CreatedAt:   now,
		LastLoginAt: now,
	}
}

// loginCodeRow memetakan tabel OAUTH_LOGIN_CODES (migrasi V7).
type loginCodeRow struct {
	ID        int64      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64      `gorm:"column:user_id"`
	CodeHash  string     `gorm:"column:code_hash"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (loginCodeRow) TableName() string { return "oauth_login_codes" }

var _ IdentityRepository = (*OracleRepository)(nil)

// FindUserByIdentity:
//
//	SELECT u.* FROM users u JOIN user_identities i ON i.user_id = u.id
//	 WHERE i.provider = 'github' AND i.subject = '583231' AND u.deleted_at IS NULL
//
// memakai index UNIQUE user_identities_subject_uq, lalu memperbarui
// last_login_at dan email di baris identitas.
func (r *OracleRepository) FindUserByIdentity(ctx context.Context, id Identity, now time.Time) (User, error) {
	var row userRow
	err := r.db.WithContext(ctx).
		Joins("JOIN user_identities i ON i.user_id = users.id").
		Where("i.provider = ? AND i.subject = ?", id.Provider, id.Subject).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("select user by identity: %w", err)
	}
	err = r.db.WithContext(ctx).Model(&identityRow{}).
		Where("provider = ? AND subject = ?", id.Provider, id.Subject).
		Updates(map[string]any{"last_login_at": now, "email": nilIfEmpty(id.Email)}).Error
	if err != nil {
		return User{}, fmt.Errorf("update identity: %w", err)
	}
	return row.toUser(), nil
}

// FindUserByEmail memakai index UNIQUE users_email_uq.
func (r *OracleRepository) FindUserByEmail(ctx context.Context, email string) (LinkedUser, error) {
	var row userRow
	err := r.db.WithContext(ctx).Where("email = ?", email).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LinkedUser{}, ErrNotFound
	}
	if err != nil {
		return LinkedUser{}, fmt.Errorf("select user by email: %w", err)
	}
	return LinkedUser{User: row.toUser(), EmailVerified: row.EmailVerifiedAt != nil}, nil
}

// LinkIdentity menyimpan satu baris user_identities.
func (r *OracleRepository) LinkIdentity(ctx context.Context, userID int64, id Identity, now time.Time) error {
	row := newIdentityRow(userID, id, now)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return identityError(err)
	}
	return nil
}

// CreateUserWithIdentity membuat user dan identitasnya dalam satu
// transaksi, jadi tidak pernah ada user Google/GitHub tanpa identitas
// (yang tidak bisa login sama sekali karena tidak punya kata sandi).
func (r *OracleRepository) CreateUserWithIdentity(ctx context.Context, u NewUser, id Identity, now time.Time) (User, error) {
	row := userRow{
		Username:        u.Username,
		Email:           u.Email,
		DisplayName:     u.DisplayName,
		EmailVerifiedAt: &now,
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			switch uniqueConstraint(err) {
			case "USERS_USERNAME_UQ":
				return ErrUsernameTaken
			case "USERS_EMAIL_UQ":
				return ErrEmailTaken
			}
			return fmt.Errorf("insert user: %w", err)
		}
		ident := newIdentityRow(row.ID, id, now)
		if err := tx.Create(&ident).Error; err != nil {
			return identityError(err)
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return row.toUser(), nil
}

// identityError menerjemahkan pelanggaran UNIQUE di user_identities.
func identityError(err error) error {
	switch uniqueConstraint(err) {
	case "USER_IDENTITIES_SUBJECT_UQ":
		return ErrIdentityTaken
	case "USER_IDENTITIES_USER_UQ":
		return ErrProviderAlreadyLinked
	}
	return fmt.Errorf("insert identity: %w", err)
}

// CreateLoginCode menyimpan hash kode login.
func (r *OracleRepository) CreateLoginCode(ctx context.Context, userID int64, codeHash string, expiresAt, now time.Time) error {
	row := loginCodeRow{UserID: userID, CodeHash: codeHash, ExpiresAt: expiresAt, CreatedAt: now}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("insert login code: %w", err)
	}
	return nil
}

// ConsumeLoginCode memakai SELECT ... FOR UPDATE seperti ResetPassword:
// jika kode yang sama dikirim dua kali bersamaan, hanya satu yang berhasil.
func (r *OracleRepository) ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (int64, error) {
	var userID int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row loginCodeRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code_hash = ?", codeHash).
			Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidLoginCode
		}
		if err != nil {
			return fmt.Errorf("select login code: %w", err)
		}
		if row.UsedAt != nil || !now.Before(row.ExpiresAt) {
			return ErrInvalidLoginCode
		}
		if err := tx.Model(&row).Update("used_at", now).Error; err != nil {
			return fmt.Errorf("tandai login code terpakai: %w", err)
		}
		userID = row.UserID
		return nil
	})
	return userID, err
}
