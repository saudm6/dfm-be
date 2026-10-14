// Package config loads the service configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"

	"github.com/caarlos0/env/v11"
)

// Config is the service configuration. It is parsed once at startup.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server, such as ":8080".
	HTTPAddr string `env:"DFM_HTTP_ADDR,required,notEmpty"`
	// LogLevel is the minimum slog level: DEBUG, INFO, WARN or ERROR.
	LogLevel slog.Level `env:"DFM_LOG_LEVEL" envDefault:"INFO"`
}

// Load parses environ, given as "KEY=value" pairs like os.Environ returns.
// A missing or empty required variable fails with an error that names it.
func Load(environ []string) (Config, error) {
	cfg, err := env.ParseAsWithOptions[Config](env.Options{Environment: env.ToMap(environ)})
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
