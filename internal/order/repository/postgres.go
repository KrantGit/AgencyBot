package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/example/order-platform/internal/order/domain"
	"github.com/example/order-platform/pkg/event"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool} }
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	return r.get(ctx, r.pool, id)
}
func (r *Repository) List(ctx context.Context, performer *uuid.UUID, limit int, after string) ([]domain.Order, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM orders WHERE ($1::uuid IS NULL OR EXISTS (SELECT 1 FROM order_performers p WHERE p.order_id=orders.id AND p.performer_id=$1)) AND ($2='' OR id::text>$2) ORDER BY order_date ASC,id ASC LIMIT $3`, performer, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Order{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		o, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}
func (r *Repository) History(ctx context.Context, orderID uuid.UUID) ([]domain.History, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,order_id,actor_user_id,action,changes,created_at FROM order_history WHERE order_id=$1 ORDER BY created_at DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.History
	for rows.Next() {
		var h domain.History
		var raw []byte
		if err = rows.Scan(&h.ID, &h.OrderID, &h.ActorID, &h.Action, &raw, &h.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &h.Changes); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (r *Repository) get(ctx context.Context, q querier, id uuid.UUID) (domain.Order, error) {
	var o domain.Order
	var status string
	err := q.QueryRow(ctx, `SELECT id,order_date,location,amount::text,customer_name,customer_phone,customer_contact,comment,status,created_by,updated_by,version,created_at,updated_at FROM orders WHERE id=$1`, id).Scan(&o.ID, &o.OrderDate, &o.Location, &o.Amount, &o.CustomerName, &o.CustomerPhone, &o.CustomerContact, &o.Comment, &status, &o.CreatedBy, &o.UpdatedBy, &o.Version, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, domain.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	o.Status = domain.Status(status)
	rows, err := q.Query(ctx, `SELECT performer_id FROM order_performers WHERE order_id=$1 ORDER BY performer_id`, id)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var x uuid.UUID
		if err = rows.Scan(&x); err != nil {
			return o, err
		}
		o.PerformerIDs = append(o.PerformerIDs, x)
	}
	return o, rows.Err()
}
func (r *Repository) Create(ctx context.Context, o domain.Order) error {
	return r.tx(ctx, o.ID, "order.created", map[string]any{"order_id": o.ID, "performer_ids": o.PerformerIDs}, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO orders(id,order_date,location,amount,customer_name,customer_phone,customer_contact,comment,status,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, o.ID, o.OrderDate, o.Location, o.Amount, o.CustomerName, o.CustomerPhone, o.CustomerContact, o.Comment, o.Status, o.CreatedBy)
		if e != nil {
			return e
		}
		for _, p := range o.PerformerIDs {
			if _, e = tx.Exec(ctx, `INSERT INTO order_performers(order_id,performer_id,assigned_by) VALUES($1,$2,$3)`, o.ID, p, o.CreatedBy); e != nil {
				return e
			}
		}
		return r.history(ctx, tx, o.ID, o.CreatedBy, "ORDER_CREATED", map[string]domain.Change{})
	})
}
func (r *Repository) Update(ctx context.Context, o domain.Order, changes map[string]domain.Change) error {
	return r.tx(ctx, o.ID, "order.updated", map[string]any{"order_id": o.ID, "performer_ids": o.PerformerIDs, "changes": changes}, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE orders SET order_date=$3,location=$4,amount=$5,customer_name=$6,customer_phone=$7,customer_contact=$8,comment=$9,updated_by=$10,updated_at=now(),version=version+1 WHERE id=$1 AND version=$2`, o.ID, o.Version, o.OrderDate, o.Location, o.Amount, o.CustomerName, o.CustomerPhone, o.CustomerContact, o.Comment, o.UpdatedBy)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}
		return r.history(ctx, tx, o.ID, o.UpdatedBy, "ORDER_UPDATED", changes)
	})
}
func (r *Repository) Status(ctx context.Context, o domain.Order, next domain.Status) error {
	return r.tx(ctx, o.ID, "order.status_changed", map[string]any{"order_id": o.ID, "performer_ids": o.PerformerIDs, "old": o.Status, "new": next}, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE orders SET status=$3,updated_by=$4,updated_at=now(),version=version+1 WHERE id=$1 AND version=$2`, o.ID, o.Version, next, o.UpdatedBy)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}
		return r.history(ctx, tx, o.ID, o.UpdatedBy, "STATUS_CHANGED", map[string]domain.Change{"status": {Old: string(o.Status), New: string(next)}})
	})
}
func (r *Repository) Assign(ctx context.Context, o domain.Order, p uuid.UUID, add bool) error {
	action, typ := "PERFORMER_ASSIGNED", "order.performer_assigned"
	if !add {
		action, typ = "PERFORMER_UNASSIGNED", "order.performer_unassigned"
	}
	return r.tx(ctx, o.ID, typ, map[string]any{"order_id": o.ID, "performer_id": p}, func(tx pgx.Tx) error {
		var e error
		if add {
			_, e = tx.Exec(ctx, `INSERT INTO order_performers(order_id,performer_id,assigned_by) VALUES($1,$2,$3)`, o.ID, p, o.UpdatedBy)
		} else {
			tag, x := tx.Exec(ctx, `DELETE FROM order_performers WHERE order_id=$1 AND performer_id=$2`, o.ID, p)
			e = x
			if e == nil && tag.RowsAffected() == 0 {
				return domain.ErrNotFound
			}
		}
		if e != nil {
			return e
		}
		return r.history(ctx, tx, o.ID, o.UpdatedBy, action, map[string]domain.Change{"performer_id": {New: p.String()}})
	})
}
func (r *Repository) history(ctx context.Context, tx pgx.Tx, id, actor uuid.UUID, action string, changes map[string]domain.Change) error {
	b, e := json.Marshal(changes)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO order_history(id,order_id,actor_user_id,action,changes) VALUES($1,$2,$3,$4,$5)`, uuid.New(), id, actor, action, b)
	return e
}
func (r *Repository) tx(ctx context.Context, id uuid.UUID, typ string, data any, fn func(pgx.Tx) error) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = fn(tx); e != nil {
		return e
	}
	b, e := json.Marshal(event.Envelope{EventID: uuid.New(), EventType: typ, EventVersion: 1, AggregateID: id, OccurredAt: time.Now().UTC(), Producer: "order-service", Data: data})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO outbox_events(id,aggregate_id,event_type,payload)VALUES($1,$2,$3,$4)`, uuid.New(), id, typ, b)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
