package livestream

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	livev1 "github.com/farhantaufikrahman/ice-stream/backend/gen/live/v1"
)

// GRPCServer menerjemahkan panggilan gRPC menjadi panggilan ke Service.
type GRPCServer struct {
	livev1.UnimplementedLiveServiceServer
	svc    *Service
	logger *slog.Logger
}

// NewGRPCServer membuat GRPCServer.
func NewGRPCServer(svc *Service, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, logger: logger}
}

func (s *GRPCServer) GetMyChannel(ctx context.Context, req *livev1.GetMyChannelRequest) (*livev1.GetMyChannelResponse, error) {
	if req.GetRequesterId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "requester_id wajib diisi")
	}
	v, err := s.svc.GetMyChannel(ctx, req.GetRequesterId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &livev1.GetMyChannelResponse{Channel: s.channelProto(v)}, nil
}

func (s *GRPCServer) UpdateMyChannel(ctx context.Context, req *livev1.UpdateMyChannelRequest) (*livev1.UpdateMyChannelResponse, error) {
	if req.GetRequesterId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "requester_id wajib diisi")
	}
	v, err := s.svc.UpdateMyChannel(ctx, req.GetRequesterId(), req.GetTitle(), req.GetCategory())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &livev1.UpdateMyChannelResponse{Channel: s.channelProto(v)}, nil
}

func (s *GRPCServer) ResetStreamKey(ctx context.Context, req *livev1.ResetStreamKeyRequest) (*livev1.ResetStreamKeyResponse, error) {
	if req.GetRequesterId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "requester_id wajib diisi")
	}
	v, key, err := s.svc.ResetStreamKey(ctx, req.GetRequesterId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &livev1.ResetStreamKeyResponse{Channel: s.channelProto(v), StreamKey: key}, nil
}

func (s *GRPCServer) ListLiveStreams(ctx context.Context, req *livev1.ListLiveStreamsRequest) (*livev1.ListLiveStreamsResponse, error) {
	streams, err := s.svc.ListLive(ctx, ListFilter{Category: req.GetCategory(), OwnerIDs: req.GetOwnerIds(), Limit: int(req.GetLimit())})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &livev1.ListLiveStreamsResponse{Streams: make([]*livev1.LiveStream, len(streams))}
	for i, st := range streams {
		resp.Streams[i] = s.streamProto(st)
	}
	return resp, nil
}

func (s *GRPCServer) GetLiveStream(ctx context.Context, req *livev1.GetLiveStreamRequest) (*livev1.GetLiveStreamResponse, error) {
	st, err := s.svc.GetStream(ctx, req.GetStreamId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &livev1.GetLiveStreamResponse{Stream: s.streamProto(st)}, nil
}

// toStatus memetakan error Service ke kode gRPC.
func (s *GRPCServer) toStatus(ctx context.Context, err error) error {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request dibatalkan")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "waktu habis")
	default:
		s.logger.ErrorContext(ctx, "live internal error", slog.Any("error", err))
		return status.Error(codes.Internal, "terjadi kesalahan internal")
	}
}

func (s *GRPCServer) channelProto(v ChannelView) *livev1.LiveChannel {
	out := &livev1.LiveChannel{
		OwnerId:      v.OwnerID,
		Title:        v.Title,
		Category:     v.Category,
		IngestUrl:    v.IngestURL,
		HasStreamKey: v.StreamKeyHash != "",
	}
	if !v.StreamKeyCreatedAt.IsZero() {
		out.StreamKeyCreatedAt = timestamppb.New(v.StreamKeyCreatedAt)
	}
	if v.Current != nil {
		out.Current = s.streamProto(*v.Current)
	}
	return out
}

var statusToProto = map[Status]livev1.LiveStatus{
	StatusLive:  livev1.LiveStatus_LIVE_STATUS_LIVE,
	StatusEnded: livev1.LiveStatus_LIVE_STATUS_ENDED,
}

func (s *GRPCServer) streamProto(st Stream) *livev1.LiveStream {
	out := &livev1.LiveStream{
		Id:          st.ID,
		OwnerId:     st.OwnerID,
		Title:       st.Title,
		Category:    st.Category,
		Status:      statusToProto[st.Status],
		StartedAt:   timestamppb.New(st.StartedAt),
		PlaybackUrl: s.svc.PlaybackURL(st),
		ViewerCount: st.ViewerCount,
		PeakViewers: st.PeakViewers,
	}
	if !st.EndedAt.IsZero() {
		out.EndedAt = timestamppb.New(st.EndedAt)
	}
	return out
}
