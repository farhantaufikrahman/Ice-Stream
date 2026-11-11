package video

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpdateDetails mengunci baris video (SELECT ... FOR UPDATE), lalu dalam
// satu transaksi:
//  1. mengganti title, description, category, tags, updated_at, dan
//     search_indexed_at;
//  2. menghapus semua kata pencarian lama video ini;
//  3. menyimpan kata pencarian baru.
//
// Kunci baris membuat dua edit bersamaan berjalan bergantian, jadi kata
// pencarian selalu cocok dengan judul dan deskripsi yang tersimpan. Kunci
// yang sama dipakai SaveSearchTerms (pengindeksan video lama), jadi
// keduanya juga tidak bisa bertabrakan.
func (r *OracleRepository) UpdateDetails(ctx context.Context, id, ownerID int64, d Details, terms []SearchTerm, now time.Time) (Video, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row videoRow
		// GORM menambahkan "deleted_at IS NULL" sendiri (field DeletedAt).
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("id = ? AND owner_id = ?", id, ownerID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select video for update: %w", err)
		}
		// Teks kosong disimpan sebagai NULL, sama seperti Create.
		err = tx.Model(&videoRow{}).Where("id = ?", id).Updates(map[string]any{
			"title":             d.Title,
			"description":       nullIfEmpty(d.Description),
			"category":          nullIfEmpty(d.Category),
			"tags":              nullIfEmpty(joinTags(d.Tags)),
			"updated_at":        now,
			"search_indexed_at": now,
		}).Error
		if err != nil {
			return fmt.Errorf("update video: %w", err)
		}
		if err := tx.Where("video_id = ?", id).Delete(&searchTermRow{}).Error; err != nil {
			return fmt.Errorf("delete search terms: %w", err)
		}
		return insertTerms(tx, id, terms)
	})
	if err != nil {
		return Video{}, err
	}
	return r.FindByID(ctx, id)
}

// SoftDelete mengisi deleted_at dan menghapus kata pencarian video dalam
// satu transaksi. Syarat pemilik dan status ada di WHERE, jadi UPDATE hanya
// mengenai baris yang memang boleh dihapus, walau status video berubah
// (misalnya transcoder baru mulai) sesaat sebelum query ini.
//
// Kalau tidak ada baris yang berubah, baris video dibaca ulang (dengan
// Unscoped, termasuk yang sudah dihapus) untuk memilih error yang tepat.
func (r *OracleRepository) SoftDelete(ctx context.Context, id, ownerID int64, allowed []string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// UpdateColumn langsung menulis deleted_at; GORM tetap menambahkan
		// "deleted_at IS NULL", jadi video yang sudah dihapus tidak berubah.
		res := tx.Model(&videoRow{}).
			Where("id = ? AND owner_id = ? AND status IN ?", id, ownerID, allowed).
			UpdateColumns(map[string]any{"deleted_at": now, "updated_at": now})
		if res.Error != nil {
			return fmt.Errorf("soft delete video: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			var row videoRow
			err := tx.Unscoped().Select("id", "owner_id", "status", "deleted_at").Where("id = ?", id).Take(&row).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				return ErrNotFound
			case err != nil:
				return fmt.Errorf("select video: %w", err)
			case row.OwnerID != ownerID || row.DeletedAt.Valid:
				return ErrNotFound
			case !slices.Contains(allowed, row.Status):
				return ErrVideoBusy
			}
			return fmt.Errorf("soft delete video %d: tidak ada baris yang berubah", id)
		}
		// Kata pencarian video yang dihapus tidak berguna lagi. Pencarian
		// memang sudah menyaring deleted_at, tapi index-nya jadi lebih kecil.
		if err := tx.Where("video_id = ?", id).Delete(&searchTermRow{}).Error; err != nil {
			return fmt.Errorf("delete search terms: %w", err)
		}
		return nil
	})
}

// ListUnpurged memakai Unscoped supaya GORM tidak menambahkan
// "deleted_at IS NULL": yang dicari justru video yang sudah dihapus.
func (r *OracleRepository) ListUnpurged(ctx context.Context, limit int) ([]Video, error) {
	var rows []videoRow
	err := r.db.WithContext(ctx).Unscoped().Select("id", "object_key").
		Where("deleted_at IS NOT NULL AND media_purged_at IS NULL").
		Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select unpurged videos: %w", err)
	}
	videos := make([]Video, len(rows))
	for i, row := range rows {
		videos[i] = row.toVideo()
	}
	return videos, nil
}

// MarkPurged mengisi media_purged_at. Error jika videonya tidak ada atau
// belum dihapus, supaya PurgeDeleted tidak berputar selamanya pada video
// yang sama.
//
// Ditulis sebagai SQL biasa: dengan Unscoped(), UPDATE lewat GORM +
// gorm-oracle ditolak dengan "WHERE conditions required" walau WHERE-nya
// ada (ditemukan saat uji di sandbox).
func (r *OracleRepository) MarkPurged(ctx context.Context, id int64, now time.Time) error {
	res := r.db.WithContext(ctx).
		Exec("UPDATE videos SET media_purged_at = ? WHERE id = ? AND deleted_at IS NOT NULL", now, id)
	if res.Error != nil {
		return fmt.Errorf("update media_purged_at: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("video %d tidak ada atau belum dihapus", id)
	}
	return nil
}
