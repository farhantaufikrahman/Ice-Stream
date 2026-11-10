package video

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// videoRow memetakan tabel VIDEOS (schema VIDEO_SVC).
type videoRow struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	OwnerID     int64      `gorm:"column:owner_id"`
	Title       string     `gorm:"column:title"`
	Description *string    `gorm:"column:description"`
	Status      string     `gorm:"column:status;default:PENDING_UPLOAD"`
	ObjectKey   string     `gorm:"column:object_key"`
	ContentType string     `gorm:"column:content_type"`
	SizeBytes   int64      `gorm:"column:size_bytes"`
	UploadedAt  *time.Time `gorm:"column:uploaded_at"`
	// Kolom hasil transcoding (migrasi V2). NULL sampai status READY/FAILED.
	PlaylistKey   *string    `gorm:"column:playlist_key"`
	ThumbnailKey  *string    `gorm:"column:thumbnail_key"`
	DurationMs    *int64     `gorm:"column:duration_ms"`
	Renditions    *string    `gorm:"column:renditions"`
	FailureReason *string    `gorm:"column:failure_reason"`
	ProcessedAt   *time.Time `gorm:"column:processed_at"`
	// Migrasi V3. "<-:update" = tidak ikut INSERT saat Create (Oracle
	// mengisi DEFAULT 0), hanya boleh diubah lewat UPDATE (IncrementViews).
	ViewCount int64     `gorm:"column:view_count;<-:update"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
	// Belum ada fitur hapus, tapi kolomnya sudah ada di tabel. Dengan field
	// ini GORM otomatis menyaring baris yang sudah dihapus.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
	// Migrasi V5. "<-:false" = hanya dibaca. Nilainya diubah oleh
	// CommentService (package comment), di transaksi yang sama dengan
	// komentarnya.
	CommentCount int64 `gorm:"column:comment_count;<-:false"`
	// Migrasi V6. "<-:update" = tidak ikut INSERT, hanya diubah lewat
	// UPDATE di Like dan Unlike.
	LikeCount int64 `gorm:"column:like_count;<-:update"`
	// Migrasi V8: waktu kata-kata video ini disimpan untuk pencarian.
	SearchIndexedAt *time.Time `gorm:"column:search_indexed_at"`
}

func (videoRow) TableName() string { return "videos" }

func (r videoRow) toVideo() Video {
	v := Video{
		ID:          r.ID,
		OwnerID:     r.OwnerID,
		Title:       r.Title,
		Status:      r.Status,
		ObjectKey:   r.ObjectKey,
		ContentType: r.ContentType,
		SizeBytes:   r.SizeBytes,
		CreatedAt:   r.CreatedAt.UTC(),
		ViewCount:   r.ViewCount,
	}
	v.CommentCount = r.CommentCount
	v.LikeCount = r.LikeCount
	if r.Description != nil {
		v.Description = *r.Description
	}
	if r.UploadedAt != nil {
		t := r.UploadedAt.UTC()
		v.UploadedAt = &t
	}
	if r.PlaylistKey != nil {
		v.PlaylistKey = *r.PlaylistKey
	}
	if r.ThumbnailKey != nil {
		v.ThumbnailKey = *r.ThumbnailKey
	}
	if r.DurationMs != nil {
		v.DurationMs = *r.DurationMs
	}
	if r.Renditions != nil && *r.Renditions != "" {
		v.Renditions = strings.Split(*r.Renditions, ",")
	}
	if r.FailureReason != nil {
		v.FailureReason = *r.FailureReason
	}
	if r.ProcessedAt != nil {
		t := r.ProcessedAt.UTC()
		v.ProcessedAt = &t
	}
	return v
}

// OracleRepository menyimpan data VideoService di schema VIDEO_SVC memakai GORM.
type OracleRepository struct {
	db *gorm.DB
}

// NewOracleRepository membuat repository dari koneksi GORM.
func NewOracleRepository(db *gorm.DB) *OracleRepository {
	return &OracleRepository{db: db}
}

var _ Repository = (*OracleRepository)(nil)

// Create menyimpan video baru. GORM mengisi ID dan Status dari Oracle.
func (r *OracleRepository) Create(ctx context.Context, v NewVideo) (Video, error) {
	row := videoRow{
		OwnerID:     v.OwnerID,
		Title:       v.Title,
		ObjectKey:   v.ObjectKey,
		ContentType: v.ContentType,
		SizeBytes:   v.SizeBytes,
	}
	// String kosong disimpan sebagai NULL (Oracle memang menganggap '' = NULL).
	if v.Description != "" {
		row.Description = &v.Description
	}
	now := time.Now().UTC()
	row.SearchIndexedAt = &now
	// Video dan kata-katanya disimpan dalam satu transaksi: tidak ada video
	// yang tersimpan tanpa kata pencarian, atau sebaliknya.
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert video: %w", err)
		}
		return insertTerms(tx, row.ID, v.SearchTerms)
	})
	if err != nil {
		return Video{}, err
	}
	return row.toVideo(), nil
}

// FindByID mencari video berdasarkan id.
func (r *OracleRepository) FindByID(ctx context.Context, id int64) (Video, error) {
	var row videoRow
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, fmt.Errorf("select video: %w", err)
	}
	return row.toVideo(), nil
}

// MarkUploaded mengubah status PENDING_UPLOAD menjadi UPLOADED. Syarat
// status ada di WHERE, jadi kalau dua request datang bersamaan, hanya satu
// yang benar-benar mengubah baris (yang lain mendapat ErrNotUploading).
func (r *OracleRepository) MarkUploaded(ctx context.Context, id int64, now time.Time) (Video, error) {
	res := r.db.WithContext(ctx).Model(&videoRow{}).
		Where("id = ? AND status = ?", id, StatusPendingUpload).
		Updates(map[string]any{"status": StatusUploaded, "uploaded_at": now, "updated_at": now})
	if res.Error != nil {
		return Video{}, fmt.Errorf("update video status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return Video{}, ErrNotUploading
	}
	return r.FindByID(ctx, id)
}

// ListByOwner mengambil maksimal limit video milik ownerID dengan id lebih
// kecil dari beforeID (0 = mulai dari yang terbaru). Cara ini disebut
// keyset pagination: lebih cepat dari OFFSET untuk tabel besar, dan tidak
// melompati data saat ada video baru masuk.
func (r *OracleRepository) ListByOwner(ctx context.Context, ownerID, beforeID int64, limit int) ([]Video, error) {
	q := r.db.WithContext(ctx).Where("owner_id = ?", ownerID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []videoRow
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select videos by owner: %w", err)
	}
	videos := make([]Video, len(rows))
	for i, row := range rows {
		videos[i] = row.toVideo()
	}
	return videos, nil
}

// ListPublic mengambil maksimal limit video READY untuk feed publik,
// diurutkan processed_at lalu id dari yang terbaru. Jika ownerIDs diisi,
// hanya video milik user-user itu (halaman channel atau feed
// subscription). Jika after tidak nil, hanya video setelah cursor itu
// (halaman berikutnya).
//
// Oracle tidak mendukung perbandingan (a, b) < (x, y), jadi syaratnya
// ditulis panjang: processed_at lebih kecil, ATAU processed_at sama dan id
// lebih kecil. id ikut dipakai supaya urutan tetap pasti walau dua video
// selesai di detik yang sama.
//
// processed_at selalu dibungkus SYS_EXTRACT_UTC (mengubah waktu menjadi
// UTC). Untuk kolom TIMESTAMP WITH TIME ZONE, Oracle menyimpan index-nya
// dalam bentuk SYS_EXTRACT_UTC(processed_at). ORDER BY harus memakai
// ekspresi yang sama persis; kalau tidak, Oracle tidak bisa memakai urutan
// index videos_feed_ix / videos_owner_feed_ix, sehingga ia membaca SEMUA
// video READY lalu mengurutkannya dulu, makin lambat setiap ada video baru.
// Dengan SYS_EXTRACT_UTC, Oracle membaca index dari belakang dan berhenti
// setelah limit baris (sudah dicek dengan EXPLAIN PLAN, lihat langkah 14).
func (r *OracleRepository) ListPublic(ctx context.Context, ownerIDs []int64, after *FeedCursor, limit int) ([]Video, error) {
	q := r.db.WithContext(ctx).Where("status = ?", StatusReady)
	switch len(ownerIDs) {
	case 0:
	case 1:
		q = q.Where("owner_id = ?", ownerIDs[0])
	default:
		// Oracle membatasi IN (...) maksimal 1000 nilai; Service sudah
		// memastikan ownerIDs tidak lebih dari itu.
		q = q.Where("owner_id IN ?", ownerIDs)
	}
	if after != nil {
		q = q.Where("(SYS_EXTRACT_UTC(processed_at) < SYS_EXTRACT_UTC(?) OR "+
			"(SYS_EXTRACT_UTC(processed_at) = SYS_EXTRACT_UTC(?) AND id < ?))",
			after.ProcessedAt, after.ProcessedAt, after.ID)
	}
	var rows []videoRow
	if err := q.Order("SYS_EXTRACT_UTC(processed_at) DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select public videos: %w", err)
	}
	videos := make([]Video, len(rows))
	for i, row := range rows {
		videos[i] = row.toVideo()
	}
	return videos, nil
}

// IncrementViews menambah view_count video READY sebanyak 1 lalu
// mengembalikan nilai barunya. "view_count = view_count + 1" dihitung oleh
// Oracle sendiri dalam satu UPDATE, jadi tidak ada view yang hilang walau
// banyak request datang bersamaan (berbeda dengan baca, tambah di Go, lalu
// simpan).
func (r *OracleRepository) IncrementViews(ctx context.Context, id int64) (int64, error) {
	res := r.db.WithContext(ctx).Model(&videoRow{}).
		Where("id = ? AND status = ?", id, StatusReady).
		UpdateColumn("view_count", gorm.Expr("view_count + 1"))
	if res.Error != nil {
		return 0, fmt.Errorf("update view_count: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return 0, ErrNotFound
	}
	var row videoRow
	if err := r.db.WithContext(ctx).Select("view_count").Where("id = ?", id).Take(&row).Error; err != nil {
		return 0, fmt.Errorf("select view_count: %w", err)
	}
	return row.ViewCount, nil
}

// MarkProcessing mengubah UPLOADED menjadi PROCESSING.
func (r *OracleRepository) MarkProcessing(ctx context.Context, id int64, now time.Time) (bool, error) {
	return r.transition(ctx, id, []string{StatusUploaded}, map[string]any{
		"status": StatusProcessing, "updated_at": now,
	})
}

// MarkReady mengubah UPLOADED/PROCESSING menjadi READY. UPLOADED ikut
// diterima karena event "started" bisa saja datang terlambat.
func (r *OracleRepository) MarkReady(ctx context.Context, id int64, res TranscodeResult, now time.Time) (bool, error) {
	return r.transition(ctx, id, []string{StatusUploaded, StatusProcessing}, map[string]any{
		"status":         StatusReady,
		"playlist_key":   res.PlaylistKey,
		"thumbnail_key":  res.ThumbnailKey,
		"duration_ms":    res.DurationMs,
		"renditions":     strings.Join(res.Renditions, ","),
		"failure_reason": nil,
		"processed_at":   now,
		"updated_at":     now,
	})
}

// MarkFailed mengubah UPLOADED/PROCESSING menjadi FAILED.
func (r *OracleRepository) MarkFailed(ctx context.Context, id int64, reason string, now time.Time) (bool, error) {
	return r.transition(ctx, id, []string{StatusUploaded, StatusProcessing}, map[string]any{
		"status": StatusFailed, "failure_reason": reason, "processed_at": now, "updated_at": now,
	})
}

// transition mengubah baris hanya jika statusnya sekarang ada di from.
// Syarat ini ada di WHERE, jadi tetap benar walau event datang dobel atau
// tidak berurutan: video READY tidak akan kembali menjadi PROCESSING.
func (r *OracleRepository) transition(ctx context.Context, id int64, from []string, updates map[string]any) (bool, error) {
	res := r.db.WithContext(ctx).Model(&videoRow{}).
		Where("id = ? AND status IN ?", id, from).
		Updates(updates)
	if res.Error != nil {
		return false, fmt.Errorf("update video %d ke %v: %w", id, updates["status"], res.Error)
	}
	return res.RowsAffected > 0, nil
}

// likeRow memetakan tabel VIDEO_LIKES (migrasi V6).
type likeRow struct {
	VideoID   int64     `gorm:"column:video_id;primaryKey"`
	UserID    int64     `gorm:"column:user_id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (likeRow) TableName() string { return "video_likes" }

