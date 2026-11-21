package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// testEncryptionKey is base64 of 32 zero bytes — a dummy, valid AES-256 key.
const testEncryptionKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func minimal() map[string]string {
	return map[string]string{
		"POSTGRES_USER":          "webhook",
		"POSTGRES_PASSWORD":      "secret",
		"POSTGRES_DB":            "webhook_platform",
		"JWT_SECRET":             "test-jwt-secret-at-least-32-bytes-long",
		"SECRETS_ENCRYPTION_KEY": testEncryptionKey,
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     func() map[string]string
		check   func(t *testing.T, c Config)
		wantErr []string
	}{
		{
			name: "defaults",
			env:  minimal,
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.App.Env != "local" || c.App.ShutdownTimeout != 15*time.Second {
					t.Errorf("app defaults = %+v", c.App)
				}
				if c.HTTP.Port != 8080 || c.Metrics.Addr != ":9100" {
					t.Errorf("http/metrics defaults = %+v %+v", c.HTTP, c.Metrics)
				}
				if got := c.Postgres.DSN(); got != "postgres://webhook:secret@localhost:5432/webhook_platform?sslmode=disable" {
					t.Errorf("DSN = %q", got)
				}
				if len(c.Kafka.Brokers) != 1 || c.Kafka.Brokers[0] != "localhost:9092" {
					t.Errorf("brokers = %v", c.Kafka.Brokers)
				}
				if c.Auth.AccessTTL != 15*time.Minute || c.Auth.RefreshTTL != 168*time.Hour {
					t.Errorf("auth ttls = %v %v", c.Auth.AccessTTL, c.Auth.RefreshTTL)
				}
				if string(c.Auth.JWTSecret) != "test-jwt-secret-at-least-32-bytes-long" {
					t.Errorf("jwt secret not loaded")
				}
				if len(c.Secrets.EncryptionKey) != 32 {
					t.Errorf("encryption key len = %d, want 32", len(c.Secrets.EncryptionKey))
				}
				if c.Ingest.MaxPayloadBytes != 262144 || c.Ingest.RateLimitPerMin != 600 {
					t.Errorf("ingest defaults = %+v", c.Ingest)
				}
				if c.Outbox.PollInterval != 200*time.Millisecond || c.Outbox.BatchSize != 100 {
					t.Errorf("outbox defaults = %+v", c.Outbox)
				}
			},
		},
		{
			name: "short jwt secret",
			env: func() map[string]string {
				m := minimal()
				m["JWT_SECRET"] = "too-short"
				return m
			},
			wantErr: []string{"JWT_SECRET must be at least 32 bytes"},
		},
		{
			name: "encryption key not base64",
			env: func() map[string]string {
				m := minimal()
				m["SECRETS_ENCRYPTION_KEY"] = "not base64!!"
				return m
			},
			wantErr: []string{"SECRETS_ENCRYPTION_KEY must be 32 bytes base64"},
		},
		{
			name: "encryption key wrong length",
			env: func() map[string]string {
				m := minimal()
				m["SECRETS_ENCRYPTION_KEY"] = "AAAAAAAAAAAAAAAAAAAAAA=="
				return m
			},
			wantErr: []string{"SECRETS_ENCRYPTION_KEY must be 32 bytes base64"},
		},
		{
			name: "zero outbox batch",
			env: func() map[string]string {
				m := minimal()
				m["OUTBOX_BATCH_SIZE"] = "0"
				return m
			},
			wantErr: []string{"OUTBOX_BATCH_SIZE must be positive"},
		},
		{
			name: "overrides and broker list",
			env: func() map[string]string {
				m := minimal()
				m["APP_ENV"] = "aws"
				m["SHUTDOWN_TIMEOUT"] = "5s"
				m["KAFKA_BROKERS"] = " a:9092, b:9092 ,"
				m["POSTGRES_MAX_CONNS"] = "50"
				return m
			},
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.App.Env != "aws" || c.App.ShutdownTimeout != 5*time.Second {
					t.Errorf("app = %+v", c.App)
				}
				if strings.Join(c.Kafka.Brokers, "|") != "a:9092|b:9092" {
					t.Errorf("brokers = %v", c.Kafka.Brokers)
				}
				if c.Postgres.MaxConns != 50 {
					t.Errorf("max conns = %d", c.Postgres.MaxConns)
				}
			},
		},
		{
			name:    "missing required values are all reported",
			env:     func() map[string]string { return map[string]string{} },
			wantErr: []string{"POSTGRES_USER is required", "POSTGRES_PASSWORD is required", "POSTGRES_DB is required", "JWT_SECRET is required", "SECRETS_ENCRYPTION_KEY is required"},
		},
		{
			name: "malformed values",
			env: func() map[string]string {
				m := minimal()
				m["API_PORT"] = "eighty"
				m["SHUTDOWN_TIMEOUT"] = "15"
				m["APP_ENV"] = "prod"
				m["LOG_LEVEL"] = "trace"
				return m
			},
			wantErr: []string{"API_PORT", "SHUTDOWN_TIMEOUT", "APP_ENV must be one of", "LOG_LEVEL must be one of"},
		},
		{
			name: "pool bounds",
			env: func() map[string]string {
				m := minimal()
				m["POSTGRES_MAX_CONNS"] = "1"
				m["POSTGRES_MIN_CONNS"] = "2"
				return m
			},
			wantErr: []string{"POSTGRES_MAX_CONNS must be >= POSTGRES_MIN_CONNS"},
		},
		{
			name: "empty broker list",
			env: func() map[string]string {
				m := minimal()
				m["KAFKA_BROKERS"] = " , "
				return m
			},
			wantErr: []string{"KAFKA_BROKERS must not be empty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(env(tt.env()))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, cfg)
		})
	}
}
