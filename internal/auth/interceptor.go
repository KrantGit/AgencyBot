package auth

import (
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"strings"
)

type Actor struct {
	UserID, Role string
	Permissions  []string
}
type actorKey struct{}

func ActorFromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

type Claims struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	jwt.RegisteredClaims
}

func UnaryInterceptor(secret string, unauthenticatedMethods map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if unauthenticatedMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "authorization metadata is required")
		}
		values := md.Get("authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "bearer token is required")
		}
		claims := new(Claims)
		_, err := jwt.ParseWithClaims(strings.TrimPrefix(values[0], "Bearer "), claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		})
		if err != nil || claims.Subject == "" {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return handler(context.WithValue(ctx, actorKey{}, Actor{UserID: claims.Subject, Role: claims.Role, Permissions: claims.Permissions}), req)
	}
}
