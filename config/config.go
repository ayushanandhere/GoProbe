package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultServerPort      = 8080
	DefaultHistorySize     = 100
	defaultIntervalMinimum = time.Millisecond
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Monitor MonitorConfig `yaml:"monitor"`
	Targets []Target      `yaml:"targets"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type MonitorConfig struct {
	DefaultInterval time.Duration `yaml:"default_interval"`
	DefaultTimeout  time.Duration `yaml:"default_timeout"`
}

type Target struct {
	Name     string        `yaml:"name" json:"name"`
	Type     string        `yaml:"type" json:"type"`
	Endpoint string        `yaml:"endpoint" json:"endpoint"`
	Interval time.Duration `yaml:"interval" json:"-"`
	Timeout  time.Duration `yaml:"timeout" json:"-"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = DefaultServerPort
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return nil, fmt.Errorf("server port must be between 1 and 65535")
	}

	if cfg.Monitor.DefaultInterval < defaultIntervalMinimum {
		return nil, fmt.Errorf("monitor default_interval must be greater than 0")
	}
	if cfg.Monitor.DefaultTimeout < defaultIntervalMinimum {
		return nil, fmt.Errorf("monitor default_timeout must be greater than 0")
	}

	seen := make(map[string]struct{}, len(cfg.Targets))
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		NormalizeTarget(target)
		ApplyTargetDefaults(target, cfg.Monitor)
		if err := ValidateTarget(*target); err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}

		if _, exists := seen[target.Name]; exists {
			return nil, fmt.Errorf("target %q is duplicated", target.Name)
		}
		seen[target.Name] = struct{}{}
	}

	return &cfg, nil
}

func NormalizeTarget(target *Target) {
	target.Name = strings.TrimSpace(target.Name)
	target.Type = strings.ToLower(strings.TrimSpace(target.Type))
	target.Endpoint = strings.TrimSpace(target.Endpoint)
}

func ApplyTargetDefaults(target *Target, defaults MonitorConfig) {
	if target.Interval == 0 {
		target.Interval = defaults.DefaultInterval
	}
	if target.Timeout == 0 {
		target.Timeout = defaults.DefaultTimeout
	}
}

func ValidateTarget(target Target) error {
	NormalizeTarget(&target)

	switch {
	case target.Name == "":
		return errors.New("missing required field: name")
	case target.Type == "":
		return errors.New("missing required field: type")
	case target.Endpoint == "":
		return errors.New("missing required field: endpoint")
	case target.Type != "http" && target.Type != "tcp":
		return fmt.Errorf("invalid target type %q", target.Type)
	case target.Interval < defaultIntervalMinimum:
		return errors.New("interval must be greater than 0")
	case target.Timeout < defaultIntervalMinimum:
		return errors.New("timeout must be greater than 0")
	default:
		return nil
	}
}
