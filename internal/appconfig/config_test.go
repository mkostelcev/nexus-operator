package appconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_NoFile_ReturnsDefaults(t *testing.T) {
	// Указываем заведомо несуществующий путь — Load должен вернуть пустой
	// Config без ошибки, чтобы оператор стартовал на дефолтах.
	t.Setenv(configPathEnv, filepath.Join(t.TempDir(), "missing.yaml"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !cfg.LeaderElection.IsEnabled() {
		t.Errorf("по умолчанию leader election должен быть включён")
	}
	if got := cfg.LeaderElection.GetID(); got != "nexus-operator-lock" {
		t.Errorf("дефолтный ID lease = %q, ожидался %q", got, "nexus-operator-lock")
	}
	if cfg.LeaderElection.LeaseDurationPtr() != nil {
		t.Errorf("LeaseDuration должен быть nil (дефолт controller-runtime)")
	}
}

func TestLoad_FullYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte(`
leaderElection:
  enabled: true
  id: my-custom-lock
  leaseDuration: 30s
  renewDeadline: 20s
  retryPeriod: 4s
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(configPathEnv, path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.LeaderElection.IsEnabled() {
		t.Errorf("enabled=true ожидался")
	}
	if cfg.LeaderElection.GetID() != "my-custom-lock" {
		t.Errorf("ID = %q", cfg.LeaderElection.GetID())
	}
	if got := cfg.LeaderElection.LeaseDurationPtr(); got == nil || *got != 30*time.Second {
		t.Errorf("LeaseDuration = %v, ожидался 30s", got)
	}
	if got := cfg.LeaderElection.RenewDeadlinePtr(); got == nil || *got != 20*time.Second {
		t.Errorf("RenewDeadline = %v, ожидался 20s", got)
	}
	if got := cfg.LeaderElection.RetryPeriodPtr(); got == nil || *got != 4*time.Second {
		t.Errorf("RetryPeriod = %v, ожидался 4s", got)
	}
}

func TestLoad_ExplicitDisable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("leaderElection:\n  enabled: false\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(configPathEnv, path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LeaderElection.IsEnabled() {
		t.Errorf("enabled=false должен явно выключать leader election")
	}
}

func TestLoad_BadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("leaderElection: [not, an, object]\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(configPathEnv, path)

	if _, err := Load(); err == nil {
		t.Fatalf("ожидалась ошибка парсинга, получено nil")
	}
}
