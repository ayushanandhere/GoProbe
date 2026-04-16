package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultServerPort      = 8080
	DefaultHistorySize     = 100
	MaxTargets             = 256
	defaultIntervalMinimum = time.Second
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Monitor MonitorConfig `yaml:"monitor"`
	Targets []Target      `yaml:"targets"`
}

type ServerConfig struct {
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
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
	Trusted  bool          `yaml:"-" json:"-"`
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
	cfg.Server.AuthToken = strings.TrimSpace(cfg.Server.AuthToken)

	if cfg.Monitor.DefaultInterval < defaultIntervalMinimum {
		return nil, fmt.Errorf("monitor default_interval must be greater than 0")
	}
	if cfg.Monitor.DefaultTimeout < defaultIntervalMinimum {
		return nil, fmt.Errorf("monitor default_timeout must be greater than 0")
	}
	if len(cfg.Targets) > MaxTargets {
		return nil, fmt.Errorf("targets cannot exceed %d entries", MaxTargets)
	}

	seen := make(map[string]struct{}, len(cfg.Targets))
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		NormalizeTarget(target)
		ApplyTargetDefaults(target, cfg.Monitor)
		target.Trusted = true
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
		return fmt.Errorf("interval must be at least %s", defaultIntervalMinimum)
	case target.Timeout < defaultIntervalMinimum:
		return fmt.Errorf("timeout must be at least %s", defaultIntervalMinimum)
	}

	switch target.Type {
	case "http":
		return validateHTTPTarget(target)
	case "tcp":
		return validateTCPTarget(target)
	}

	return nil
}

func validateHTTPTarget(target Target) error {
	parsed, err := url.Parse(target.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid http endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("http endpoint must use http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return errors.New("http endpoint must include a host")
	}
	if parsed.User != nil {
		return errors.New("http endpoint must not include user info")
	}
	if parsed.Fragment != "" {
		return errors.New("http endpoint must not include a fragment")
	}
	if target.Trusted {
		return nil
	}

	return validateUntrustedHost(parsed.Hostname())
}

func validateTCPTarget(target Target) error {
	host, port, err := net.SplitHostPort(target.Endpoint)
	if err != nil {
		return errors.New("tcp endpoint must be in host:port format")
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("tcp endpoint must include a host")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("tcp endpoint must include a valid port")
	}
	if target.Trusted {
		return nil
	}

	return validateUntrustedHost(host)
}

func validateUntrustedHost(host string) error {
	normalizedHost := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if strings.EqualFold(normalizedHost, "localhost") {
		return errors.New("localhost targets are not allowed for runtime-created targets")
	}

	ip := net.ParseIP(normalizedHost)
	if ip == nil {
		return nil
	}
	if !isAllowedUntrustedIP(ip) {
		return errors.New("private, loopback, link-local, and special-use IPs are not allowed for runtime-created targets")
	}
	return nil
}

func isAllowedUntrustedIP(ip net.IP) bool {
	for _, network := range blockedIPNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedIPNetworks = mustParseCIDRs(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"fe80::/10",
	"fc00::/7",
	"ff00::/8",
	"2001:db8::/32",
)

func mustParseCIDRs(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}
