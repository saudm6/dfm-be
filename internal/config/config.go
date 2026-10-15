// Package config loads the service configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"

	"github.com/caarlos0/env/v11"
)

// Config is the configuration of the server. It is parsed once at startup.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server, such as ":8080".
	HTTPAddr string `env:"DFM_HTTP_ADDR,required,notEmpty"`
	Base
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
	return parse[Config](environ)
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
