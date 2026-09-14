package domain

import (
	"errors"
	"github.com/google/uuid"
	"math/big"
	"strings"
	"time"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrValidation = errors.New("validation failed")
	ErrConflict   = errors.New("conflict")
)

type Status string

const (
	Planned   Status = "PLANNED"
	Completed Status = "COMPLETED"
	Cancelled Status = "CANCELLED"
)

type Order struct {
	ID                                                                      uuid.UUID
	OrderDate                                                               time.Time
	Location, Amount, CustomerName, CustomerPhone, CustomerContact, Comment string
	Status                                                                  Status
	CreatedBy, UpdatedBy                                                    uuid.UUID
	Version                                                                 int64
	PerformerIDs                                                            []uuid.UUID
	CreatedAt, UpdatedAt                                                    time.Time
}
type History struct {
	ID, OrderID, ActorID uuid.UUID
	Action               string
	Changes              map[string]Change
	CreatedAt            time.Time
}
type Change struct{ Old, New string }

func ValidAmount(value string) bool {
	r, ok := new(big.Rat).SetString(value)
	return ok && r.Sign() > 0 && len(strings.Split(value, ".")) <= 2 || ok && r.Sign() > 0
}
func Valid(o Order) bool {
	return !o.OrderDate.IsZero() && strings.TrimSpace(o.Location) != "" && ValidAmount(o.Amount) && strings.TrimSpace(o.CustomerName) != ""
}
func CanTransition(old, next Status) bool {
	return old == Planned && (next == Completed || next == Cancelled)
}