// Like mencatat like dalam satu transaksi:
//  1. kunci baris video READY dengan SELECT ... FOR UPDATE;
//  2. cek apakah user ini sudah like;
//  3. jika belum: simpan like dan tambah like_count.
//
// Karena baris video dikunci lebih dulu, semua like dan unlike untuk video
// yang sama berjalan bergantian. Jadi cek di langkah 2 tidak bisa
// "kecolongan" oleh request lain, dan Unlike memakai urutan kunci yang
// sama sehingga tidak bisa deadlock.
func (r *OracleRepository) Like(ctx context.Context, videoID, userID int64) (int64, error) {
	return r.changeLike(ctx, videoID, userID, true)
}

// Unlike menghapus like (jika ada) dalam satu transaksi, dengan urutan
// kunci yang sama dengan Like.
func (r *OracleRepository) Unlike(ctx context.Context, videoID, userID int64) (int64, error) {
	return r.changeLike(ctx, videoID, userID, false)
}

func (r *OracleRepository) changeLike(ctx context.Context, videoID, userID int64, like bool) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var v videoRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "like_count").
			Where("id = ? AND status = ?", videoID, StatusReady).
			Take(&v).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select video for update: %w", err)
		}
		count = v.LikeCount

		var exists int64
		err = tx.Model(&likeRow{}).Where("video_id = ? AND user_id = ?", videoID, userID).Count(&exists).Error
		if err != nil {
			return fmt.Errorf("select like: %w", err)
		}
		if (exists > 0) == like {
			return nil // sudah sesuai keinginan, tidak ada yang diubah
		}

		delta := int64(1)
		if like {
			err = tx.Create(&likeRow{VideoID: videoID, UserID: userID}).Error
		} else {
			delta = -1
			err = tx.Where("video_id = ? AND user_id = ?", videoID, userID).Delete(&likeRow{}).Error
		}
		if err != nil {
			return fmt.Errorf("simpan like: %w", err)
		}
		err = tx.Model(&videoRow{}).Where("id = ?", videoID).
			UpdateColumn("like_count", gorm.Expr("like_count + ?", delta)).Error
		if err != nil {
			return fmt.Errorf("update like_count: %w", err)
		}
		count += delta
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// IsLiked mengembalikan true jika userID sudah like videoID.
func (r *OracleRepository) IsLiked(ctx context.Context, videoID, userID int64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&likeRow{}).
		Where("video_id = ? AND user_id = ?", videoID, userID).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("select like: %w", err)
	}
	return n > 0, nil
}

