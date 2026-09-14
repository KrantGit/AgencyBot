package app

import (
	"context"
	"errors"
	"github.com/example/order-platform/internal/user/domain"
	"github.com/example/order-platform/internal/user/password"
	"github.com/google/uuid"
	"strings"
)

type Store interface {
	Get(context.Context, uuid.UUID) (domain.User, error)
	ByLogin(context.Context, string) (domain.User, error)
	ByTelegram(context.Context, int64) (domain.User, error)
	List(context.Context, int, string) ([]domain.User, error)
	Create(context.Context, domain.User) error
	UpdateName(context.Context, uuid.UUID, string) error
	SetPermissions(context.Context, uuid.UUID, []string) error
	SetActive(context.Context, uuid.UUID, bool) error
	Password(context.Context, uuid.UUID, string) error
	BindTelegram(context.Context, uuid.UUID, int64) error
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store} }
func (s *Service) Authenticate(ctx context.Context, login, raw string) (domain.User, error) {
	u, err := s.store.ByLogin(ctx, login)
	if err != nil {
		return domain.User{}, domain.ErrUnauthorized
	}
	if !u.IsActive || !password.Verify(raw, u.PasswordHash) {
		return domain.User{}, domain.ErrUnauthorized
	}
	return u, nil
}
func (s *Service) Create(ctx context.Context, login, raw, fullName string, role domain.Role) (domain.User, error) {
	login = strings.TrimSpace(login)
	fullName = strings.TrimSpace(fullName)
	if !domain.ValidLogin(login) || len(raw) < 12 || fullName == "" || (role != domain.RoleAdmin && role != domain.RolePerformer) {
		return domain.User{}, domain.ErrValidation
	}
	hash, err := password.Hash(raw)
	if err != nil {
		return domain.User{}, err
	}
	u := domain.User{ID: uuid.New(), Login: login, PasswordHash: hash, FullName: fullName, Role: role, IsActive: true, Permissions: domain.DefaultPermissions(role)}
	if err = s.store.Create(ctx, u); err != nil {
		return domain.User{}, err
	}
	return s.store.Get(ctx, u.ID)
}
func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) ByTelegram(ctx context.Context, id int64) (domain.User, error) {
	return s.store.ByTelegram(ctx, id)
}
func (s *Service) List(ctx context.Context, limit int, after string) ([]domain.User, error) {
	return s.store.List(ctx, limit, after)
}
func (s *Service) UpdateName(ctx context.Context, id uuid.UUID, name string) (domain.User, error) {
	if strings.TrimSpace(name) == "" {
		return domain.User{}, domain.ErrValidation
	}
	if err := s.store.UpdateName(ctx, id, strings.TrimSpace(name)); err != nil {
		return domain.User{}, err
	}
	return s.store.Get(ctx, id)
}
func (s *Service) Permissions(ctx context.Context, id uuid.UUID, ps []string) (domain.User, error) {
	if !domain.ValidPermissions(ps) {
		return domain.User{}, domain.ErrValidation
	}
	if err := s.store.SetPermissions(ctx, id, ps); err != nil {
		return domain.User{}, err
	}
	return s.store.Get(ctx, id)
}
func (s *Service) Active(ctx context.Context, id uuid.UUID, active bool) (domain.User, error) {
	if err := s.store.SetActive(ctx, id, active); err != nil {
		return domain.User{}, err
	}
	return s.store.Get(ctx, id)
}
func (s *Service) ChangePassword(ctx context.Context, id uuid.UUID, old, raw string) error {
	u, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !password.Verify(old, u.PasswordHash) {
		return domain.ErrUnauthorized
	}
	return s.reset(ctx, id, raw)
}
func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID, raw string) error {
	return s.reset(ctx, id, raw)
}
func (s *Service) reset(ctx context.Context, id uuid.UUID, raw string) error {
	if len(raw) < 12 {
		return domain.ErrValidation
	}
	h, err := password.Hash(raw)
	if err != nil {
		return err
	}
	return s.store.Password(ctx, id, h)
}
func (s *Service) BindTelegram(ctx context.Context, id uuid.UUID, telegramID int64) (domain.User, error) {
	if telegramID == 0 {
		return domain.User{}, domain.ErrValidation
	}
	if err := s.store.BindTelegram(ctx, id, telegramID); err != nil {
		return domain.User{}, mapConflict(err)
	}
	return s.store.Get(ctx, id)
}
func mapConflict(err error) error {
	if strings.Contains(err.Error(), "duplicate key") {
		return domain.ErrConflict
	}
	return err
}

var _ = errors.Is
