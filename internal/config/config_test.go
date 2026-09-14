package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_ParsesDuration(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := file.WriteString("server:\n  port: 9000\nshutdown:\n  timeout: 45s\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close config: %v", err)
	}

	cfg, err := Load(file.Name())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.Shutdown.Timeout.Std() != 45*time.Second {
		t.Errorf("shutdown timeout = %s, want 45s", cfg.Shutdown.Timeout.Std())
	}
}

func TestLoad_RejectsInvalidDuration(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := file.WriteString("server:\n  port: 8080\nshutdown:\n  timeout: forever\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close config: %v", err)
	}

	if _, err := Load(file.Name()); err == nil {
		t.Fatal("expected invalid duration to be rejected")
	}
}
