package transport

import (
	"context"
	"errors"
	"log/slog"
	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/example/order-platform/internal/metrics"
	"github.com/example/order-platform/internal/user/app"
	"github.com/example/order-platform/internal/user/domain"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type Server struct {
	userv1.UnimplementedUserServiceServer
	app *app.Service
}

func New(s *app.Service) *Server { return &Server{app: s} }
func (s *Server) Authenticate(ctx context.Context, r *userv1.AuthenticateRequest) (*userv1.AuthenticateResponse, error) {
	u, e := s.app.Authenticate(ctx, r.Login, r.Password)
	return &userv1.AuthenticateResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) GetUser(ctx context.Context, r *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.Get(ctx, parseID(r.Id))
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) GetUserByTelegramID(ctx context.Context, r *userv1.GetUserByTelegramIDRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.ByTelegram(ctx, r.TelegramId)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) ListUsers(ctx context.Context, r *userv1.ListUsersRequest) (*userv1.ListUsersResponse, error) {
	users, e := s.app.List(ctx, int(r.PageSize), r.PageToken)
	out := &userv1.ListUsersResponse{}
	for _, u := range users {
		out.Users = append(out.Users, toProto(u))
	}
	if len(users) > 0 {
		out.NextPageToken = users[len(users)-1].ID.String()
	}
	return out, grpcErr(e)
}
func (s *Server) CreateUser(ctx context.Context, r *userv1.CreateUserRequest) (*userv1.GetUserResponse, error) {
	role := domain.RolePerformer
	if r.Role == userv1.Role_ROLE_ADMIN {
		role = domain.RoleAdmin
	}
	u, e := s.app.Create(ctx, r.Login, r.Password, r.FullName, role)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) UpdateUser(ctx context.Context, r *userv1.UpdateUserRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.UpdateName(ctx, parseID(r.Id), r.FullName)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) SetUserPermissions(ctx context.Context, r *userv1.SetUserPermissionsRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.Permissions(ctx, parseID(r.Id), r.Permissions)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) DisableUser(ctx context.Context, r *userv1.UserIDRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.Active(ctx, parseID(r.Id), false)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) EnableUser(ctx context.Context, r *userv1.UserIDRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.Active(ctx, parseID(r.Id), true)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) ChangePassword(ctx context.Context, r *userv1.ChangePasswordRequest) (*userv1.Empty, error) {
	e := s.app.ChangePassword(ctx, parseID(r.Id), r.OldPassword, r.NewPassword)
	return &userv1.Empty{}, grpcErr(e)
}
func (s *Server) ResetPassword(ctx context.Context, r *userv1.ResetPasswordRequest) (*userv1.Empty, error) {
	e := s.app.ResetPassword(ctx, parseID(r.Id), r.NewPassword)
	return &userv1.Empty{}, grpcErr(e)
}
func (s *Server) BindTelegram(ctx context.Context, r *userv1.BindTelegramRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.BindTelegram(ctx, parseID(r.Id), r.TelegramId)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func (s *Server) CreateTelegramLink(ctx context.Context, r *userv1.CreateTelegramLinkRequest) (*userv1.CreateTelegramLinkResponse, error) {
	validity := time.Duration(r.ExpiresInSeconds) * time.Second
	token, expiresAt, e := s.app.CreateTelegramLink(ctx, parseID(r.UserId), validity)
	return &userv1.CreateTelegramLinkResponse{Token: token, ExpiresAt: timestamppb.New(expiresAt)}, grpcErr(e)
}
func (s *Server) RedeemTelegramLink(ctx context.Context, r *userv1.RedeemTelegramLinkRequest) (*userv1.GetUserResponse, error) {
	u, e := s.app.RedeemTelegramLink(ctx, r.Token, r.TelegramId)
	return &userv1.GetUserResponse{User: toProto(u)}, grpcErr(e)
}
func parseID(v string) uuid.UUID {
	id, e := uuid.Parse(v)
	if e != nil {
		return uuid.Nil
	}
	return id
}
func toProto(u domain.User) *userv1.User {
	if u.ID == uuid.Nil {
		return nil
	}
	role := userv1.Role_ROLE_PERFORMER
	if u.Role == domain.RoleAdmin {
		role = userv1.Role_ROLE_ADMIN
	}
	p := &userv1.User{Id: u.ID.String(), Login: u.Login, FullName: u.FullName, Role: role, IsActive: u.IsActive, Permissions: u.Permissions, CreatedAt: timestamppb.New(u.CreatedAt), UpdatedAt: timestamppb.New(u.UpdatedAt)}
	if u.TelegramID != nil {
		p.TelegramId = u.TelegramID
	}
	return p
}
func grpcErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, domain.ErrValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		slog.Error("user rpc failed", "error", err)
		return status.Error(codes.Internal, "internal error")
	}
}
func UnaryMetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		metrics.GRPCDuration.WithLabelValues(info.FullMethod).Observe(time.Since(started).Seconds())
		metrics.GRPCRequests.WithLabelValues(info.FullMethod, status.Code(err).String()).Inc()
		return resp, err
	}
}
