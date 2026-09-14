package outbox

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"log/slog"
	"time"
)

type Worker struct {
	pool  *pgxpool.Pool
	kafka *kgo.Client
	topic string
	log   *slog.Logger
}
type row struct {
	id, aggregateID string
	payload         []byte
}

func New(pool *pgxpool.Pool, kafka *kgo.Client, topic string, log *slog.Logger) *Worker {
	return &Worker{pool, kafka, topic, log}
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := w.Process(ctx); err != nil {
			w.log.Error("outbox batch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *Worker) Process(ctx context.Context) error {
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id,aggregate_id,payload FROM outbox_events WHERE published_at IS NULL ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 50`)
	if e != nil {
		return e
	}
	var batch []row
	for rows.Next() {
		var x row
		if e = rows.Scan(&x.id, &x.aggregateID, &x.payload); e != nil {
			rows.Close()
			return e
		}
		batch = append(batch, x)
	}
	rows.Close()
	for _, x := range batch {
		if e = w.kafka.ProduceSync(ctx, &kgo.Record{Topic: w.topic, Key: []byte(x.aggregateID), Value: x.payload}).FirstErr(); e != nil {
			_, _ = tx.Exec(ctx, `UPDATE outbox_events SET attempts=attempts+1 WHERE id=$1`, x.id)
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE outbox_events SET published_at=now(),attempts=attempts+1 WHERE id=$1`, x.id); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

var _ = json.Valid
