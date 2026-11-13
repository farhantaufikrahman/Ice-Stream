package video

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// GRPCServer menerjemahkan panggilan gRPC menjadi panggilan ke Service.
type GRPCServer struct {
	videov1.UnimplementedVideoServiceServer
	svc    *Service
	logger *slog.Logger
}

// NewGRPCServer membuat GRPCServer.
func NewGRPCServer(svc *Service, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, logger: logger}
}

func (s *GRPCServer) CreateUpload(ctx context.Context, req *videov1.CreateUploadRequest) (*videov1.CreateUploadResponse, error) {
	up, err := s.svc.CreateUpload(ctx, CreateUploadInput{
		OwnerID:     req.GetRequesterId(),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
		ContentType: req.GetContentType(),
		SizeBytes:   req.GetSizeBytes(),
		Category:    req.GetCategory(),
		Tags:        req.GetTags(),
		Visibility:  fromProtoVideoVisibility(req.GetVisibility()),
		Draft:       req.GetDraft(),
		Language:    req.GetLanguage(),
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.CreateUploadResponse{
		Video:         s.toProtoVideo(up.Video),
		PartSizeBytes: up.PartSize,
		PartCount:     up.PartCount,
	}, nil
}

func (s *GRPCServer) PresignUploadParts(ctx context.Context, req *videov1.PresignUploadPartsRequest) (*videov1.PresignUploadPartsResponse, error) {
	out, err := s.svc.PresignUploadParts(ctx, req.GetRequesterId(), req.GetVideoId(), req.GetPartNumbers())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	parts := make([]*videov1.UploadPart, len(out.Parts))
	for i, p := range out.Parts {
		parts[i] = &videov1.UploadPart{PartNumber: p.Number, Url: p.Request.URL, SizeBytes: p.Size, Headers: p.Request.Headers}
	}
	return &videov1.PresignUploadPartsResponse{
		Parts:               parts,
		ExpiresAt:           timestamppb.New(out.ExpiresAt),
		UploadedPartNumbers: out.Uploaded,
	}, nil
}

func (s *GRPCServer) CompleteUpload(ctx context.Context, req *videov1.CompleteUploadRequest) (*videov1.CompleteUploadResponse, error) {
	v, err := s.svc.CompleteUpload(ctx, req.GetRequesterId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.CompleteUploadResponse{Video: s.toProtoVideo(v)}, nil
}

func (s *GRPCServer) GetVideo(ctx context.Context, req *videov1.GetVideoRequest) (*videov1.GetVideoResponse, error) {
	d, err := s.svc.GetVideo(ctx, req.GetViewerId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.GetVideoResponse{
		Video:            s.toProtoVideo(d.Video),
		ViewerLiked:      d.ViewerLiked,
		ResumePositionMs: d.ResumePositionMs,
		ViewerSaved:      d.ViewerSaved,
		ViewerInPlaylist: d.ViewerInPlaylist,
	}
	if d.Original.URL != "" {
		resp.OriginalUrl = d.Original.URL
		resp.OriginalUrlExpiresAt = timestamppb.New(d.Original.ExpiresAt)
	}
	return resp, nil
}

func (s *GRPCServer) ListMyVideos(ctx context.Context, req *videov1.ListMyVideosRequest) (*videov1.ListMyVideosResponse, error) {
	videos, next, err := s.svc.ListMyVideos(ctx, req.GetRequesterId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListMyVideosResponse{NextPageToken: next}
	for _, v := range videos {
		resp.Videos = append(resp.Videos, s.toProtoVideo(v))
	}
	return resp, nil
}

func (s *GRPCServer) ListVideos(ctx context.Context, req *videov1.ListVideosRequest) (*videov1.ListVideosResponse, error) {
	// owner_id (halaman channel) dan owner_ids (feed subscription) digabung.
	owners := req.GetOwnerIds()
	if req.GetOwnerId() > 0 {
		owners = append([]int64{req.GetOwnerId()}, owners...)
	}
	var (
		videos []Video
		next   string
		err    error
	)
	switch req.GetSort() {
	case videov1.VideoSort_VIDEO_SORT_UNSPECIFIED, videov1.VideoSort_VIDEO_SORT_NEWEST:
		filter := FeedFilter{OwnerIDs: owners, Category: req.GetCategory()}
		videos, next, err = s.svc.ListVideos(ctx, filter, int(req.GetPageSize()), req.GetPageToken())
	case videov1.VideoSort_VIDEO_SORT_POPULAR:
		// Index videos_owner_popular_ix hanya cocok untuk satu pemilik.
		if len(uniquePositive(owners)) != 1 || req.GetCategory() != "" {
			err = &ValidationError{"sort", "POPULAR hanya untuk satu owner_id, tanpa category"}
			break
		}
		videos, next, err = s.svc.ListPopular(ctx, owners[0], int(req.GetPageSize()), req.GetPageToken())
	default:
		err = &ValidationError{"sort", "tidak dikenal"}
	}
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListVideosResponse{NextPageToken: next}
	for _, v := range videos {
		resp.Videos = append(resp.Videos, s.toProtoVideo(v))
	}
	return resp, nil
}

func (s *GRPCServer) SearchVideos(ctx context.Context, req *videov1.SearchVideosRequest) (*videov1.SearchVideosResponse, error) {
	opts := SearchOptions{Sort: searchSorts[req.GetSort()], Duration: durationFilters[req.GetDuration()]}
	// Nilai enum yang tidak dikenal (client lebih baru dari server) ditolak,
	// bukan diam-diam dianggap "Paling relevan".
	if _, ok := searchSorts[req.GetSort()]; !ok {
		return nil, s.toStatus(ctx, &ValidationError{"sort", "tidak dikenal"})
	}
	if _, ok := durationFilters[req.GetDuration()]; !ok {
		return nil, s.toStatus(ctx, &ValidationError{"duration", "tidak dikenal"})
	}
	videos, next, err := s.svc.SearchVideos(ctx, req.GetQuery(), opts, int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.SearchVideosResponse{NextPageToken: next}
	for _, v := range videos {
		resp.Videos = append(resp.Videos, s.toProtoVideo(v))
	}
	// Jumlah hasil hanya di halaman pertama (langkah 27).
	if req.GetPageToken() == "" {
		if resp.TotalSize, err = s.svc.CountSearchVideos(ctx, req.GetQuery(), opts, len(videos), next != ""); err != nil {
			return nil, s.toStatus(ctx, err)
		}
	}
	return resp, nil
}

func (s *GRPCServer) RecordView(ctx context.Context, req *videov1.RecordViewRequest) (*videov1.RecordViewResponse, error) {
	r, err := s.svc.RecordView(ctx, req.GetVideoId(), req.GetViewerKey())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.RecordViewResponse{Counted: r.Counted, ViewCount: r.ViewCount}, nil
}

func (s *GRPCServer) LikeVideo(ctx context.Context, req *videov1.LikeVideoRequest) (*videov1.LikeVideoResponse, error) {
	r, err := s.svc.LikeVideo(ctx, req.GetRequesterId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.LikeVideoResponse{Liked: r.Liked, LikeCount: r.LikeCount}, nil
}

func (s *GRPCServer) UnlikeVideo(ctx context.Context, req *videov1.UnlikeVideoRequest) (*videov1.UnlikeVideoResponse, error) {
	r, err := s.svc.UnlikeVideo(ctx, req.GetRequesterId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.UnlikeVideoResponse{Liked: r.Liked, LikeCount: r.LikeCount}, nil
}

func (s *GRPCServer) SaveWatchProgress(ctx context.Context, req *videov1.SaveWatchProgressRequest) (*videov1.SaveWatchProgressResponse, error) {
	p, err := s.svc.SaveWatchProgress(ctx, req.GetRequesterId(), req.GetVideoId(), req.GetPositionMs())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.SaveWatchProgressResponse{PositionMs: p.PositionMs, WatchedAt: timestamppb.New(p.WatchedAt)}, nil
}

func (s *GRPCServer) ListWatchHistory(ctx context.Context, req *videov1.ListWatchHistoryRequest) (*videov1.ListWatchHistoryResponse, error) {
	entries, next, err := s.svc.ListWatchHistory(ctx, req.GetRequesterId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListWatchHistoryResponse{NextPageToken: next}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, &videov1.WatchHistoryEntry{
			Video:      s.toProtoVideo(e.Video),
			PositionMs: e.PositionMs,
			WatchedAt:  timestamppb.New(e.WatchedAt),
		})
	}
	return resp, nil
}

func (s *GRPCServer) DeleteWatchHistory(ctx context.Context, req *videov1.DeleteWatchHistoryRequest) (*videov1.DeleteWatchHistoryResponse, error) {
	if err := s.svc.DeleteWatchHistory(ctx, req.GetRequesterId(), req.GetVideoId()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.DeleteWatchHistoryResponse{}, nil
}

func (s *GRPCServer) ClearWatchHistory(ctx context.Context, req *videov1.ClearWatchHistoryRequest) (*videov1.ClearWatchHistoryResponse, error) {
	n, err := s.svc.ClearWatchHistory(ctx, req.GetRequesterId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.ClearWatchHistoryResponse{Deleted: n}, nil
}

func (s *GRPCServer) UpdateVideo(ctx context.Context, req *videov1.UpdateVideoRequest) (*videov1.UpdateVideoResponse, error) {
	in := UpdateInput{
		RequesterID: req.GetRequesterId(),
		VideoID:     req.GetVideoId(),
		// Field "optional" di proto menjadi pointer di Go: nil = tidak dikirim.
		Title:       req.Title,
		Description: req.Description,
		Category:    req.Category,
		// tags adalah message: nil = tidak dikirim (lihat TagList di proto).
		Tags:     req.GetTags().GetTags(),
		SetTags:  req.GetTags() != nil,
		Draft:    req.Draft,
		Language: req.Language,
	}
	if req.Visibility != nil {
		vis := fromProtoVideoVisibility(req.GetVisibility())
		in.Visibility = &vis
	}
	v, err := s.svc.UpdateVideo(ctx, in)
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.UpdateVideoResponse{Video: s.toProtoVideo(v)}, nil
}

// purgeTimeout membatasi waktu menghapus file video di storage.
const purgeTimeout = 30 * time.Second

func (s *GRPCServer) DeleteVideo(ctx context.Context, req *videov1.DeleteVideoRequest) (*videov1.DeleteVideoResponse, error) {
	v, err := s.svc.DeleteVideo(ctx, req.GetRequesterId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	// Video sudah terhapus dari database. Hapus file-nya sekarang dengan
	// context baru: walau client memutus koneksi, penghapusan file tetap
	// selesai. Kalau gagal (misalnya storage mati), cukup dicatat di log;
	// PurgeDeleted mencobanya lagi saat VideoService start berikutnya.
	purgeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), purgeTimeout)
	defer cancel()
	n, err := s.svc.PurgeMedia(purgeCtx, v)
	if err != nil {
		s.logger.WarnContext(ctx, "gagal menghapus file video, dicoba lagi saat start berikutnya",
			slog.Int64("video_id", v.ID), slog.Any("error", err))
	} else {
		s.logger.InfoContext(ctx, "video dihapus", slog.Int64("video_id", v.ID), slog.Int("file", n))
	}
	return &videov1.DeleteVideoResponse{}, nil
}

func (s *GRPCServer) ListNotifications(ctx context.Context, req *videov1.ListNotificationsRequest) (*videov1.ListNotificationsResponse, error) {
	items, next, err := s.svc.ListNotifications(ctx, req.GetRequesterId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListNotificationsResponse{NextPageToken: next}
	for _, n := range items {
		pn := &videov1.Notification{
			Id:        n.ID,
			Kind:      n.Kind,
			Video:     s.toProtoVideo(n.Video),
			CreatedAt: timestamppb.New(n.CreatedAt),
		}
		if n.ModerationReason != "" {
			pn.ModerationReason = toProtoReportReason(n.ModerationReason)
		}
		if n.ReadAt != nil {
			pn.ReadAt = timestamppb.New(*n.ReadAt)
		}
		resp.Notifications = append(resp.Notifications, pn)
	}
	return resp, nil
}

func (s *GRPCServer) CountUnreadNotifications(ctx context.Context, req *videov1.CountUnreadNotificationsRequest) (*videov1.CountUnreadNotificationsResponse, error) {
	n, err := s.svc.CountUnreadNotifications(ctx, req.GetRequesterId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.CountUnreadNotificationsResponse{UnreadCount: n}, nil
}

func (s *GRPCServer) MarkNotificationsRead(ctx context.Context, req *videov1.MarkNotificationsReadRequest) (*videov1.MarkNotificationsReadResponse, error) {
	r, err := s.svc.MarkNotificationsRead(ctx, req.GetRequesterId(), req.GetNotificationId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.MarkNotificationsReadResponse{Updated: r.Updated, UnreadCount: r.UnreadCount}, nil
}

// toStatus memetakan error Service ke kode gRPC.
func (s *GRPCServer) toStatus(ctx context.Context, err error) error {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrPlaylistNotFound), errors.Is(err, ErrFrameNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrUploadIncomplete), errors.Is(err, ErrNotUploading), errors.Is(err, ErrUploadNotFound):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ErrVideoBusy), errors.Is(err, ErrAlreadyBlocked), errors.Is(err, ErrNotBlocked):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ErrEventsUnavailable):
		// UNAVAILABLE = boleh dicoba lagi. Detail error NATS cukup di log.
		s.logger.WarnContext(ctx, "gagal mengirim event video", slog.Any("error", err))
		return status.Error(codes.Unavailable, ErrEventsUnavailable.Error())
	case errors.Is(err, ErrViewsUnavailable):
		s.logger.WarnContext(ctx, "gagal mencatat view", slog.Any("error", err))
		return status.Error(codes.Unavailable, ErrViewsUnavailable.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request dibatalkan")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "waktu habis")
	default:
		s.logger.ErrorContext(ctx, "video internal error", slog.Any("error", err))
		return status.Error(codes.Internal, "terjadi kesalahan internal")
	}
}

// toProtoVideo mengubah Video menjadi pesan gRPC. URL HLS dan thumbnail
// hanya diisi untuk video READY.
func (s *GRPCServer) toProtoVideo(v Video) *videov1.Video {
	pv := &videov1.Video{
		Id:          v.ID,
		OwnerId:     v.OwnerID,
		Title:       v.Title,
		Description: v.Description,
		Status:      toProtoStatus(v.Status),
		ContentType: v.ContentType,
		SizeBytes:   v.SizeBytes,
		CreatedAt:   timestamppb.New(v.CreatedAt),
		ViewCount:   v.ViewCount,
	}
	if v.UploadedAt != nil {
		pv.UploadedAt = timestamppb.New(*v.UploadedAt)
	}
	pv.CommentCount = v.CommentCount
	pv.LikeCount = v.LikeCount
	pv.DanmakuCount = v.DanmakuCount
	pv.DurationMs = v.DurationMs
	pv.Renditions = v.Renditions
	pv.FailureReason = v.FailureReason
	pv.Category = v.Category
	pv.Tags = v.Tags
	if v.ProcessedAt != nil {
		pv.ProcessedAt = timestamppb.New(*v.ProcessedAt)
	}
	pv.Visibility = toProtoVideoVisibility(v.Visibility)
	if m := v.Moderation; m != nil {
		pv.Moderation = &videov1.VideoModeration{
			Reason:    toProtoReportReason(m.Reason),
			Note:      m.Note,
			BlockedAt: timestamppb.New(m.BlockedAt),
		}
	}
	pv.Draft = v.Draft
	if v.PublishedAt != nil {
		pv.PublishedAt = timestamppb.New(*v.PublishedAt)
	}
	pv.Language = v.Language
	pv.CustomThumbnail = isCustomThumbnail(v, v.ThumbnailKey)
	if v.Status == StatusReady {
		pv.HlsUrl = s.svc.MediaURL(v.PlaylistKey)
		pv.ThumbnailUrl = s.svc.MediaURL(v.ThumbnailKey)
		// Pilihan bingkai hanya berguna bagi pemilik. Gateway hanya
		// meneruskannya di endpoint /me/videos (langkah 26).
		for _, ms := range v.ThumbnailFrames {
			key := FrameKey(v, ms)
			pv.ThumbnailFrames = append(pv.ThumbnailFrames, &videov1.ThumbnailFrame{
				PositionMs: ms,
				Url:        s.svc.MediaURL(key),
				Selected:   key == v.ThumbnailKey,
			})
		}
	} else if pv.CustomThumbnail {
		// Gambar kreator bisa diunggah sebelum video selesai diproses, jadi
		// halaman Upload bisa langsung menampilkannya.
		pv.ThumbnailUrl = s.svc.MediaURL(v.ThumbnailKey)
	}
	return pv
}

func toProtoVideoVisibility(v string) videov1.VideoVisibility {
	switch v {
	case VisibilityUnlisted:
		return videov1.VideoVisibility_VIDEO_VISIBILITY_UNLISTED
	case VisibilityPrivate:
		return videov1.VideoVisibility_VIDEO_VISIBILITY_PRIVATE
	case VisibilityBlocked:
		return videov1.VideoVisibility_VIDEO_VISIBILITY_BLOCKED
	default:
		return videov1.VideoVisibility_VIDEO_VISIBILITY_PUBLIC
	}
}

// fromProtoVideoVisibility: UNSPECIFIED menjadi "" (= PUBLIC, lihat
// cleanVideoVisibility). Angka yang tidak dikenal diteruskan apa adanya
// supaya ditolak validasi Service, bukan diam-diam menjadi PUBLIC.
func fromProtoVideoVisibility(v videov1.VideoVisibility) string {
	switch v {
	case videov1.VideoVisibility_VIDEO_VISIBILITY_UNSPECIFIED:
		return ""
	case videov1.VideoVisibility_VIDEO_VISIBILITY_PUBLIC:
		return VisibilityPublic
	case videov1.VideoVisibility_VIDEO_VISIBILITY_UNLISTED:
		return VisibilityUnlisted
	case videov1.VideoVisibility_VIDEO_VISIBILITY_PRIVATE:
		return VisibilityPrivate
	default:
		return v.String()
	}
}

func (s *GRPCServer) ChooseThumbnailFrame(ctx context.Context, req *videov1.ChooseThumbnailFrameRequest) (*videov1.ChooseThumbnailFrameResponse, error) {
	v, err := s.svc.ChooseThumbnailFrame(ctx, req.GetRequesterId(), req.GetVideoId(), req.GetPositionMs())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.ChooseThumbnailFrameResponse{Video: s.toProtoVideo(v)}, nil
}

func (s *GRPCServer) UploadThumbnail(ctx context.Context, req *videov1.UploadThumbnailRequest) (*videov1.UploadThumbnailResponse, error) {
	v, err := s.svc.UploadThumbnail(ctx, req.GetRequesterId(), req.GetVideoId(), req.GetImage())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.UploadThumbnailResponse{Video: s.toProtoVideo(v)}, nil
}

func toProtoStatus(s string) videov1.VideoStatus {
	switch s {
	case StatusPendingUpload:
		return videov1.VideoStatus_VIDEO_STATUS_PENDING_UPLOAD
	case StatusUploaded:
		return videov1.VideoStatus_VIDEO_STATUS_UPLOADED
	case StatusProcessing:
		return videov1.VideoStatus_VIDEO_STATUS_PROCESSING
	case StatusReady:
		return videov1.VideoStatus_VIDEO_STATUS_READY
	case StatusFailed:
		return videov1.VideoStatus_VIDEO_STATUS_FAILED
	}
	return videov1.VideoStatus_VIDEO_STATUS_UNSPECIFIED
}

func (s *GRPCServer) AddToWatchLater(ctx context.Context, req *videov1.AddToWatchLaterRequest) (*videov1.AddToWatchLaterResponse, error) {
	addedAt, err := s.svc.AddToWatchLater(ctx, req.GetRequesterId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.AddToWatchLaterResponse{AddedAt: timestamppb.New(addedAt)}, nil
}

func (s *GRPCServer) RemoveFromWatchLater(ctx context.Context, req *videov1.RemoveFromWatchLaterRequest) (*videov1.RemoveFromWatchLaterResponse, error) {
	if err := s.svc.RemoveFromWatchLater(ctx, req.GetRequesterId(), req.GetVideoId()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.RemoveFromWatchLaterResponse{}, nil
}

func (s *GRPCServer) ListWatchLater(ctx context.Context, req *videov1.ListWatchLaterRequest) (*videov1.ListWatchLaterResponse, error) {
	entries, next, err := s.svc.ListWatchLater(ctx, req.GetRequesterId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListWatchLaterResponse{NextPageToken: next}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, &videov1.WatchLaterEntry{
			Video:   s.toProtoVideo(e.Video),
			AddedAt: timestamppb.New(e.AddedAt),
		})
	}
	return resp, nil
}

var searchSorts = map[videov1.SearchSort]SearchSort{
	videov1.SearchSort_SEARCH_SORT_UNSPECIFIED:  SortRelevance,
	videov1.SearchSort_SEARCH_SORT_RELEVANCE:    SortRelevance,
	videov1.SearchSort_SEARCH_SORT_NEWEST:       SortNewest,
	videov1.SearchSort_SEARCH_SORT_MOST_VIEWED:  SortMostViewed,
	videov1.SearchSort_SEARCH_SORT_MOST_DANMAKU: SortMostDanmaku,
}

var durationFilters = map[videov1.DurationFilter]DurationFilter{
	videov1.DurationFilter_DURATION_FILTER_UNSPECIFIED: DurationAny,
	videov1.DurationFilter_DURATION_FILTER_SHORT:       DurationShort,
	videov1.DurationFilter_DURATION_FILTER_MEDIUM:      DurationMedium,
	videov1.DurationFilter_DURATION_FILTER_LONG:        DurationLong,
}

func (s *GRPCServer) ListTrendingVideos(ctx context.Context, req *videov1.ListTrendingVideosRequest) (*videov1.ListTrendingVideosResponse, error) {
	trending, err := s.svc.ListTrending(ctx, req.GetCategory(), int(req.GetLimit()))
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListTrendingVideosResponse{Videos: make([]*videov1.TrendingVideo, len(trending))}
	for i, t := range trending {
		resp.Videos[i] = &videov1.TrendingVideo{Video: s.toProtoVideo(t.Video), RecentViews: t.RecentViews}
	}
	return resp, nil
}

// reportReasons memetakan enum gRPC ke alasan di database.
var reportReasons = map[videov1.ReportReason]ReportReason{
	videov1.ReportReason_REPORT_REASON_SPAM:       ReportSpam,
	videov1.ReportReason_REPORT_REASON_SEXUAL:     ReportSexual,
	videov1.ReportReason_REPORT_REASON_VIOLENCE:   ReportViolence,
	videov1.ReportReason_REPORT_REASON_HATE:       ReportHate,
	videov1.ReportReason_REPORT_REASON_HARASSMENT: ReportHarassment,
	videov1.ReportReason_REPORT_REASON_MISLEADING: ReportMisleading,
	videov1.ReportReason_REPORT_REASON_COPYRIGHT:  ReportCopyright,
	videov1.ReportReason_REPORT_REASON_OTHER:      ReportOther,
}

// toProtoReportReason adalah kebalikan reportReasons.
func toProtoReportReason(r ReportReason) videov1.ReportReason {
	for k, v := range reportReasons {
		if v == r {
			return k
		}
	}
	return videov1.ReportReason_REPORT_REASON_UNSPECIFIED
}

func (s *GRPCServer) ReportVideo(ctx context.Context, req *videov1.ReportVideoRequest) (*videov1.ReportVideoResponse, error) {
	// Nilai yang tidak ada di map (termasuk UNSPECIFIED) menjadi "" dan
	// ditolak oleh ReportVideo sebagai alasan tidak dikenal.
	reason := reportReasons[req.GetReason()]
	at, err := s.svc.ReportVideo(ctx, req.GetRequesterId(), req.GetVideoId(), reason, req.GetDetails())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.ReportVideoResponse{ReportedAt: timestamppb.New(at)}, nil
}
