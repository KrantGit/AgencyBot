package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	orderv1 "github.com/example/order-platform/gen/go/order/v1"
	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/example/order-platform/internal/auth"
	"github.com/example/order-platform/internal/config"
	"github.com/example/order-platform/internal/metrics"
	"github.com/example/order-platform/internal/order/app"
	"github.com/example/order-platform/internal/order/repository"
	"github.com/example/order-platform/internal/order/transport"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "order-service")
	cfg := config.Order()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseDSN)
	if err != nil || pool.Ping(ctx) != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	userConn, err := grpc.NewClient(cfg.UserGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("user service connection failed", "error", err)
		os.Exit(1)
	}
	defer userConn.Close()
	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		logger.Error("listen failed", "error", err)
		os.Exit(1)
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(transport.UnaryMetricsInterceptor(), auth.UnaryInterceptor(cfg.JWTSecret, nil)))
	orderv1.RegisterOrderServiceServer(server, transport.New(app.New(repository.New(pool), app.NewGRPCUsers(userv1.NewUserServiceClient(userConn)))))
	go func() {
		if err := metrics.Serve(ctx, ":"+cfg.MetricsPort); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("metrics stopped", "error", err)
		}
	}()
	go func() {
		if err := server.Serve(lis); err != nil {
			logger.Error("grpc stopped", "error", err)
		}
	}()
	<-ctx.Done()
	done := make(chan struct{})
	go func() { server.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		server.Stop()
	}
}
