package comment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// statusReady sama dengan video.StatusReady. Ditulis ulang di sini supaya
// package comment tidak bergantung pada package video.
const statusReady = "READY"

// watchableSQL: video boleh ditonton siapa saja yang punya tautannya, sama
// dengan watchableSQL di package video (langkah 26). Video draf dan
// Pribadi tidak bisa diberi komentar atau danmaku, juga oleh pemiliknya.
const watchableSQL = "status = 'READY' AND draft = 0 AND visibility IN ('PUBLIC', 'UNLISTED')"

// commentRow memetakan tabel COMMENTS (schema VIDEO_SVC).
type commentRow struct {
	ID       int64  `gorm:"column:id;primaryKey;autoIncrement"`
	VideoID  int64  `gorm:"column:video_id"`
	AuthorID int64  `gorm:"column:author_id"`
	ParentID *int64 `gorm:"column:parent_id"`
	Body     string `gorm:"column:body"`
	// "<-:update": tidak ikut INSERT (Oracle mengisi DEFAULT 0).
	ReplyCount int64     `gorm:"column:reply_count;<-:update"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	// Dengan field ini GORM otomatis menambahkan "deleted_at IS NULL" di
	// setiap SELECT dan UPDATE.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (commentRow) TableName() string { return "comments" }

func (r commentRow) toComment() Comment {
	c := Comment{
		ID:         r.ID,
		VideoID:    r.VideoID,
		AuthorID:   r.AuthorID,
		Body:       r.Body,
		ReplyCount: r.ReplyCount,
		CreatedAt:  r.CreatedAt.UTC(),
	}
	if r.ParentID != nil {
		c.ParentID = *r.ParentID
	}
	return c
}

// videoRef hanya memetakan kolom tabel VIDEOS yang dibutuhkan komentar.
// "<-:update" pada CommentCount: package ini hanya boleh mengubah
// comment_count, tidak pernah INSERT video baru.
type videoRef struct {
	ID           int64          `gorm:"column:id;primaryKey"`
	OwnerID      int64          `gorm:"column:owner_id"`
	CommentCount int64          `gorm:"column:comment_count;<-:update"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (videoRef) TableName() string { return "videos" }

// OracleRepository menyimpan komentar di schema VIDEO_SVC memakai GORM.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// CheckVideo memastikan video ada dan READY.
func (r *OracleRepository) CheckVideo(ctx context.Context, videoID int64) error {
	var v videoRef
	err := r.db.WithContext(ctx).Select("id").
		Where("id = ? AND "+watchableSQL, videoID).
		Take(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrVideoNotFound
	}
	if err != nil {
		return fmt.Errorf("select video: %w", err)
	}
	return nil
}

// Create menyimpan komentar dalam satu transaksi:
//  1. comment_count video ditambah 1. UPDATE ini sekaligus memeriksa video
//     READY, dan mengunci baris video sampai COMMIT;
//  2. untuk balasan: cari komentar yang dibalas, lalu tambah reply_count
//     komentar utamanya;
//  3. simpan komentar.
//
// Urutan kunci selalu "video dulu, baru komentar", sama dengan Delete.
// Dengan urutan yang sama, dua transaksi tidak akan saling menunggu
// selamanya (deadlock).
func (r *OracleRepository) Create(ctx context.Context, nc NewComment) (Comment, error) {
	var row commentRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&videoRef{}).
			Where("id = ? AND "+watchableSQL, nc.VideoID).
			UpdateColumn("comment_count", gorm.Expr("comment_count + 1"))
		if res.Error != nil {
			return fmt.Errorf("update comment_count: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrVideoNotFound
		}

		row = commentRow{VideoID: nc.VideoID, AuthorID: nc.AuthorID, Body: nc.Body}
		if nc.ParentID > 0 {
			rootID, err := r.addReply(tx, nc.VideoID, nc.ParentID)
			if err != nil {
				return err
			}
			row.ParentID = &rootID
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert comment: %w", err)
		}
		return nil
	})
	if err != nil {
		return Comment{}, err
	}
	return row.toComment(), nil
}

