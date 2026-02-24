package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllowsSoloScheme(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := []byte(`
coinbase:
  address: "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh"
payouts:
  scheme: "solo"
`)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DATABASE_URL", "postgresql://x:y@localhost:5432/db")
	if _, err := Load(cfgPath); err != nil {
		t.Fatalf("load config failed for solo scheme: %v", err)
	}
}

func TestLoadRejectsUnknownScheme(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := []byte(`
coinbase:
  address: "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh"
payouts:
  scheme: "unknown"
`)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DATABASE_URL", "postgresql://x:y@localhost:5432/db")
	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected load error for unknown scheme")
	}
}

func TestLoadRejectsDeveloperFeeWithTooManyDecimals(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := []byte(`
coinbase:
  address: "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh"
  developer_fee_percent: 1.234
  developer_fee_address: "MWnxrbCdBdtUP2M5NZ7v5bYrbZ78ZsMBzh"
`)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DATABASE_URL", "postgresql://x:y@localhost:5432/db")
	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected load error for developer fee precision")
	}
}
