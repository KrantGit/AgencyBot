package domain

import (
	"errors"
	"github.com/google/uuid"
	"regexp"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrUnauthorized = errors.New("unauthorized")
	ErrValidation   = errors.New("validation failed")
	ErrConflict     = errors.New("conflict")
)

type Role string

const (
	RoleAdmin     Role = "ADMIN"
	RolePerformer Role = "PERFORMER"
)
const (
	PermissionUserRead          = "USER_READ"
	PermissionUserCreate        = "USER_CREATE"
	PermissionUserUpdate        = "USER_UPDATE"
	PermissionUserDisable       = "USER_DISABLE"
	PermissionOrderReadAssigned = "ORDER_READ_ASSIGNED"
	PermissionOrderReadAll      = "ORDER_READ_ALL"
	PermissionOrderCreate       = "ORDER_CREATE"
	PermissionOrderUpdate       = "ORDER_UPDATE"
	PermissionOrderChangeStatus = "ORDER_CHANGE_STATUS"
	PermissionOrderHistoryRead  = "ORDER_HISTORY_READ"
)

var permissions = map[string]struct{}{PermissionUserRead: {}, PermissionUserCreate: {}, PermissionUserUpdate: {}, PermissionUserDisable: {}, PermissionOrderReadAssigned: {}, PermissionOrderReadAll: {}, PermissionOrderCreate: {}, PermissionOrderUpdate: {}, PermissionOrderChangeStatus: {}, PermissionOrderHistoryRead: {}}
var loginPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,63}$`)

type User struct {
	ID                            uuid.UUID
	Login, PasswordHash, FullName string
	Role                          Role
	IsActive                      bool
	TelegramID                    *int64
	Permissions                   []string
	CreatedAt, UpdatedAt          time.Time
}

func ValidLogin(s string) bool { return loginPattern.MatchString(s) }
func ValidPermissions(items []string) bool {
	for _, p := range items {
		if _, ok := permissions[p]; !ok {
			return false
		}
	}
	return true
}
func DefaultPermissions(role Role) []string {
	if role == RoleAdmin {
		return []string{PermissionUserRead, PermissionUserCreate, PermissionUserUpdate, PermissionUserDisable, PermissionOrderReadAssigned, PermissionOrderReadAll, PermissionOrderCreate, PermissionOrderUpdate, PermissionOrderChangeStatus, PermissionOrderHistoryRead}
	}
	return []string{PermissionOrderReadAssigned}
}
