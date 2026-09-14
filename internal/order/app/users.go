package app

import (
	"context"
	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

type GRPCUsers struct{ client userv1.UserServiceClient }

func NewGRPCUsers(c userv1.UserServiceClient) *GRPCUsers { return &GRPCUsers{c} }
func (g *GRPCUsers) IsActivePerformer(ctx context.Context, id uuid.UUID) bool {
	md, _ := metadata.FromIncomingContext(ctx)
	ctx = metadata.NewOutgoingContext(ctx, md)
	u, e := g.client.GetUser(ctx, &userv1.GetUserRequest{Id: id.String()})
	return e == nil && u.User.GetIsActive() && u.User.GetRole() == userv1.Role_ROLE_PERFORMER
}
