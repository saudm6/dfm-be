// Command dfm runs the DFM trader terminal backend.
//
// With no arguments it migrates the database and serves the HTTP API.
// "dfm migrate" applies pending database migrations and exits.
// "dfm user create --username NAME --role owner|member" migrates the database
// and adds an account; it reads the password from the first line of stdin.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/config"
	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second
	usage           = "usage: dfm [migrate | user create --username NAME --role owner|member]"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Environ(), os.Stdin, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dfm:", err)
		os.Exit(1)
	}
}

// run executes the command named by args. It writes JSON logs to stdout.
func run(ctx context.Context, args, environ []string, stdin io.Reader, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		return serve(ctx, environ, stdout)
	case len(args) == 1 && args[0] == "migrate":
		return migrate(ctx, environ, stdout)
	case len(args) >= 2 && args[0] == "user" && args[1] == "create":
		return createUser(ctx, args[2:], environ, stdin, stdout)
	default:
		return errors.New(usage)
	}
}

// createUser migrates the database and adds the account described by args,
// with the password read from the first line of stdin.
func createUser(ctx context.Context, args, environ []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("dfm user create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	username := fs.String("username", "", "account name")
	roleName := fs.String("role", "", "owner or member")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New(usage)
	}
	if _, err := auth.NormalizeUsername(*username); err != nil {
		return err
	}
	role, err := auth.ParseRole(*roleName)
	if err != nil {
		return fmt.Errorf("--role must be owner or member, not %q", *roleName)
	}
	password, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read password from stdin: %w", err)
	}
	password = strings.TrimRight(password, "\r\n")

	cfg, err := config.LoadBase(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel)
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := applyMigrations(ctx, logger, pool); err != nil {
		return err
	}
	user, err := auth.NewService(pool, auth.Options{}).CreateUser(ctx, *username, password, role)
	if err != nil {
		return err
	}
	logger.Info("user created", "id", user.ID, "username", user.Username, "role", string(user.Role))
	return nil
}

// migrate applies pending migrations and exits.
func migrate(ctx context.Context, environ []string, stdout io.Writer) error {
	cfg, err := config.LoadBase(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel)
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return applyMigrations(ctx, logger, pool)
}

// serve migrates the database, then serves the HTTP API until ctx is
// cancelled and shuts down gracefully.
func serve(ctx context.Context, environ []string, stdout io.Writer) error {
	cfg, err := config.Load(environ)
	if err != nil {
		return err
	}
	logger := newLogger(stdout, cfg.LogLevel)

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := applyMigrations(ctx, logger, pool); err != nil {
		return err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	srv := &http.Server{
		Handler: httpapi.NewRouter(logger, pool, auth.NewService(pool, auth.Options{
			IdleTimeout:    cfg.SessionIdleTimeout,
			MaxAge:         cfg.SessionMaxAge,
			InsecureCookie: !cfg.CookieSecure,
		})),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info("http server listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down http server: %w", err)
	}
	return nil
}

func newLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

func applyMigrations(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) error {
	applied, err := store.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	logger.Info("database migrated", "applied", applied)
	return nil
}
