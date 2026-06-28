package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	Storage   StorageConfig   `yaml:"storage"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

// RateLimitConfig controls the token-bucket rate limiters applied to write
// and unauthenticated endpoints.  All limits are zero-values-safe: when
// Enabled is false the middleware is bypassed entirely.
type RateLimitConfig struct {
	// Enabled activates rate limiting globally.  Defaults to true.
	Enabled bool `yaml:"enabled"`

	// SpacesWritesPerSec is the sustained token refill rate (tokens/second)
	// for authenticated Spaces write operations (send, edit, react, pin, …),
	// keyed per user ID (or IP when auth is disabled).
	SpacesWritesPerSec float64 `yaml:"spaces_writes_per_sec"`
	// SpacesWritesBurst is the maximum burst depth for Spaces writes.
	SpacesWritesBurst int `yaml:"spaces_writes_burst"`

	// BotAPIPerSec is the sustained refill rate for the bot REST API
	// (/api/bot/v1/*), keyed per bot token prefix.
	BotAPIPerSec float64 `yaml:"bot_api_per_sec"`
	// BotAPIBurst is the maximum burst depth for the bot API.
	BotAPIBurst int `yaml:"bot_api_burst"`

	// WebhookPerSec is the sustained refill rate for unauthenticated incoming
	// webhooks (/api/bot/hooks/*), keyed per source IP.
	WebhookPerSec float64 `yaml:"webhook_per_sec"`
	// WebhookBurst is the maximum burst depth for incoming webhooks.
	WebhookBurst int `yaml:"webhook_burst"`
}

type ServerConfig struct {
	Addr       string `yaml:"addr"`
	DataDir    string `yaml:"data_dir"`
	UploadsDir string `yaml:"uploads_dir"`
}

type AuthConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Password       string `yaml:"password"`
	MaxAttempts    int    `yaml:"max_attempts"`
	LockoutMinutes int    `yaml:"lockout_minutes"`
	SessionHours   int    `yaml:"session_hours"`
}

type StorageConfig struct {
	Type     string         `yaml:"type"`
	Postgres PostgresConfig `yaml:"postgres"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Default(), nil
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Addr:       ":8080",
			DataDir:    "./data",
			UploadsDir: "./uploads",
		},
		Auth: AuthConfig{
			Enabled:        false,
			Password:       "",
			MaxAttempts:    5,
			LockoutMinutes: 15,
			SessionHours:   24,
		},
		Storage: StorageConfig{
			Type: "local",
			Postgres: PostgresConfig{
				Host:    "localhost",
				Port:    5432,
				SSLMode: "disable",
			},
		},
		RateLimit: RateLimitConfig{
			Enabled: true,
			// Spaces writes: 10 messages/second sustained, burst of 30.
			// Generous enough for power users; tight enough to block floods.
			SpacesWritesPerSec: 10,
			SpacesWritesBurst:  30,
			// Bot API: 20 calls/second sustained, burst of 60.
			// Bots often send bursts on startup; higher ceiling than human writes.
			BotAPIPerSec: 20,
			BotAPIBurst:  60,
			// Incoming webhooks: 2/second sustained, burst of 10.
			// Unauthenticated — conservative to resist abuse from unknown sources.
			WebhookPerSec: 2,
			WebhookBurst:  10,
		},
	}
}
