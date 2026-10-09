package domain

import (
	"testing"
	"time"
)

func TestAvailabilityAndRecovery(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	tests := []struct {
		name string
		c    Credential
		want error
	}{
		{"disabled", Credential{Generation: 1, Disabled: true}, ErrDisabled},
		{"expired", Credential{Generation: 1, ExpiryKnown: true, ExpiresAt: &past}, ErrExpired},
		{"malformed expiry", Credential{Generation: 1, ExpiryKnown: true}, ErrExpired},
		{"unknown unconfirmed", Credential{Generation: 1}, ErrMissing},
		{"unknown confirmed", Credential{Generation: 1, ConfirmedGeneration: 1}, nil},
		{"invalid", Credential{Generation: 1, ConfirmedGeneration: 1, ValidationGeneration: 1, ValidationResult: "invalid"}, ErrMissing},
		{"known unconfirmed", Credential{Generation: 1, ExpiryKnown: true, ExpiresAt: &future}, ErrMissing},
		{"known valid", Credential{Generation: 1, ExpiryKnown: true, ExpiresAt: &future, ValidationGeneration: 1, ValidationResult: "valid"}, nil},
		{"stale valid", Credential{Generation: 2, ExpiryKnown: true, ExpiresAt: &future, ValidationGeneration: 1, ValidationResult: "valid"}, ErrMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Available(now); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
	c := Credential{Generation: 3, Disabled: true, DisabledGeneration: 1, RecoveryBasisGeneration: 2, RecoveryBasisOperationID: "op"}
	if !c.CanRestore() {
		t.Fatal("qualified replacement")
	}
	c.RecoveryBasisOperationID = ""
	if c.CanRestore() {
		t.Fatal("missing evidence")
	}
}
