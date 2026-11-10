package comment

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commentv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/comment/v1"
)

// GRPCServer menerjemahkan panggilan gRPC menjadi panggilan ke Service.
type GRPCServer struct {
	commentv1.UnimplementedCommentServiceServer
	svc    *Service
	logger *slog.Logger
}

// NewGRPCServer membuat GRPCServer.
func NewGRPCServer(svc *Service, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, logger: logger}
}

func (s *GRPCServer) CreateComment(ctx context.Context, req *commentv1.CreateCommentRequest) (*commentv1.CreateCommentResponse, error) {
	c, err := s.svc.CreateComment(ctx, req.GetRequesterId(), req.GetVideoId(), req.GetParentId(), req.GetBody())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &commentv1.CreateCommentResponse{Comment: toProto(c)}, nil
}

func (s *GRPCServer) ListComments(ctx context.Context, req *commentv1.ListCommentsRequest) (*commentv1.ListCommentsResponse, error) {
	comments, next, err := s.svc.ListComments(ctx, req.GetVideoId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &commentv1.ListCommentsResponse{Comments: toProtoList(comments), NextPageToken: next}, nil
}

func (s *GRPCServer) ListReplies(ctx context.Context, req *commentv1.ListRepliesRequest) (*commentv1.ListRepliesResponse, error) {
	replies, next, err := s.svc.ListReplies(ctx, req.GetVideoId(), req.GetCommentId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &commentv1.ListRepliesResponse{Comments: toProtoList(replies), NextPageToken: next}, nil
}

func (s *GRPCServer) DeleteComment(ctx context.Context, req *commentv1.DeleteCommentRequest) (*commentv1.DeleteCommentResponse, error) {
	if err := s.svc.DeleteComment(ctx, req.GetRequesterId(), req.GetVideoId(), req.GetCommentId()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &commentv1.DeleteCommentResponse{}, nil
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
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request dibatalkan")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "waktu habis")
	default:
		s.logger.ErrorContext(ctx, "comment internal error", slog.Any("error", err))
		return status.Error(codes.Internal, "terjadi kesalahan internal")
	}
}

func toProto(c Comment) *commentv1.Comment {
	return &commentv1.Comment{
		Id:         c.ID,
		VideoId:    c.VideoID,
		AuthorId:   c.AuthorID,
		ParentId:   c.ParentID,
		Body:       c.Body,
		ReplyCount: c.ReplyCount,
		CreatedAt:  timestamppb.New(c.CreatedAt),
	}
}

func toProtoList(comments []Comment) []*commentv1.Comment {
	out := make([]*commentv1.Comment, len(comments))
	for i, c := range comments {
		out[i] = toProto(c)
	}
	return out
}
