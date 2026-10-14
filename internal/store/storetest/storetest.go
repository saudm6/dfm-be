// Package storetest gives tests a fresh, empty database on a TimescaleDB
// container started with testcontainers-go. It needs a Docker daemon.
//
// The container starts once per test binary and is shared; every call gets its
// own database, dropped when the test ends. The testcontainers reaper removes
// the container when the test binary exits.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/saudm6/dfm-be/internal/store"
)

// Image is the database image, TimescaleDB on Postgres 17.
// Keep it in step with docker-compose.yml.
const Image = "timescale/timescaledb:2.30.2-pg17"

var (
	startOnce sync.Once
	adminURL  string
	startErr  error
	dbSeq     atomic.Int64
)

// NewPool returns a pool on a fresh database with every migration applied.
// The pool is closed and the database dropped when t ends.
func NewPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := store.Open(t.Context(), NewURL(t))
	if err != nil {
		t.Fatalf("storetest: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := store.Migrate(t.Context(), pool); err != nil {
		t.Fatalf("storetest: %v", err)
	}
	return pool
}

// NewURL returns the connection URL of a fresh database with no migrations
// applied. The database is dropped when t ends.
func NewURL(t testing.TB) string {
	t.Helper()
	base := start(t)
	name := fmt.Sprintf("test_%d", dbSeq.Add(1))
	admin(t, base, "CREATE DATABASE "+name)
	t.Cleanup(func() { admin(t, base, "DROP DATABASE "+name+" WITH (FORCE)") })

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("storetest: parse container url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func start(t testing.TB) string {
	t.Helper()
	startOnce.Do(func() {
		// The container outlives this test, so it must not use t.Context.
		ctx := context.Background()
		ctr, err := postgres.Run(ctx, Image,
			postgres.WithDatabase("dfm"),
			postgres.WithUsername("dfm"),
			postgres.WithPassword("dfm"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			startErr = fmt.Errorf("start %s: %w", Image, err)
			return
		}
		adminURL, startErr = ctr.ConnectionString(ctx, "sslmode=disable")
	})
	if startErr != nil {
		t.Fatalf("storetest: %v", startErr)
	}
	return adminURL
}

func admin(t testing.TB, dsn, sql string) {
	t.Helper()
	ctx := context.WithoutCancel(t.Context())
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("storetest: connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("storetest: %s: %v", sql, err)
	}
}
