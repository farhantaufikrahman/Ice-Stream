package auth

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
)

// GRPCServer menerjemahkan panggilan gRPC menjadi panggilan ke Service.
// Mirip "controller" di Express/Spring: tidak ada aturan bisnis di sini.
type GRPCServer struct {
	authv1.UnimplementedAuthServiceServer
	svc    *Service
	logger *slog.Logger
}

// NewGRPCServer membuat GRPCServer.
func NewGRPCServer(svc *Service, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, logger: logger}
}

func (s *GRPCServer) Register(ctx context.Context, req *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	u, err := s.svc.Register(ctx, RegisterInput{
		Username:    req.GetUsername(),
		Email:       req.GetEmail(),
		Password:    req.GetPassword(),
		DisplayName: req.GetDisplayName(),
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &authv1.RegisterResponse{User: toProtoUser(u)}, nil
}

func (s *GRPCServer) Login(ctx context.Context, req *authv1.LoginRequest) (*authv1.LoginResponse, error) {
	u, pair, err := s.svc.Login(ctx, req.GetLogin(), req.GetPassword(), clientInfo(req.GetClient()))
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &authv1.LoginResponse{User: toProtoUser(u), Tokens: toProtoTokens(pair)}, nil
}

func (s *GRPCServer) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.RefreshTokenResponse, error) {
	pair, err := s.svc.Refresh(ctx, req.GetRefreshToken(), clientInfo(req.GetClient()))
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &authv1.RefreshTokenResponse{Tokens: toProtoTokens(pair)}, nil
}

func (s *GRPCServer) Logout(ctx context.Context, req *authv1.LogoutRequest) (*authv1.LogoutResponse, error) {
	if err := s.svc.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &authv1.LogoutResponse{}, nil
}

// toStatus memetakan error Service ke kode gRPC. Kode gRPC ini nanti
// diterjemahkan gateway menjadi kode HTTP (misalnya ALREADY_EXISTS -> 409).
func (s *GRPCServer) toStatus(ctx context.Context, err error) error {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		return status.Error(codes.InvalidArgument, verr.Error())
	case errors.Is(err, ErrUsernameTaken), errors.Is(err, ErrEmailTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidRefreshToken):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, ErrUserBanned):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request dibatalkan")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "waktu habis")
	default:
		// Detail error internal (misalnya pesan Oracle) hanya masuk log,
		// tidak dikirim ke pemanggil.
		s.logger.ErrorContext(ctx, "auth internal error", slog.Any("error", err))
		return status.Error(codes.Internal, "terjadi kesalahan internal")
	}
}

func clientInfo(c *authv1.ClientInfo) ClientInfo {
	return ClientInfo{UserAgent: c.GetUserAgent(), IPAddress: c.GetIpAddress()}
}

func toProtoUser(u User) *authv1.User {
	return &authv1.User{
		Id:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		CreatedAt:   timestamppb.New(u.CreatedAt),
	}
}

func toProtoTokens(p TokenPair) *authv1.TokenPair {
	return &authv1.TokenPair{
		AccessToken:           p.AccessToken,
		AccessTokenExpiresAt:  timestamppb.New(p.AccessTokenExpiresAt),
		RefreshToken:          p.RefreshToken,
		RefreshTokenExpiresAt: timestamppb.New(p.RefreshTokenExpiresAt),
	}
}
