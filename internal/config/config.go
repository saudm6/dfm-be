// Package config loads the service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/saudm6/dfm-be/internal/calendar"
)

// Config is the configuration of the server. It is parsed once at startup.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server, such as ":8080".
	HTTPAddr string `env:"DFM_HTTP_ADDR,required,notEmpty"`
	Session
	Base
	Market Market
	DFM    DFM
}

// DFM is where the service reads DFM's public web API.
type DFM struct {
	// APIBaseURL is the origin of the API.
	APIBaseURL string `env:"DFM_API_BASE_URL" envDefault:"https://api2.dfm.ae"`
}

// Sync is the configuration of `dfm sync`.
type Sync struct {
	Base
	DFM DFM
}

// Market is the DFM trading day in Asia/Dubai time. The defaults are the
// session times dfm.ae publishes: pre-opening 09:30, trading 10:00,
// pre-closing 14:45, trade at last 14:55 to the 15:00 close, Monday to
// Friday.
type Market struct {
	PreOpen        calendar.TimeOfDay `env:"DFM_SESSION_PRE_OPEN" envDefault:"09:30"`
	Open           calendar.TimeOfDay `env:"DFM_SESSION_OPEN" envDefault:"10:00"`
	ClosingAuction calendar.TimeOfDay `env:"DFM_SESSION_CLOSING_AUCTION" envDefault:"14:45"`
	Close          calendar.TimeOfDay `env:"DFM_SESSION_CLOSE" envDefault:"15:00"`
	// TradingDays lists the days of the trading week, comma-separated.
	TradingDays []calendar.Weekday `env:"DFM_TRADING_DAYS" envDefault:"Mon,Tue,Wed,Thu,Fri"`
}

// Hours returns m as calendar hours.
func (m Market) Hours() calendar.Hours {
	week := make([]time.Weekday, len(m.TradingDays))
	for i, d := range m.TradingDays {
		week[i] = time.Weekday(d)
	}
	return calendar.Hours{PreOpen: m.PreOpen, Open: m.Open, ClosingAuction: m.ClosingAuction, Close: m.Close, Week: week}
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
// with an error that names it, and so do session times out of order.
func Load(environ []string) (Config, error) {
	cfg, err := parse[Config](environ)
	if err != nil {
		return Config{}, err
	}
	if cfg.SessionIdleTimeout <= 0 || cfg.SessionMaxAge <= 0 {
		return Config{}, errors.New("config: DFM_SESSION_IDLE_TIMEOUT and DFM_SESSION_MAX_AGE must be positive")
	}
	if err := cfg.Market.Hours().Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// LoadSync parses the variables of Sync from environ.
func LoadSync(environ []string) (Sync, error) {
	return parse[Sync](environ)
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
