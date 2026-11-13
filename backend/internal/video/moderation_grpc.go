package video

import (
	"context"
	"log/slog"

	"google.golang.org/protobuf/types/known/timestamppb"

	moderationv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/moderation/v1"
)

// ModerationGRPCServer menjalankan ModerationService (langkah 28) di atas
// Service yang sama dengan VideoService. Konversi Video dan error memakai
// GRPCServer supaya hasilnya sama persis dengan RPC video lain.
type ModerationGRPCServer struct {
	moderationv1.UnimplementedModerationServiceServer
	svc   *Service
	video *GRPCServer
}

// NewModerationGRPCServer membuat ModerationGRPCServer.
func NewModerationGRPCServer(svc *Service, logger *slog.Logger) *ModerationGRPCServer {
	return &ModerationGRPCServer{svc: svc, video: NewGRPCServer(svc, logger)}
}

func (s *ModerationGRPCServer) ListReportedVideos(ctx context.Context, req *moderationv1.ListReportedVideosRequest) (*moderationv1.ListReportedVideosResponse, error) {
	items, next, err := s.svc.ListReportedVideos(ctx, int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.video.toStatus(ctx, err)
	}
	resp := &moderationv1.ListReportedVideosResponse{
		Videos:        make([]*moderationv1.ReportedVideo, len(items)),
		NextPageToken: next,
	}
	for i, it := range items {
		rv := &moderationv1.ReportedVideo{
			Video:           s.video.toProtoVideo(it.Video),
			OpenReports:     it.OpenReports,
			FirstReportedAt: timestamppb.New(it.FirstReportedAt),
			LastReportedAt:  timestamppb.New(it.LastReportedAt),
		}
		for _, rc := range it.Reasons {
			rv.Reasons = append(rv.Reasons, &moderationv1.ReasonCount{Reason: toProtoReportReason(rc.Reason), Count: rc.Count})
		}
		resp.Videos[i] = rv
	}
	return resp, nil
}

func (s *ModerationGRPCServer) ListVideoReports(ctx context.Context, req *moderationv1.ListVideoReportsRequest) (*moderationv1.ListVideoReportsResponse, error) {
	reports, err := s.svc.ListVideoReports(ctx, req.GetVideoId())
	if err != nil {
		return nil, s.video.toStatus(ctx, err)
	}
	resp := &moderationv1.ListVideoReportsResponse{Reports: make([]*moderationv1.VideoReport, len(reports))}
	for i, r := range reports {
		resp.Reports[i] = &moderationv1.VideoReport{
			ReporterId: r.ReporterID,
			Reason:     toProtoReportReason(r.Reason),
			Details:    r.Details,
			ReportedAt: timestamppb.New(r.ReportedAt),
		}
	}
	return resp, nil
}

func (s *ModerationGRPCServer) BlockVideo(ctx context.Context, req *moderationv1.BlockVideoRequest) (*moderationv1.BlockVideoResponse, error) {
	// Enum yang tidak dikenal menjadi "" dan ditolak BlockVideo.
	blocked, closed, err := s.svc.BlockVideo(ctx, BlockInput{
		ModeratorID: req.GetModeratorId(),
		VideoID:     req.GetVideoId(),
		Reason:      reportReasons[req.GetReason()],
		Note:        req.GetNote(),
	})
	if err != nil {
		return nil, s.video.toStatus(ctx, err)
	}
	return &moderationv1.BlockVideoResponse{Blocked: s.toProtoBlocked(blocked), ClosedReports: closed}, nil
}

func (s *ModerationGRPCServer) DismissReports(ctx context.Context, req *moderationv1.DismissReportsRequest) (*moderationv1.DismissReportsResponse, error) {
	closed, err := s.svc.DismissReports(ctx, req.GetModeratorId(), req.GetVideoId())
	if err != nil {
		return nil, s.video.toStatus(ctx, err)
	}
	return &moderationv1.DismissReportsResponse{ClosedReports: closed}, nil
}

func (s *ModerationGRPCServer) UnblockVideo(ctx context.Context, req *moderationv1.UnblockVideoRequest) (*moderationv1.UnblockVideoResponse, error) {
	v, err := s.svc.UnblockVideo(ctx, req.GetModeratorId(), req.GetVideoId())
	if err != nil {
		return nil, s.video.toStatus(ctx, err)
	}
	return &moderationv1.UnblockVideoResponse{Video: s.video.toProtoVideo(v)}, nil
}

func (s *ModerationGRPCServer) ListBlockedVideos(ctx context.Context, req *moderationv1.ListBlockedVideosRequest) (*moderationv1.ListBlockedVideosResponse, error) {
	items, next, err := s.svc.ListBlockedVideos(ctx, int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.video.toStatus(ctx, err)
	}
	resp := &moderationv1.ListBlockedVideosResponse{
		Videos:        make([]*moderationv1.BlockedVideo, len(items)),
		NextPageToken: next,
	}
	for i, it := range items {
		resp.Videos[i] = s.toProtoBlocked(it)
	}
	return resp, nil
}

func (s *ModerationGRPCServer) toProtoBlocked(b BlockedVideo) *moderationv1.BlockedVideo {
	return &moderationv1.BlockedVideo{
		Video:       s.video.toProtoVideo(b.Video),
		Reason:      toProtoReportReason(b.Moderation.Reason),
		Note:        b.Moderation.Note,
		ModeratorId: b.Moderation.ModeratorID,
		BlockedAt:   timestamppb.New(b.Moderation.BlockedAt),
	}
}
