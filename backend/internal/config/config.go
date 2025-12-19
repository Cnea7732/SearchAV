package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig `mapstructure:"server"`
	Log     LogConfig    `mapstructure:"log"`
	Auth    AuthConfig   `mapstructure:"auth"`
	Source  SourceConfig `mapstructure:"source"`
	Sources []SourceItem `mapstructure:"sources"`
}

type AuthConfig struct {
	Enabled   bool           `mapstructure:"enabled"`
	Passwords []PasswordItem `mapstructure:"passwords"`
}

type PasswordItem struct {
	Password string `mapstructure:"password"`
	Adult    bool   `mapstructure:"adult"`
}

// AuthResult contains the result of password validation
type AuthResult struct {
	Valid bool
	Adult bool
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type SourceConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
	Retry   int           `mapstructure:"retry"`
}

type SourceItem struct {
	Code    string `mapstructure:"code"`
	Name    string `mapstructure:"name"`
	URL     string `mapstructure:"url"`
	Enabled bool   `mapstructure:"enabled"`
	Adult   bool   `mapstructure:"adult"`
}

// New loads configuration from file or environment variable
func New() (*Config, error) {
	viper.SetConfigType("yaml")

	// Check for CONFIG_LOCAL environment variable (base64 encoded, for Fly.io Secrets)
	if configBase64 := os.Getenv("CONFIG_LOCAL"); configBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(configBase64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CONFIG_LOCAL: %w", err)
		}
		if err := viper.ReadConfig(strings.NewReader(string(decoded))); err != nil {
			return nil, fmt.Errorf("failed to parse CONFIG_LOCAL: %w", err)
		}
	} else {
		// Load from files
		viper.SetConfigName("config")
		viper.AddConfigPath("./configs")

		if err := viper.ReadInConfig(); err != nil {
			return nil, err
		}

		// Merge local config if exists (for sensitive data like sources)
		viper.SetConfigName("config.local")
		_ = viper.MergeInConfig() // Ignore error if not exists
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Validate config
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validate checks config for errors
func (c *Config) validate() error {
	// Check for duplicate source codes
	seen := make(map[string]bool)
	for _, s := range c.Sources {
		if seen[s.Code] {
			return fmt.Errorf("duplicate source code: %s", s.Code)
		}
		seen[s.Code] = true
	}
	return nil
}

// GetEnabledSources returns enabled sources
func (c *Config) GetEnabledSources() []SourceItem {
	var enabled []SourceItem
	for _, s := range c.Sources {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	return enabled
}

// GetSourceByCode returns a source by its code
func (c *Config) GetSourceByCode(code string) (*SourceItem, bool) {
	for _, s := range c.Sources {
		if s.Code == code {
			return &s, true
		}
	}
	return nil, false
}

// ValidatePassword checks if the password is in the whitelist and returns auth result
func (c *Config) ValidatePassword(password string) AuthResult {
	// If auth is disabled, allow everything including adult
	if !c.Auth.Enabled {
		return AuthResult{Valid: true, Adult: true}
	}
	for _, p := range c.Auth.Passwords {
		if p.Password == password {
			return AuthResult{Valid: true, Adult: p.Adult}
		}
	}
	return AuthResult{Valid: false, Adult: false}
}
