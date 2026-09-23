package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/example/order-platform/internal/user/domain"
	"github.com/example/order-platform/pkg/event"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool} }
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return r.find(ctx, `SELECT id,login,password_hash,full_name,role,is_active,telegram_id,created_at,updated_at FROM users WHERE id=$1`, id)
}
func (r *Repository) ByLogin(ctx context.Context, login string) (domain.User, error) {
	return r.find(ctx, `SELECT id,login,password_hash,full_name,role,is_active,telegram_id,created_at,updated_at FROM users WHERE login=$1`, login)
}
func (r *Repository) ByTelegram(ctx context.Context, telegramID int64) (domain.User, error) {
	return r.find(ctx, `SELECT id,login,password_hash,full_name,role,is_active,telegram_id,created_at,updated_at FROM users WHERE telegram_id=$1`, telegramID)
}
func (r *Repository) find(ctx context.Context, q string, arg any) (domain.User, error) {
	var u domain.User
	var role string
	err := r.pool.QueryRow(ctx, q, arg).Scan(&u.ID, &u.Login, &u.PasswordHash, &u.FullName, &role, &u.IsActive, &u.TelegramID, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, domain.ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.Role = domain.Role(role)
	rows, err := r.pool.Query(ctx, `SELECT permission_code FROM user_permissions WHERE user_id=$1 ORDER BY permission_code`, u.ID)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			return u, err
		}
		u.Permissions = append(u.Permissions, p)
	}
	return u, rows.Err()
}
func (r *Repository) List(ctx context.Context, limit int, after string) ([]domain.User, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM users WHERE ($1='' OR id::text>$1) ORDER BY id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.User
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		u, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
func (r *Repository) Create(ctx context.Context, u domain.User) error {
	return r.withEvent(ctx, u.ID, "user.created", map[string]any{"user_id": u.ID, "telegram_id": u.TelegramID, "is_active": u.IsActive}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO users(id,login,password_hash,full_name,role,is_active) VALUES($1,$2,$3,$4,$5,$6)`, u.ID, u.Login, u.PasswordHash, u.FullName, u.Role, u.IsActive)
		if err != nil {
			return err
		}
		for _, p := range u.Permissions {
			if _, err = tx.Exec(ctx, `INSERT INTO user_permissions(user_id,permission_code) VALUES($1,$2)`, u.ID, p); err != nil {
				return err
			}
		}
		return nil
	})
}
func (r *Repository) UpdateName(ctx context.Context, id uuid.UUID, name string) error {
	return r.mutate(ctx, id, "user.updated", map[string]any{"user_id": id, "full_name": name}, `UPDATE users SET full_name=$2,updated_at=now() WHERE id=$1`, id, name)
}
func (r *Repository) SetPermissions(ctx context.Context, id uuid.UUID, ps []string) error {
	return r.withEvent(ctx, id, "user.updated", map[string]any{"user_id": id, "permissions": ps}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET updated_at=now() WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if _, err = tx.Exec(ctx, `DELETE FROM user_permissions WHERE user_id=$1`, id); err != nil {
			return err
		}
		for _, p := range ps {
			if _, err = tx.Exec(ctx, `INSERT INTO user_permissions(user_id,permission_code) VALUES($1,$2)`, id, p); err != nil {
				return err
			}
		}
		return nil
	})
}
func (r *Repository) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	typ := "user.updated"
	if !active {
		typ = "user.disabled"
	}
	return r.mutate(ctx, id, typ, map[string]any{"user_id": id, "is_active": active}, `UPDATE users SET is_active=$2,updated_at=now() WHERE id=$1`, id, active)
}
func (r *Repository) Password(ctx context.Context, id uuid.UUID, hash string) error {
	return r.mutate(ctx, id, "user.updated", map[string]any{"user_id": id}, `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, id, hash)
}
func (r *Repository) BindTelegram(ctx context.Context, id uuid.UUID, telegramID int64) error {
	return r.mutate(ctx, id, "user.telegram_bound", map[string]any{"user_id": id, "telegram_id": telegramID}, `UPDATE users SET telegram_id=$2,updated_at=now() WHERE id=$1`, id, telegramID)
}
func (r *Repository) CreateTelegramLink(ctx context.Context, hash []byte, userID uuid.UUID, expiresAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `INSERT INTO telegram_link_tokens(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash, userID, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
func (r *Repository) RedeemTelegramLink(ctx context.Context, hash []byte, telegramID int64) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_link_tokens WHERE token_hash=$1 AND used_at IS NULL AND expires_at > now() FOR UPDATE`, hash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET telegram_id=$2,updated_at=now() WHERE id=$1`, userID, telegramID)
	if err != nil {
		return uuid.Nil, err
	}
	if tag.RowsAffected() == 0 {
		return uuid.Nil, domain.ErrNotFound
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_link_tokens SET used_at=now() WHERE token_hash=$1`, hash); err != nil {
		return uuid.Nil, err
	}
	payload, err := json.Marshal(event.Envelope{EventID: uuid.New(), EventType: "user.telegram_bound", EventVersion: 1, AggregateID: userID, OccurredAt: time.Now().UTC(), Producer: "user-service", Data: map[string]any{"user_id": userID, "telegram_id": telegramID}})
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox_events(id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, uuid.New(), userID, payload); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}
func (r *Repository) mutate(ctx context.Context, id uuid.UUID, typ string, data any, q string, args ...any) error {
	return r.withEvent(ctx, id, typ, data, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, q, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}
func (r *Repository) withEvent(ctx context.Context, id uuid.UUID, typ string, data any, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = fn(tx); err != nil {
		return err
	}
	payload, err := json.Marshal(event.Envelope{EventID: uuid.New(), EventType: typ, EventVersion: 1, AggregateID: id, OccurredAt: time.Now().UTC(), Producer: "user-service", Data: data})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events(id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, uuid.New(), id, typ, payload)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
