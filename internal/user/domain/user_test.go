package domain

import "testing"

func TestValidationAndDefaultPermissions(t *testing.T) {
	if !ValidLogin("worker.01") || ValidLogin("x") {
		t.Fatal("login validation is incorrect")
	}
	if !ValidPermissions(DefaultPermissions(RoleAdmin)) {
		t.Fatal("admin defaults must be valid")
	}
	if ValidPermissions([]string{"ROOT"}) {
		t.Fatal("unknown permission accepted")
	}
}
