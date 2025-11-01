package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config describes gateway configuration loaded from YAML.
// The structure mirrors the layout of config.yaml and is used across
// the gateway to control server behavior and service discovery.
type Config struct {
	Server   ServerConfig    `yaml:"server"`
	Etcd     EtcdConfig      `yaml:"etcd"`
	Services []ServiceConfig `yaml:"services"`
}

// ServerConfig contains HTTP server settings that shape the listener.
type ServerConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"readTimeout"`
	WriteTimeout time.Duration `yaml:"writeTimeout"`
	IdleTimeout  time.Duration `yaml:"idleTimeout"`
}

// EtcdConfig holds etcd client connection parameters; the gateway
// shares a single client for all service discovery operations.
type EtcdConfig struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dialTimeout"`
	Username    string        `yaml:"username"`
	Password    string        `yaml:"password"`
}

// ServiceConfig defines how the gateway maps incoming path prefixes to
// downstream services and how those services should be discovered.
type ServiceConfig struct {
	Name            string   `yaml:"name"`
	Scheme          string   `yaml:"scheme"`
	DiscoveryPrefix string   `yaml:"discoveryPrefix"`
	Prefixes        []string `yaml:"prefixes"`
}

// LoadConfig reads and validates gateway configuration from disk.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() error {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 5 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 10 * time.Second
	}
	if c.Server.IdleTimeout == 0 {
		c.Server.IdleTimeout = 60 * time.Second
	}

	if len(c.Etcd.Endpoints) == 0 {
		return fmt.Errorf("etcd endpoints must be configured")
	}
	if c.Etcd.DialTimeout == 0 {
		c.Etcd.DialTimeout = 5 * time.Second
	}

	if len(c.Services) == 0 {
		return fmt.Errorf("at least one service must be configured")
	}

	for i := range c.Services {
		if err := c.Services[i].applyDefaults(); err != nil {
			return fmt.Errorf("service %d config invalid: %w", i, err)
		}
	}

	return nil
}

func (s *ServiceConfig) applyDefaults() error {
	if s.Name == "" {
		return fmt.Errorf("service name cannot be empty")
	}
	if s.Scheme == "" {
		s.Scheme = "http"
	}
	if !strings.HasPrefix(s.DiscoveryPrefix, "/") {
		s.DiscoveryPrefix = "/" + strings.TrimPrefix(s.DiscoveryPrefix, "/")
	}
	if len(s.Prefixes) == 0 {
		return fmt.Errorf("service %s must declare at least one path prefix", s.Name)
	}
	for i, prefix := range s.Prefixes {
		if prefix == "" {
			return fmt.Errorf("service %s has empty path prefix", s.Name)
		}
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		// Normalize duplicated slashes to avoid mismatch issues.
		prefix = strings.ReplaceAll(prefix, "//", "/")
		s.Prefixes[i] = prefix
	}
	return nil
}
