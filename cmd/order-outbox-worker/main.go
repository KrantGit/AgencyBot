package main

import (
	"context"
	"github.com/example/order-platform/internal/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, e := pgxpool.New(ctx, os.Getenv("ORDER_DB_DSN"))
	if e != nil {
		slog.Error("database connection failed", "error", e)
		return
	}
	defer pool.Close()
	k, e := kgo.NewClient(kgo.SeedBrokers(os.Getenv("KAFKA_BROKERS")))
	if e != nil {
		slog.Error("kafka client failed", "error", e)
		return
	}
	defer k.Close()
	outbox.New(pool, k, "orders.events", slog.Default().With("service", "order-outbox-worker")).Run(ctx)
}
