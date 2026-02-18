package main

import (
	"testing"

	"github.com/q-bitx/pool/internal/config"
)

func TestValidateDatabaseURL(t *testing.T) {
	err := validateDatabaseURL(&config.Config{})
	if err == nil {
		t.Fatalf("expected error when DATABASE_URL is empty")
	}

	err = validateDatabaseURL(&config.Config{DatabaseURL: "postgresql://localhost:5432/qbitx"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
