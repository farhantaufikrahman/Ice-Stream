package video

import (
	"context"
	"errors"
	"log/slog"

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
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.CreateUploadResponse{
		Video:              s.toProtoVideo(up.Video),
		UploadUrl:          up.Request.URL,
		UploadUrlExpiresAt: timestamppb.New(up.Request.ExpiresAt),
		UploadHeaders:      up.Request.Headers,
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
	resp := &videov1.GetVideoResponse{Video: s.toProtoVideo(d.Video)}
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
	videos, next, err := s.svc.ListVideos(ctx, req.GetOwnerId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListVideosResponse{NextPageToken: next}
	for _, v := range videos {
		resp.Videos = append(resp.Videos, s.toProtoVideo(v))
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

// toStatus memetakan error Service ke kode gRPC.
func (s *GRPCServer) toStatus(ctx context.Context, err error) error {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrUploadIncomplete), errors.Is(err, ErrNotUploading):
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
	pv.DurationMs = v.DurationMs
	pv.Renditions = v.Renditions
	pv.FailureReason = v.FailureReason
	if v.ProcessedAt != nil {
		pv.ProcessedAt = timestamppb.New(*v.ProcessedAt)
	}
	if v.Status == StatusReady {
		pv.HlsUrl = s.svc.MediaURL(v.PlaylistKey)
		pv.ThumbnailUrl = s.svc.MediaURL(v.ThumbnailKey)
	}
	return pv
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
