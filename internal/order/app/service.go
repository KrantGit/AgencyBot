package app

import (
	"context"
	"github.com/example/order-platform/internal/order/domain"
	"github.com/google/uuid"
)

type Store interface {
	Get(context.Context, uuid.UUID) (domain.Order, error)
	Create(context.Context, domain.Order) error
	Update(context.Context, domain.Order, map[string]domain.Change) error
	Status(context.Context, domain.Order, domain.Status) error
	Assign(context.Context, domain.Order, uuid.UUID, bool) error
	List(context.Context, *uuid.UUID, int, string) ([]domain.Order, error)
	History(context.Context, uuid.UUID) ([]domain.History, error)
}
type PerformerLookup interface {
	IsActivePerformer(context.Context, uuid.UUID) bool
}
type Service struct {
	store Store
	users PerformerLookup
}

func New(store Store, users PerformerLookup) *Service { return &Service{store, users} }
func (s *Service) Create(ctx context.Context, o domain.Order) error {
	if !domain.Valid(o) {
		return domain.ErrValidation
	}
	for _, id := range o.PerformerIDs {
		if !s.users.IsActivePerformer(ctx, id) {
			return domain.ErrValidation
		}
	}
	o.Status = domain.Planned
	return s.store.Create(ctx, o)
}
func (s *Service) Get(ctx context.Context, id uuid.UUID, actor uuid.UUID, readAll bool) (domain.Order, error) {
	o, e := s.store.Get(ctx, id)
	if e != nil {
		return o, e
	}
	if readAll {
		return o, nil
	}
	for _, p := range o.PerformerIDs {
		if p == actor {
			return o, nil
		}
	}
	return o, domain.ErrForbidden
}
func (s *Service) Update(ctx context.Context, o domain.Order) (domain.Order, error) {
	current, e := s.store.Get(ctx, o.ID)
	if e != nil {
		return current, e
	}
	if !domain.Valid(o) || o.Version < 1 {
		return current, domain.ErrValidation
	}
	o.PerformerIDs = current.PerformerIDs
	changes := map[string]domain.Change{}
	for _, v := range []struct{ k, a, b string }{{"location", current.Location, o.Location}, {"amount", current.Amount, o.Amount}, {"comment", current.Comment, o.Comment}} {
		if v.a != v.b {
			changes[v.k] = domain.Change{Old: v.a, New: v.b}
		}
	}
	if e = s.store.Update(ctx, o, changes); e != nil {
		return current, e
	}
	return s.store.Get(ctx, o.ID)
}
func (s *Service) ChangeStatus(ctx context.Context, id, actor uuid.UUID, version int64, next domain.Status) (domain.Order, error) {
	o, e := s.store.Get(ctx, id)
	if e != nil {
		return o, e
	}
	if !domain.CanTransition(o.Status, next) || version != o.Version {
		return o, domain.ErrConflict
	}
	o.UpdatedBy = actor
	if e = s.store.Status(ctx, o, next); e != nil {
		return o, e
	}
	return s.store.Get(ctx, id)
}
func (s *Service) Assign(ctx context.Context, id, actor, performer uuid.UUID, add bool) (domain.Order, error) {
	o, e := s.store.Get(ctx, id)
	if e != nil {
		return o, e
	}
	if add && !s.users.IsActivePerformer(ctx, performer) {
		return o, domain.ErrValidation
	}
	o.UpdatedBy = actor
	if e = s.store.Assign(ctx, o, performer, add); e != nil {
		return o, e
	}
	return s.store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context, performer *uuid.UUID, limit int, after string) ([]domain.Order, error) {
	return s.store.List(ctx, performer, limit, after)
}
func (s *Service) History(ctx context.Context, id uuid.UUID) ([]domain.History, error) {
	return s.store.History(ctx, id)
}