// addReply mencari komentar yang dibalas, lalu menambah reply_count
// komentar utamanya. Jika yang dibalas adalah balasan, komentar utamanya
// adalah parent dari balasan itu.
func (r *OracleRepository) addReply(tx *gorm.DB, videoID, parentID int64) (int64, error) {
	var parent commentRow
	err := tx.Where("id = ? AND video_id = ?", parentID, videoID).Take(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("select parent comment: %w", err)
	}
	rootID := parent.ID
	if parent.ParentID != nil {
		rootID = *parent.ParentID
	}
	res := tx.Model(&commentRow{}).
		Where("id = ? AND parent_id IS NULL", rootID).
		UpdateColumn("reply_count", gorm.Expr("reply_count + 1"))
	if res.Error != nil {
		return 0, fmt.Errorf("update reply_count: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return 0, ErrNotFound
	}
	return rootID, nil
}

// FindRoot mencari komentar utama milik videoID.
func (r *OracleRepository) FindRoot(ctx context.Context, videoID, commentID int64) (Comment, error) {
	var row commentRow
	err := r.db.WithContext(ctx).
		Where("id = ? AND video_id = ? AND parent_id IS NULL", commentID, videoID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("select comment: %w", err)
	}
	return row.toComment(), nil
}

// ListRoots mengambil komentar utama, terbaru lebih dulu (keyset
// pagination dengan id, seperti ListByOwner di package video).
func (r *OracleRepository) ListRoots(ctx context.Context, videoID, beforeID int64, limit int) ([]Comment, error) {
	q := r.db.WithContext(ctx).Where("video_id = ? AND parent_id IS NULL", videoID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	return list(q.Order("id DESC").Limit(limit))
}

// ListReplies mengambil balasan, terlama lebih dulu.
func (r *OracleRepository) ListReplies(ctx context.Context, videoID, rootID, afterID int64, limit int) ([]Comment, error) {
	q := r.db.WithContext(ctx).Where("video_id = ? AND parent_id = ?", videoID, rootID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	return list(q.Order("id").Limit(limit))
}

func list(q *gorm.DB) ([]Comment, error) {
	var rows []commentRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select comments: %w", err)
	}
	comments := make([]Comment, len(rows))
	for i, row := range rows {
		comments[i] = row.toComment()
	}
	return comments, nil
}

// Delete menghapus komentar dalam satu transaksi. Baris video dikunci
// lebih dulu dengan SELECT ... FOR UPDATE (urutan kunci sama dengan
// Create), jadi selama transaksi ini berjalan tidak ada balasan baru yang
// masuk ke komentar yang sedang dihapus.
func (r *OracleRepository) Delete(ctx context.Context, videoID, commentID int64, now time.Time, authorize func(Comment, int64) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var v videoRef
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "owner_id").
			Where("id = ? AND status = ?", videoID, statusReady).
			Take(&v).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoNotFound
		}
		if err != nil {
			return fmt.Errorf("select video for update: %w", err)
		}

		var row commentRow
		err = tx.Where("id = ? AND video_id = ?", commentID, videoID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select comment: %w", err)
		}
		if err := authorize(row.toComment(), v.OwnerID); err != nil {
			return err
		}

		if err := tx.Model(&commentRow{}).Where("id = ?", row.ID).UpdateColumn("deleted_at", now).Error; err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		removed := int64(1)
		if row.ParentID == nil {
			// Komentar utama: semua balasannya ikut dihapus.
			res := tx.Model(&commentRow{}).Where("parent_id = ?", row.ID).UpdateColumn("deleted_at", now)
			if res.Error != nil {
				return fmt.Errorf("delete replies: %w", res.Error)
			}
			removed += res.RowsAffected
		} else {
			err := tx.Model(&commentRow{}).Where("id = ?", *row.ParentID).
				UpdateColumn("reply_count", gorm.Expr("reply_count - 1")).Error
			if err != nil {
				return fmt.Errorf("update reply_count: %w", err)
			}
		}

		err = tx.Model(&videoRef{}).Where("id = ?", videoID).
			UpdateColumn("comment_count", gorm.Expr("comment_count - ?", removed)).Error
		if err != nil {
			return fmt.Errorf("update comment_count: %w", err)
		}
		return nil
	})
}

// channelCommentRow adalah satu baris hasil ListByChannel. Aliasnya memakai
// tanda kutip supaya Oracle mengembalikan nama kolom huruf kecil, sama
// seperti searchHit di package video.
type channelCommentRow struct {
	ID         int64     `gorm:"column:id"`
	VideoID    int64     `gorm:"column:video_id"`
	AuthorID   int64     `gorm:"column:author_id"`
	ParentID   *int64    `gorm:"column:parent_id"`
	Body       string    `gorm:"column:body"`
	ReplyCount int64     `gorm:"column:reply_count"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	VideoTitle string    `gorm:"column:video_title"`
}

// ListByChannel:
//
//	SELECT c.id, ..., v.title AS video_title
//	  FROM comments c JOIN videos v ON v.id = c.video_id
//	 WHERE v.owner_id = 7 AND v.deleted_at IS NULL AND c.deleted_at IS NULL
//	   [AND c.video_id = 42] [AND c.id < 900]
//	 ORDER BY c.id DESC FETCH FIRST 21 ROWS ONLY
//
// Oracle mencari video milik channel lewat index videos_owner_id_ix, lalu
// komentarnya lewat comments_video_ix. Untuk semua video, komentar dari
// banyak video harus diurutkan dulu; cukup cepat untuk channel dengan
// ribuan komentar. Channel yang jauh lebih besar butuh kolom owner_id di
// tabel comments beserta index (owner_id, id).
func (r *OracleRepository) ListByChannel(ctx context.Context, ownerID, videoID, beforeID int64, limit int) ([]ChannelComment, error) {
	sql := `SELECT c.id AS "id", c.video_id AS "video_id", c.author_id AS "author_id", c.parent_id AS "parent_id",
       c.body AS "body", c.reply_count AS "reply_count", c.created_at AS "created_at", v.title AS "video_title"
  FROM comments c JOIN videos v ON v.id = c.video_id
 WHERE v.owner_id = ? AND v.deleted_at IS NULL AND c.deleted_at IS NULL`
	args := []any{ownerID}
	if videoID > 0 {
		sql += " AND c.video_id = ?"
		args = append(args, videoID)
	}
	if beforeID > 0 {
		sql += " AND c.id < ?"
		args = append(args, beforeID)
	}
	sql += " ORDER BY c.id DESC FETCH FIRST ? ROWS ONLY"
	args = append(args, limit)

	var rows []channelCommentRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("select channel comments: %w", err)
	}
	out := make([]ChannelComment, len(rows))
	for i, row := range rows {
		c := commentRow{ID: row.ID, VideoID: row.VideoID, AuthorID: row.AuthorID, ParentID: row.ParentID,
			Body: row.Body, ReplyCount: row.ReplyCount, CreatedAt: row.CreatedAt}
		out[i] = ChannelComment{Comment: c.toComment(), VideoTitle: row.VideoTitle}
	}
	return out, nil
}
