package danmaku

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	danmakuv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/danmaku/v1"
)

// GRPCServer menerjemahkan panggilan gRPC menjadi panggilan ke Service.
type GRPCServer struct {
	danmakuv1.UnimplementedDanmakuServiceServer
	svc    *Service
	logger *slog.Logger
}

// NewGRPCServer membuat GRPCServer.
func NewGRPCServer(svc *Service, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, logger: logger}
}

func (s *GRPCServer) SendDanmaku(ctx context.Context, req *danmakuv1.SendDanmakuRequest) (*danmakuv1.SendDanmakuResponse, error) {
	mode, ok := modeFromProto(req.GetMode())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "mode: tidak dikenal")
	}
	d, err := s.svc.Send(ctx, SendInput{
		RequesterID: req.GetRequesterId(),
		VideoID:     req.GetVideoId(),
		PositionMs:  req.GetPositionMs(),
		Body:        req.GetBody(),
		// req.Color bertipe *uint32 karena field-nya "optional": nil = tidak dikirim.
		Color: req.Color,
		Mode:  mode,
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &danmakuv1.SendDanmakuResponse{Danmaku: toProto(d)}, nil
}

func (s *GRPCServer) ListDanmaku(ctx context.Context, req *danmakuv1.ListDanmakuRequest) (*danmakuv1.ListDanmakuResponse, error) {
	items, next, err := s.svc.List(ctx, req.GetVideoId(), req.GetFromMs(), req.GetToMs(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &danmakuv1.ListDanmakuResponse{Danmaku: make([]*danmakuv1.Danmaku, len(items)), NextPageToken: next}
	for i, d := range items {
		resp.Danmaku[i] = toProto(d)
	}
	return resp, nil
}

func (s *GRPCServer) ListChannelDanmaku(ctx context.Context, req *danmakuv1.ListChannelDanmakuRequest) (*danmakuv1.ListChannelDanmakuResponse, error) {
	items, next, err := s.svc.ListChannelDanmaku(ctx, req.GetRequesterId(), req.GetVideoId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &danmakuv1.ListChannelDanmakuResponse{
		Danmaku:       make([]*danmakuv1.ChannelDanmaku, len(items)),
		NextPageToken: next,
	}
	for i, d := range items {
		resp.Danmaku[i] = &danmakuv1.ChannelDanmaku{Danmaku: toProto(d.Danmaku), VideoTitle: d.VideoTitle}
	}
	return resp, nil
}

func (s *GRPCServer) DeleteDanmaku(ctx context.Context, req *danmakuv1.DeleteDanmakuRequest) (*danmakuv1.DeleteDanmakuResponse, error) {
	if err := s.svc.DeleteDanmaku(ctx, req.GetRequesterId(), req.GetDanmakuId()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &danmakuv1.DeleteDanmakuResponse{}, nil
}

// toStatus memetakan error Service ke kode gRPC.
func (s *GRPCServer) toStatus(ctx context.Context, err error) error {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, ErrVideoNotFound), errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, ErrRateLimited):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request dibatalkan")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "waktu habis")
	default:
		s.logger.ErrorContext(ctx, "danmaku internal error", slog.Any("error", err))
		return status.Error(codes.Internal, "terjadi kesalahan internal")
	}
}

var modeToProto = map[Mode]danmakuv1.DanmakuMode{
	ModeScroll: danmakuv1.DanmakuMode_DANMAKU_MODE_SCROLL,
	ModeTop:    danmakuv1.DanmakuMode_DANMAKU_MODE_TOP,
	ModeBottom: danmakuv1.DanmakuMode_DANMAKU_MODE_BOTTOM,
}

// modeFromProto: UNSPECIFIED menjadi "" (Service mengisinya SCROLL).
func modeFromProto(m danmakuv1.DanmakuMode) (Mode, bool) {
	if m == danmakuv1.DanmakuMode_DANMAKU_MODE_UNSPECIFIED {
		return "", true
	}
	for mode, pm := range modeToProto {
		if pm == m {
			return mode, true
		}
	}
	return "", false
}

func toProto(d Danmaku) *danmakuv1.Danmaku {
	return &danmakuv1.Danmaku{
		Id:         d.ID,
		VideoId:    d.VideoID,
		UserId:     d.UserID,
		PositionMs: d.PositionMs,
		Body:       d.Body,
		Color:      d.Color,
		Mode:       modeToProto[d.Mode],
		CreatedAt:  timestamppb.New(d.CreatedAt),
	}
}