// searchTermRow memetakan tabel VIDEO_SEARCH_TERMS (migrasi V8).
type searchTermRow struct {
	Term    string `gorm:"column:term;primaryKey"`
	VideoID int64  `gorm:"column:video_id;primaryKey"`
	Weight  int    `gorm:"column:weight"`
}

func (searchTermRow) TableName() string { return "video_search_terms" }

// insertTerms menyimpan kata-kata satu video, 100 baris per INSERT.
func insertTerms(tx *gorm.DB, videoID int64, terms []SearchTerm) error {
	if len(terms) == 0 {
		return nil
	}
	rows := make([]searchTermRow, len(terms))
	for i, t := range terms {
		rows[i] = searchTermRow{Term: t.Term, VideoID: videoID, Weight: t.Weight}
	}
	if err := tx.CreateInBatches(rows, 100).Error; err != nil {
		return fmt.Errorf("insert search terms: %w", err)
	}
	return nil
}

// searchHit adalah satu baris hasil query pencarian: id video dan skornya.
type searchHit struct {
	VideoID int64 `gorm:"column:video_id"`
	Score   int   `gorm:"column:score"`
}

// Search mencari dalam dua query. Query pertama mencari id video yang
// cocok beserta urutannya. Untuk terms ["belajar", "go"]:
//
//	SELECT s.video_id AS "video_id", s.score AS "score" FROM (
//	  SELECT video_id, SUM(w) AS score FROM (
//	    SELECT video_id, MAX(weight) AS w FROM video_search_terms
//	     WHERE term LIKE 'belajar%' GROUP BY video_id
//	    UNION ALL
//	    SELECT video_id, MAX(weight) AS w FROM video_search_terms
//	     WHERE term LIKE 'go%' GROUP BY video_id
//	  ) GROUP BY video_id
//	  HAVING COUNT(*) = 2            -- video harus cocok dengan SEMUA kata
//	) s JOIN videos v ON v.id = s.video_id
//	WHERE v.status = 'READY' AND v.deleted_at IS NULL
//	ORDER BY s.score DESC, v.view_count DESC, v.id DESC
//	OFFSET 0 ROWS FETCH NEXT 21 ROWS ONLY
//
// Setiap subquery menghasilkan paling banyak satu baris per video, jadi
// COUNT(*) = jumlah kata yang cocok. "LIKE 'go%'" (tanpa % di depan) bisa
// memakai index: Oracle langsung melompat ke kata yang diawali "go". Kata
// dari ParseSearchQuery hanya berisi huruf dan angka, jadi tidak ada % atau
// _ yang perlu di-escape.
//
// Query kedua mengambil data lengkap video-video itu dengan model GORM
// biasa (videoRow), lalu disusun ulang mengikuti urutan query pertama.
func (r *OracleRepository) Search(ctx context.Context, terms []string, offset, limit int) ([]Video, error) {
	parts := make([]string, len(terms))
	args := make([]any, 0, len(terms)+4)
	for i, t := range terms {
		parts[i] = "SELECT video_id, MAX(weight) AS w FROM video_search_terms WHERE term LIKE ? GROUP BY video_id"
		args = append(args, t+"%")
	}
	// Alias dengan tanda kutip ("video_id") supaya Oracle mengembalikan nama
	// kolom huruf kecil persis seperti tag GORM di searchHit. Tanpa kutip,
	// Oracle mengembalikan VIDEO_ID dan GORM tidak mengisi field VideoID.
	sql := `SELECT s.video_id AS "video_id", s.score AS "score" FROM (` +
		"SELECT video_id, SUM(w) AS score FROM (" + strings.Join(parts, " UNION ALL ") + ") " +
		"GROUP BY video_id HAVING COUNT(*) = ?" +
		") s JOIN videos v ON v.id = s.video_id " +
		"WHERE v.status = ? AND v.deleted_at IS NULL " +
		"ORDER BY s.score DESC, v.view_count DESC, v.id DESC " +
		"OFFSET ? ROWS FETCH NEXT ? ROWS ONLY"
	args = append(args, len(terms), StatusReady, offset, limit)

	var hits []searchHit
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&hits).Error; err != nil {
		return nil, fmt.Errorf("search videos: %w", err)
	}
	if len(hits) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.VideoID
	}
	var rows []videoRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select search results: %w", err)
	}
	byID := make(map[int64]videoRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	videos := make([]Video, 0, len(hits))
	for _, h := range hits {
		// Video yang dihapus di antara dua query dilewati saja.
		if row, ok := byID[h.VideoID]; ok {
			videos = append(videos, row.toVideo())
		}
	}
	return videos, nil
}

