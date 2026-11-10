package user

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// GRPCServer menerjemahkan panggilan gRPC menjadi panggilan ke Service.
type GRPCServer struct {
	userv1.UnimplementedUserServiceServer
	svc    *Service
	logger *slog.Logger
}

// NewGRPCServer membuat GRPCServer.
func NewGRPCServer(svc *Service, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, logger: logger}
}

func (s *GRPCServer) GetProfile(ctx context.Context, req *userv1.GetProfileRequest) (*userv1.GetProfileResponse, error) {
	p, err := s.svc.GetProfile(ctx, req.GetViewerId(), req.GetUserId(), req.GetUsername())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	subscribed, err := s.svc.ViewerSubscribed(ctx, req.GetViewerId(), p.ID)
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.GetProfileResponse{Profile: s.toProto(p), ViewerSubscribed: subscribed}, nil
}

func (s *GRPCServer) BatchGetProfiles(ctx context.Context, req *userv1.BatchGetProfilesRequest) (*userv1.BatchGetProfilesResponse, error) {
	profiles, err := s.svc.BatchGetProfiles(ctx, req.GetUserIds())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &userv1.BatchGetProfilesResponse{}
	for _, p := range profiles {
		resp.Profiles = append(resp.Profiles, s.toProto(p))
	}
	return resp, nil
}

func (s *GRPCServer) UpdateProfile(ctx context.Context, req *userv1.UpdateProfileRequest) (*userv1.UpdateProfileResponse, error) {
	// req.DisplayName bertipe *string karena field-nya "optional" di proto:
	// nil berarti tidak dikirim.
	p, err := s.svc.UpdateProfile(ctx, req.GetRequesterId(), ProfileUpdate{DisplayName: req.DisplayName, Bio: req.Bio})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.UpdateProfileResponse{Profile: s.toProto(p)}, nil
}

func (s *GRPCServer) UploadAvatar(ctx context.Context, req *userv1.UploadAvatarRequest) (*userv1.UploadAvatarResponse, error) {
	p, err := s.svc.UploadAvatar(ctx, req.GetRequesterId(), req.GetImage())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.UploadAvatarResponse{Profile: s.toProto(p)}, nil
}

func (s *GRPCServer) RemoveAvatar(ctx context.Context, req *userv1.RemoveAvatarRequest) (*userv1.RemoveAvatarResponse, error) {
	p, err := s.svc.RemoveAvatar(ctx, req.GetRequesterId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.RemoveAvatarResponse{Profile: s.toProto(p)}, nil
}

func (s *GRPCServer) Subscribe(ctx context.Context, req *userv1.SubscribeRequest) (*userv1.SubscribeResponse, error) {
	r, err := s.svc.Subscribe(ctx, req.GetRequesterId(), req.GetChannelId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.SubscribeResponse{Subscribed: r.Subscribed, SubscriberCount: r.SubscriberCount}, nil
}

func (s *GRPCServer) Unsubscribe(ctx context.Context, req *userv1.UnsubscribeRequest) (*userv1.UnsubscribeResponse, error) {
	r, err := s.svc.Unsubscribe(ctx, req.GetRequesterId(), req.GetChannelId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.UnsubscribeResponse{Subscribed: r.Subscribed, SubscriberCount: r.SubscriberCount}, nil
}

func (s *GRPCServer) ListSubscriptions(ctx context.Context, req *userv1.ListSubscriptionsRequest) (*userv1.ListSubscriptionsResponse, error) {
	channels, next, err := s.svc.ListSubscriptions(ctx, req.GetRequesterId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &userv1.ListSubscriptionsResponse{NextPageToken: next}
	for _, p := range channels {
		resp.Channels = append(resp.Channels, s.toProto(p))
	}
	return resp, nil
}

func (s *GRPCServer) ListSubscribedChannelIds(ctx context.Context, req *userv1.ListSubscribedChannelIdsRequest) (*userv1.ListSubscribedChannelIdsResponse, error) {
	ids, err := s.svc.ListSubscribedChannelIDs(ctx, req.GetRequesterId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.ListSubscribedChannelIdsResponse{ChannelIds: ids}, nil
}

func (s *GRPCServer) ListSubscriberIds(ctx context.Context, req *userv1.ListSubscriberIdsRequest) (*userv1.ListSubscriberIdsResponse, error) {
	ids, err := s.svc.ListSubscriberIDs(ctx, req.GetChannelId(), req.GetAfterSubscriberId(), int(req.GetPageSize()))
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.ListSubscriberIdsResponse{SubscriberIds: ids}, nil
}

// toStatus memetakan error Service ke kode gRPC.
func (s *GRPCServer) toStatus(ctx context.Context, err error) error {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrTooManySubscriptions):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request dibatalkan")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "waktu habis")
	default:
		s.logger.ErrorContext(ctx, "user internal error", slog.Any("error", err))
		return status.Error(codes.Internal, "terjadi kesalahan internal")
	}
}

func (s *GRPCServer) toProto(p Profile) *userv1.Profile {
	return &userv1.Profile{
		Id:          p.ID,
		Username:    p.Username,
		DisplayName: p.DisplayName,
		Bio:         p.Bio,
		AvatarUrl:   s.svc.AvatarURL(p.AvatarKey),
		CreatedAt:   timestamppb.New(p.CreatedAt),
		Email:       p.Email,
		// Jumlah subscriber selalu publik, seperti di YouTube dan Bilibili.
		SubscriberCount: p.SubscriberCount,
	}
}
