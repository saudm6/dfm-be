// Package config loads the service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the configuration of the server. It is parsed once at startup.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server, such as ":8080".
	HTTPAddr string `env:"DFM_HTTP_ADDR,required,notEmpty"`
	Session
	Base
}

// Session configures sign-in sessions.
type Session struct {
	// SessionIdleTimeout ends a session after this long without a request.
	SessionIdleTimeout time.Duration `env:"DFM_SESSION_IDLE_TIMEOUT" envDefault:"12h"`
	// SessionMaxAge ends a session this long after sign-in, however active.
	SessionMaxAge time.Duration `env:"DFM_SESSION_MAX_AGE" envDefault:"168h"`
	// CookieSecure sets the Secure attribute on the session cookie, so
	// clients send it over https only. Set DFM_COOKIE_SECURE=false to sign
	// in over plain http in local development; `make run` does.
	CookieSecure bool `env:"DFM_COOKIE_SECURE" envDefault:"true"`
}

// Base is the configuration every command needs, including migrate.
type Base struct {
	// LogLevel is the minimum slog level: DEBUG, INFO, WARN or ERROR.
	LogLevel slog.Level `env:"DFM_LOG_LEVEL" envDefault:"INFO"`
	// DatabaseURL is the Postgres connection URL, such as
	// "postgres://dfm:dfm@localhost:5432/dfm?sslmode=disable".
	DatabaseURL string `env:"DFM_DATABASE_URL,required,notEmpty"`
}

// Load parses the server configuration from environ, given as "KEY=value"
// pairs like os.Environ returns. A missing or empty required variable fails
// with an error that names it.
func Load(environ []string) (Config, error) {
	cfg, err := parse[Config](environ)
	if err != nil {
		return Config{}, err
	}
	if cfg.SessionIdleTimeout <= 0 || cfg.SessionMaxAge <= 0 {
		return Config{}, errors.New("config: DFM_SESSION_IDLE_TIMEOUT and DFM_SESSION_MAX_AGE must be positive")
	}
	return cfg, nil
}

// LoadBase parses only the variables in Base from environ.
func LoadBase(environ []string) (Base, error) {
	return parse[Base](environ)
}

func parse[T any](environ []string) (T, error) {
	cfg, err := env.ParseAsWithOptions[T](env.Options{Environment: env.ToMap(environ)})
	if err != nil {
		var zero T
		return zero, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
