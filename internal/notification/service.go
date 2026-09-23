package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/order-platform/internal/bot"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool     *pgxpool.Pool
	telegram *bot.TelegramClient
	log      *slog.Logger
}

type envelope struct {
	EventID   uuid.UUID       `json:"event_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
}

func New(pool *pgxpool.Pool, telegram *bot.TelegramClient, logger *slog.Logger) *Service {
	return &Service{pool: pool, telegram: telegram, log: logger}
}

func (s *Service) Process(ctx context.Context, value []byte) error {
	var event envelope
	if err := json.Unmarshal(value, &event); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	if event.EventID == uuid.Nil || event.EventType == "" {
		return errors.New("event id and type are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO processed_events(event_id) VALUES($1) ON CONFLICT DO NOTHING`, event.EventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if len(event.EventType) >= 5 && event.EventType[:5] == "user." {
		err = syncRecipient(ctx, tx, event.Data)
	} else if len(event.EventType) >= 6 && event.EventType[:6] == "order." {
		err = createNotifications(ctx, tx, event)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func syncRecipient(ctx context.Context, tx pgx.Tx, data json.RawMessage) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	var userID uuid.UUID
	if err := json.Unmarshal(values["user_id"], &userID); err != nil || userID == uuid.Nil {
		return fmt.Errorf("user event has no user_id")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO notification_recipients(user_id,is_active) VALUES($1,true) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	if raw, ok := values["telegram_id"]; ok {
		var telegramID *int64
		if err := json.Unmarshal(raw, &telegramID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notification_recipients SET telegram_id=$2,updated_at=now() WHERE user_id=$1`, userID, telegramID); err != nil {
			return err
		}
	}
	if raw, ok := values["is_active"]; ok {
		var active bool
		if err := json.Unmarshal(raw, &active); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notification_recipients SET is_active=$2,updated_at=now() WHERE user_id=$1`, userID, active); err != nil {
			return err
		}
	}
	return nil
}

func createNotifications(ctx context.Context, tx pgx.Tx, event envelope) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &values); err != nil {
		return err
	}
	recipients := make([]uuid.UUID, 0)
	if raw, ok := values["performer_ids"]; ok {
		if err := json.Unmarshal(raw, &recipients); err != nil {
			return err
		}
	} else if raw, ok := values["performer_id"]; ok {
		var recipient uuid.UUID
		if err := json.Unmarshal(raw, &recipient); err != nil {
			return err
		}
		recipients = append(recipients, recipient)
	}
	for _, recipient := range recipients {
		if _, err := tx.Exec(ctx, `INSERT INTO notifications(id,event_id,recipient_id,channel,status) SELECT $1,$2,user_id,'TELEGRAM','PENDING' FROM notification_recipients WHERE user_id=$3 AND telegram_id IS NOT NULL AND is_active ON CONFLICT(event_id,recipient_id) DO NOTHING`, uuid.New(), event.EventID, recipient); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Dispatch(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT n.id,n.event_id,r.telegram_id FROM notifications n JOIN notification_recipients r ON r.user_id=n.recipient_id WHERE n.status='PENDING' AND r.is_active AND r.telegram_id IS NOT NULL ORDER BY n.created_at LIMIT 50`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, eventID uuid.UUID
		var telegramID int64
		if err := rows.Scan(&id, &eventID, &telegramID); err != nil {
			return err
		}
		if err := s.telegram.Send(ctx, telegramID, "У вас новое изменение по заказу. Откройте /orders для просмотра."); err != nil {
			_, _ = s.pool.Exec(ctx, `UPDATE notifications SET attempts=attempts+1,error_message=$2 WHERE id=$1`, id, err.Error())
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE notifications SET status='SENT',sent_at=now(),attempts=attempts+1,error_message=NULL WHERE id=$1`, id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) RunDispatcher(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Dispatch(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("notification dispatch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
