package main

import (
	"context"
	"errors"
	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/example/order-platform/internal/auth"
	"github.com/example/order-platform/internal/config"
	"github.com/example/order-platform/internal/metrics"
	"github.com/example/order-platform/internal/user/app"
	"github.com/example/order-platform/internal/user/repository"
	"github.com/example/order-platform/internal/user/transport"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "user-service")
	cfg := config.User()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseDSN)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		logger.Error("database ping failed", "error", err)
		os.Exit(1)
	}
	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		logger.Error("grpc listen failed", "error", err)
		os.Exit(1)
	}
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(transport.UnaryMetricsInterceptor(), auth.UnaryInterceptor(cfg.JWTSecret, map[string]bool{"/user.v1.UserService/Authenticate": true})))
	userv1.RegisterUserServiceServer(grpcServer, transport.New(app.New(repository.New(pool))))
	go func() {
		if err := metrics.Serve(ctx, ":"+cfg.MetricsPort); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("metrics server stopped", "error", err)
		}
	}()
	go func() {
		logger.Info("grpc server started", "port", cfg.GRPCPort)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("grpc server stopped", "error", err)
		}
	}()
	<-ctx.Done()
	done := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		grpcServer.Stop()
	}
}
