package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigValid(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 9090
monitor:
  default_interval: 10s
  default_timeout: 5s
targets:
  - name: "Google"
    type: "http"
    endpoint: "https://google.com"
  - name: "Redis"
    type: "tcp"
    endpoint: "localhost:6379"
    interval: 2s
    timeout: 1s
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Fatalf("port = %d, want 9090", cfg.Server.Port)
	}
	if got := cfg.Targets[0].Interval; got != 10*time.Second {
		t.Fatalf("default interval = %v, want 10s", got)
	}
	if got := cfg.Targets[0].Timeout; got != 5*time.Second {
		t.Fatalf("default timeout = %v, want 5s", got)
	}
	if got := cfg.Targets[1].Interval; got != 2*time.Second {
		t.Fatalf("custom interval = %v, want 2s", got)
	}
}

func TestLoadConfigMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "missing name",
			body: `
server:
  port: 8080
monitor:
  default_interval: 10s
  default_timeout: 5s
targets:
  - type: "http"
    endpoint: "https://example.com"
`,
			wantErr: "missing required field: name",
		},
		{
			name: "missing endpoint",
			body: `
server:
  port: 8080
monitor:
  default_interval: 10s
  default_timeout: 5s
targets:
  - name: "Example"
    type: "http"
`,
			wantErr: "missing required field: endpoint",
		},
		{
			name: "invalid type",
			body: `
server:
  port: 8080
monitor:
  default_interval: 10s
  default_timeout: 5s
targets:
  - name: "Example"
    type: "udp"
    endpoint: "localhost:53"
`,
			wantErr: `invalid target type "udp"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadConfig() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadConfigRejectsDuplicateTargetNames(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
monitor:
  default_interval: 10s
  default_timeout: 5s
targets:
  - name: "Example"
    type: "http"
    endpoint: "https://example.com"
  - name: "Example"
    type: "tcp"
    endpoint: "localhost:1234"
`)

	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `target "Example" is duplicated`) {
		t.Fatalf("LoadConfig() error = %v, want duplicate target error", err)
	}
}

func TestApplyTargetDefaultsAndValidateTarget(t *testing.T) {
	target := Target{
		Name:     "Example",
		Type:     "http",
		Endpoint: "https://example.com",
	}

	ApplyTargetDefaults(&target, MonitorConfig{
		DefaultInterval: 12 * time.Second,
		DefaultTimeout:  4 * time.Second,
	})

	if err := ValidateTarget(target); err != nil {
		t.Fatalf("ValidateTarget() error = %v", err)
	}
	if target.Interval != 12*time.Second {
		t.Fatalf("interval = %v, want 12s", target.Interval)
	}
	if target.Timeout != 4*time.Second {
		t.Fatalf("timeout = %v, want 4s", target.Timeout)
	}
}

func TestValidateTargetRejectsUnsafeRuntimeTargets(t *testing.T) {
	tests := []struct {
		name   string
		target Target
	}{
		{
			name: "bad scheme",
			target: Target{
				Name:     "Example",
				Type:     "http",
				Endpoint: "file:///etc/passwd",
				Interval: time.Second,
				Timeout:  time.Second,
			},
		},
		{
			name: "localhost http",
			target: Target{
				Name:     "Example",
				Type:     "http",
				Endpoint: "http://localhost:8080",
				Interval: time.Second,
				Timeout:  time.Second,
			},
		},
		{
			name: "private ip http",
			target: Target{
				Name:     "Example",
				Type:     "http",
				Endpoint: "http://127.0.0.1:8080",
				Interval: time.Second,
				Timeout:  time.Second,
			},
		},
		{
			name: "private ip tcp",
			target: Target{
				Name:     "Example",
				Type:     "tcp",
				Endpoint: "10.0.0.5:5432",
				Interval: time.Second,
				Timeout:  time.Second,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTarget(tc.target); err == nil {
				t.Fatal("ValidateTarget() error = nil, want rejection")
			}
		})
	}
}

func TestValidateTargetAllowsTrustedPrivateTargets(t *testing.T) {
	target := Target{
		Name:     "Example",
		Type:     "tcp",
		Endpoint: "localhost:6379",
		Interval: time.Second,
		Timeout:  time.Second,
		Trusted:  true,
	}

	if err := ValidateTarget(target); err != nil {
		t.Fatalf("ValidateTarget() error = %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
