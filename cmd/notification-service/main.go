package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/order-platform/internal/bot"
	"github.com/example/order-platform/internal/config"
	"github.com/example/order-platform/internal/metrics"
	"github.com/example/order-platform/internal/notification"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "notification-service")
	cfg := config.Notification()
	if cfg.TelegramToken == "" {
		logger.Error("TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseDSN)
	if err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	kafka, err := kgo.NewClient(kgo.SeedBrokers(cfg.KafkaBrokers), kgo.ConsumerGroup("notification-service"), kgo.ConsumeTopics("users.events", "orders.events"), kgo.DisableAutoCommit())
	if err != nil {
		logger.Error("kafka client failed", "error", err)
		os.Exit(1)
	}
	defer kafka.Close()
	service := notification.New(pool, bot.NewTelegramClient(cfg.TelegramToken), logger)
	go service.RunDispatcher(ctx)
	go func() {
		if err := metrics.Serve(ctx, ":"+cfg.MetricsPort); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("metrics stopped", "error", err)
		}
	}()
	for ctx.Err() == nil {
		fetches := kafka.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			logger.Error("kafka poll failed", "error", errs[0].Err)
			continue
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if err := service.Process(ctx, record.Value); err != nil {
				logger.Error("event processing failed", "error", err)
				return
			}
			if err := kafka.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				logger.Error("kafka commit failed", "error", err)
			}
		})
	}
}
