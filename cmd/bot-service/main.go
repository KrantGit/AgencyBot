package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	orderv1 "github.com/example/order-platform/gen/go/order/v1"
	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/example/order-platform/internal/bot"
	"github.com/example/order-platform/internal/config"
	"github.com/example/order-platform/internal/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "bot-service")
	cfg := config.Bot()
	if cfg.TelegramToken == "" || cfg.JWTSecret == "" {
		logger.Error("TELEGRAM_BOT_TOKEN and JWT_SECRET are required")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	userConn, err := grpc.NewClient(cfg.UserGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("user service connection failed", "error", err)
		os.Exit(1)
	}
	defer userConn.Close()
	orderConn, err := grpc.NewClient(cfg.OrderGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("order service connection failed", "error", err)
		os.Exit(1)
	}
	defer orderConn.Close()
	go func() {
		if err := metrics.Serve(ctx, ":"+cfg.MetricsPort); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("metrics stopped", "error", err)
		}
	}()
	if err := bot.NewService(bot.NewTelegramClient(cfg.TelegramToken), userv1.NewUserServiceClient(userConn), orderv1.NewOrderServiceClient(orderConn), bot.NewDraftStore(cfg.RedisAddr), cfg.JWTSecret).Run(ctx); err != nil {
		logger.Error("bot stopped", "error", err)
		os.Exit(1)
	}
}
