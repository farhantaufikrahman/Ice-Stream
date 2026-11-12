package video

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Langkah 27: angka di kepala halaman kanal (desain Channel: video, total
// tayangan, suka) dan "Video unggulan".

// ChannelCounts adalah jumlah dari semua video publik satu kanal.
type ChannelCounts struct {
	VideoCount int64
	TotalViews int64
	TotalLikes int64
}

// ChannelSummary adalah ChannelCounts beserta Video unggulan.
type ChannelSummary struct {
	Totals ChannelCounts
	// Featured nil jika belum dipilih atau sudah tidak publik.
	Featured *Video
}

// GetChannelSummary dipakai halaman kanal. Boleh dibuka siapa saja.
func (s *Service) GetChannelSummary(ctx context.Context, ownerID int64) (ChannelSummary, error) {
	if ownerID <= 0 {
		return ChannelSummary{}, &ValidationError{"owner_id", "wajib diisi"}
	}
	totals, featuredID, err := s.repo.ChannelSummary(ctx, ownerID)
	if err != nil {
		return ChannelSummary{}, err
	}
	out := ChannelSummary{Totals: totals}
	if featuredID > 0 {
		// Video yang sudah dihapus, dijadikan pribadi, atau jadi draf lagi
		// tidak ditampilkan, tanpa perlu menghapus pilihannya: jika video
		// itu publik lagi, ia otomatis kembali menjadi unggulan.
		found, err := s.repo.FindListedByIDs(ctx, []int64{featuredID})
		if err != nil {
			return ChannelSummary{}, err
		}
		if len(found) == 1 && found[0].OwnerID == ownerID {
			out.Featured = &found[0]
		}
	}
	return out, nil
}

// SetFeaturedVideo memilih Video unggulan kanal requester. videoID 0 =
// hapus pilihan.
func (s *Service) SetFeaturedVideo(ctx context.Context, requesterID, videoID int64) (*Video, error) {
	if videoID < 0 {
		return nil, &ValidationError{"video_id", "tidak valid"}
	}
	if videoID == 0 {
		return nil, s.repo.SetFeaturedVideo(ctx, requesterID, 0, s.now())
	}
	v, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if v.OwnerID != requesterID {
		return nil, ErrNotFound
	}
	if !v.IsListed() {
		return nil, &ValidationError{"video_id", "hanya video publik yang sudah terbit yang bisa dijadikan unggulan"}
	}
	if err := s.repo.SetFeaturedVideo(ctx, requesterID, videoID, s.now()); err != nil {
		return nil, err
	}
	return &v, nil
}

// ---------- Oracle ----------

type channelTotalsRow struct {
	VideoCount int64 `gorm:"column:video_count"`
	TotalViews int64 `gorm:"column:total_views"`
	TotalLikes int64 `gorm:"column:total_likes"`
}

// ChannelSummary:
//
//	SELECT COUNT(*), NVL(SUM(view_count), 0), NVL(SUM(like_count), 0)
//	  FROM videos
//	 WHERE owner_id = ? AND status = 'READY' AND draft = 0
//	   AND visibility = 'PUBLIC' AND deleted_at IS NULL
//
// memakai index videos_owner_feed_ix (owner_id, visibility, draft, status,
// ...). Jumlah video satu kanal kecil, jadi menjumlah langsung lebih
// sederhana daripada menyimpan total terpisah yang harus selalu disamakan.
func (r *OracleRepository) ChannelSummary(ctx context.Context, ownerID int64) (ChannelCounts, int64, error) {
	var row channelTotalsRow
	err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) AS "video_count", NVL(SUM(view_count), 0) AS "total_views", `+
		`NVL(SUM(like_count), 0) AS "total_likes" FROM videos `+
		"WHERE owner_id = ? AND "+listedSQL+" AND deleted_at IS NULL", ownerID).Scan(&row).Error
	if err != nil {
		return ChannelCounts{}, 0, fmt.Errorf("select channel totals: %w", err)
	}
	var settings channelSettingsRow
	err = r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Take(&settings).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelCounts{}, 0, fmt.Errorf("select channel settings: %w", err)
	}
	var featured int64
	if settings.FeaturedVideoID != nil {
		featured = *settings.FeaturedVideoID
	}
	return ChannelCounts(row), featured, nil
}

// channelSettingsRow memetakan tabel channel_settings (migrasi V21).
type channelSettingsRow struct {
	OwnerID         int64     `gorm:"column:owner_id;primaryKey"`
	FeaturedVideoID *int64    `gorm:"column:featured_video_id"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (channelSettingsRow) TableName() string { return "channel_settings" }

const setFeaturedSQL = `MERGE INTO channel_settings c
USING (SELECT ? AS owner_id FROM dual) s
   ON (c.owner_id = s.owner_id)
 WHEN MATCHED THEN
   UPDATE SET c.featured_video_id = ?, c.updated_at = ?
 WHEN NOT MATCHED THEN
   INSERT (owner_id, featured_video_id, updated_at)
   VALUES (s.owner_id, ?, ?)`

// SetFeaturedVideo menyimpan pilihan dengan MERGE (buat baris jika belum
// ada). videoID 0 disimpan sebagai NULL.
func (r *OracleRepository) SetFeaturedVideo(ctx context.Context, ownerID, videoID int64, now time.Time) error {
	var featured *int64
	if videoID > 0 {
		featured = &videoID
	}
	if err := r.db.WithContext(ctx).Exec(setFeaturedSQL, ownerID, featured, now, featured, now).Error; err != nil {
		return fmt.Errorf("merge channel settings: %w", err)
	}
	return nil
}

// ---------- gRPC ----------

func (s *GRPCServer) GetChannelSummary(ctx context.Context, req *videov1.GetChannelSummaryRequest) (*videov1.GetChannelSummaryResponse, error) {
	sum, err := s.svc.GetChannelSummary(ctx, req.GetOwnerId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.GetChannelSummaryResponse{
		VideoCount: sum.Totals.VideoCount,
		TotalViews: sum.Totals.TotalViews,
		TotalLikes: sum.Totals.TotalLikes,
	}
	if sum.Featured != nil {
		resp.FeaturedVideo = s.toProtoVideo(*sum.Featured)
	}
	return resp, nil
}

func (s *GRPCServer) SetFeaturedVideo(ctx context.Context, req *videov1.SetFeaturedVideoRequest) (*videov1.SetFeaturedVideoResponse, error) {
	v, err := s.svc.SetFeaturedVideo(ctx, req.GetRequesterId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.SetFeaturedVideoResponse{}
	if v != nil {
		resp.FeaturedVideo = s.toProtoVideo(*v)
	}
	return resp, nil
}

func (s *GRPCServer) ListRecommendedVideos(ctx context.Context, req *videov1.ListRecommendedVideosRequest) (*videov1.ListRecommendedVideosResponse, error) {
	videos, err := s.svc.Recommend(ctx, RecommendInput{
		ViewerID:         req.GetViewerId(),
		FollowedOwnerIDs: req.GetFollowedOwnerIds(),
		Category:         req.GetCategory(),
		Seed:             req.GetSeed(),
		Limit:            int(req.GetLimit()),
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListRecommendedVideosResponse{Videos: make([]*videov1.Video, len(videos))}
	for i, v := range videos {
		resp.Videos[i] = s.toProtoVideo(v)
	}
	return resp, nil
}