// ListUnindexed mengambil video yang search_indexed_at-nya masih NULL.
func (r *OracleRepository) ListUnindexed(ctx context.Context, limit int) ([]Video, error) {
	var rows []videoRow
	err := r.db.WithContext(ctx).Select("id", "title", "description").
		Where("search_indexed_at IS NULL").Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select unindexed videos: %w", err)
	}
	videos := make([]Video, len(rows))
	for i, row := range rows {
		videos[i] = row.toVideo()
	}
	return videos, nil
}

// SaveSearchTerms mengunci baris video (SELECT ... FOR UPDATE) lalu
// memeriksa ulang search_indexed_at. Jika dua pod VideoService start
// bersamaan, pod kedua menunggu kunci, melihat video sudah diindeks, dan
// tidak menyimpan kata yang sama dua kali.
func (r *OracleRepository) SaveSearchTerms(ctx context.Context, videoID int64, terms []SearchTerm, now time.Time) (bool, error) {
	saved := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row videoRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "search_indexed_at").Where("id = ?", videoID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select video for update: %w", err)
		}
		if row.SearchIndexedAt != nil {
			return nil
		}
		if err := insertTerms(tx, videoID, terms); err != nil {
			return err
		}
		err = tx.Model(&videoRow{}).Where("id = ?", videoID).UpdateColumn("search_indexed_at", now).Error
		if err != nil {
			return fmt.Errorf("update search_indexed_at: %w", err)
		}
		saved = true
		return nil
	})
	return saved, err
}
