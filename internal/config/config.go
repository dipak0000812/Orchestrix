package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for Orchestrix.
// Only foundational runtime config lives here.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Shutdown ShutdownConfig `yaml:"shutdown"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type ShutdownConfig struct {
	Timeout Duration `yaml:"timeout"`
}

// Duration parses a human-readable Go duration from YAML, for example "30s".
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// Load reads configuration from a YAML file.
// This is intentionally simple and explicit.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port: %d", c.Server.Port)
	}

	if c.Shutdown.Timeout <= 0 {
		return fmt.Errorf("shutdown.timeout must be positive")
	}

	return nil
}
