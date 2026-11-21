package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the typed configuration shared by every process. Each process uses only the
// sections it needs; validation covers all of them so a bad .env fails at boot everywhere.
type Config struct {
	App      App
	HTTP     HTTP
	Metrics  Metrics
	Postgres Postgres
	Redis    Redis
	Kafka    Kafka
}

// App holds process-wide settings.
type App struct {
	Env             string
	Version         string
	LogLevel        string
	ShutdownTimeout time.Duration
}

// HTTP configures the api process's public server.
type HTTP struct {
	Port           int
	RequestTimeout time.Duration
}

// Metrics configures the /metrics, /healthz, /readyz server every process runs.
type Metrics struct {
	Addr string
}

// Postgres holds connection and pool settings.
type Postgres struct {
	Host            string
	Port            int
	User            string
	Password        string
	DB              string
	SSLMode         string
	MaxConns        int
	MinConns        int
	MaxConnLifetime time.Duration
}

// DSN returns a pgx connection string.
func (p Postgres) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.DB, p.SSLMode)
}

// Redis holds client settings.
type Redis struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

// Kafka holds client settings.
type Kafka struct {
	Brokers     []string
	TopicPrefix string
}

var validEnvs = map[string]bool{"local": true, "dev": true, "aws": true}

var validLogLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}

// Load reads configuration from the environment and validates it. It is the only place in the
// codebase that reads environment variables. All problems are reported at once.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

type lookupFunc func(key string) (string, bool)

func load(lookup lookupFunc) (Config, error) {
	r := reader{lookup: lookup}

	cfg := Config{
		App: App{
			Env:             r.str("APP_ENV", "local"),
			Version:         r.str("APP_VERSION", "dev"),
			LogLevel:        r.str("LOG_LEVEL", "info"),
			ShutdownTimeout: r.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		HTTP: HTTP{
			Port:           r.int("API_PORT", 8080),
			RequestTimeout: r.duration("REQUEST_TIMEOUT", 30*time.Second),
		},
		Metrics: Metrics{
			Addr: r.str("METRICS_ADDR", ":9100"),
		},
		Postgres: Postgres{
			Host:            r.str("POSTGRES_HOST", "localhost"),
			Port:            r.int("POSTGRES_PORT", 5432),
			User:            r.required("POSTGRES_USER"),
			Password:        r.required("POSTGRES_PASSWORD"),
			DB:              r.required("POSTGRES_DB"),
			SSLMode:         r.str("POSTGRES_SSLMODE", "disable"),
			MaxConns:        r.int("POSTGRES_MAX_CONNS", 20),
			MinConns:        r.int("POSTGRES_MIN_CONNS", 2),
			MaxConnLifetime: r.duration("POSTGRES_MAX_CONN_LIFETIME", 30*time.Minute),
		},
		Redis: Redis{
			Addr:     r.str("REDIS_ADDR", "localhost:6379"),
			Password: r.str("REDIS_PASSWORD", ""),
			DB:       r.int("REDIS_DB", 0),
			PoolSize: r.int("REDIS_POOL_SIZE", 20),
		},
		Kafka: Kafka{
			Brokers:     r.list("KAFKA_BROKERS", []string{"localhost:9092"}),
			TopicPrefix: r.str("KAFKA_TOPIC_PREFIX", ""),
		},
	}

	r.check(validEnvs[cfg.App.Env], "APP_ENV must be one of local, dev, aws")
	r.check(validLogLevels[cfg.App.LogLevel], "LOG_LEVEL must be one of debug, info, warn, error")
	r.check(cfg.App.ShutdownTimeout > 0, "SHUTDOWN_TIMEOUT must be positive")
	r.check(cfg.HTTP.Port > 0 && cfg.HTTP.Port < 65536, "API_PORT must be a valid port")
	r.check(cfg.Postgres.MaxConns >= cfg.Postgres.MinConns, "POSTGRES_MAX_CONNS must be >= POSTGRES_MIN_CONNS")
	r.check(cfg.Postgres.MinConns >= 0, "POSTGRES_MIN_CONNS must not be negative")
	r.check(cfg.Postgres.MaxConns <= 1000, "POSTGRES_MAX_CONNS must be <= 1000")
	r.check(len(cfg.Kafka.Brokers) > 0, "KAFKA_BROKERS must not be empty")

	if len(r.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(r.errs...))
	}
	return cfg, nil
}

type reader struct {
	lookup lookupFunc
	errs   []error
}

func (r *reader) check(ok bool, msg string) {
	if !ok {
		r.errs = append(r.errs, errors.New(msg))
	}
}

func (r *reader) str(key, def string) string {
	if v, ok := r.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (r *reader) required(key string) string {
	v, ok := r.lookup(key)
	if !ok || strings.TrimSpace(v) == "" {
		r.errs = append(r.errs, fmt.Errorf("%s is required", key))
		return ""
	}
	return strings.TrimSpace(v)
}

func (r *reader) int(key string, def int) int {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not an integer", key, v))
		return def
	}
	return n
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not a duration", key, v))
		return def
	}
	return d
}

func (r *reader) list(key string, def []string) []string {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
