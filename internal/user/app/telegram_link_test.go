package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/order-platform/internal/user/domain"
	"github.com/google/uuid"
)

type linkStore struct {
	hash   []byte
	userID uuid.UUID
	used   bool
}

func (s *linkStore) Get(_ context.Context, id uuid.UUID) (domain.User, error) {
	if id != s.userID {
		return domain.User{}, domain.ErrNotFound
	}
	return domain.User{ID: id}, nil
}
func (*linkStore) ByLogin(context.Context, string) (domain.User, error) {
	return domain.User{}, domain.ErrNotFound
}
func (*linkStore) ByTelegram(context.Context, int64) (domain.User, error) {
	return domain.User{}, domain.ErrNotFound
}
func (*linkStore) List(context.Context, int, string) ([]domain.User, error)  { return nil, nil }
func (*linkStore) Create(context.Context, domain.User) error                 { return nil }
func (*linkStore) UpdateName(context.Context, uuid.UUID, string) error       { return nil }
func (*linkStore) SetPermissions(context.Context, uuid.UUID, []string) error { return nil }
func (*linkStore) SetActive(context.Context, uuid.UUID, bool) error          { return nil }
func (*linkStore) Password(context.Context, uuid.UUID, string) error         { return nil }
func (*linkStore) BindTelegram(context.Context, uuid.UUID, int64) error      { return nil }
func (s *linkStore) CreateTelegramLink(_ context.Context, hash []byte, userID uuid.UUID, _ time.Time) error {
	s.hash, s.userID = append([]byte(nil), hash...), userID
	return nil
}
func (s *linkStore) RedeemTelegramLink(_ context.Context, hash []byte, _ int64) (uuid.UUID, error) {
	if s.used || string(hash) != string(s.hash) {
		return uuid.Nil, domain.ErrNotFound
	}
	s.used = true
	return s.userID, nil
}

func TestTelegramLinkCanBeRedeemedOnlyOnce(t *testing.T) {
	store := &linkStore{}
	service := New(store)
	userID := uuid.New()
	token, _, err := service.CreateTelegramLink(context.Background(), userID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	user, err := service.RedeemTelegramLink(context.Background(), token, 123)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != userID {
		t.Fatalf("got %s, want %s", user.ID, userID)
	}
	if _, err = service.RedeemTelegramLink(context.Background(), token, 123); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second redemption error = %v, want not found", err)
	}
}
