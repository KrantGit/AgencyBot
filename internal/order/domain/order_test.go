package domain

import "testing"

func TestStatusTransitions(t *testing.T) {
	if !CanTransition(Planned, Completed) || CanTransition(Completed, Planned) || CanTransition(Planned, Planned) {
		t.Fatal("invalid transition rules")
	}
}
