package store_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saudm6/dfm-be/internal/store"
	"github.com/saudm6/dfm-be/internal/store/dbgen"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

func TestMigrateAppliesEveryMigrationOnce(t *testing.T) {
	pool, err := store.Open(t.Context(), storetest.NewURL(t))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()
	if _, err := dbgen.New(pool).TimescaleDBVersion(t.Context()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("TimescaleDBVersion() before Migrate error = %v, want %v", err, pgx.ErrNoRows)
	}

	first, err := store.Migrate(t.Context(), pool)
	if err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if want := []int64{20261015120000, 20261015125900}; !slices.Equal(first, want) {
		t.Errorf("first Migrate() applied %v, want %v", first, want)
	}

	second, err := store.Migrate(t.Context(), pool)
	if err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second Migrate() applied %v, want none", second)
	}
}

func migration(up, down string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- +goose Up\n" + up + "\n\n-- +goose Down\n" + down + "\n")}
}

func openEmpty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := store.Open(t.Context(), storetest.NewURL(t))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func appliedVersions(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()
	rows, err := pool.Query(t.Context(),
		"SELECT version_id FROM goose_db_version WHERE version_id > 0 ORDER BY version_id")
	if err != nil {
		t.Fatalf("query goose_db_version: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatalf("collect goose_db_version: %v", err)
	}
	return got
}

func TestMigrateFSAppliesOutOfOrderMigration(t *testing.T) {
	pool := openEmpty(t)
	later := migration("CREATE TABLE later (id int);", "DROP TABLE later;")
	earlier := migration("CREATE TABLE earlier (id int);", "DROP TABLE earlier;")

	first, err := store.MigrateFS(t.Context(), pool, fstest.MapFS{
		"20261016100000_later.sql": later,
	})
	if err != nil {
		t.Fatalf("first MigrateFS() error = %v", err)
	}
	if want := []int64{20261016100000}; !slices.Equal(first, want) {
		t.Errorf("first MigrateFS() applied %v, want %v", first, want)
	}

	second, err := store.MigrateFS(t.Context(), pool, fstest.MapFS{
		"20261016090000_earlier.sql": earlier,
		"20261016100000_later.sql":   later,
	})
	if err != nil {
		t.Fatalf("second MigrateFS() error = %v", err)
	}
	if want := []int64{20261016090000}; !slices.Equal(second, want) {
		t.Errorf("second MigrateFS() applied %v, want %v", second, want)
	}

	if got, want := appliedVersions(t, pool), []int64{20261016090000, 20261016100000}; !slices.Equal(got, want) {
		t.Errorf("goose_db_version = %v, want %v", got, want)
	}
	for _, table := range []string{"earlier", "later"} {
		if _, err := pool.Exec(t.Context(), "SELECT id FROM "+table); err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}

func TestMigrateFSSerialisesConcurrentCallers(t *testing.T) {
	pool := openEmpty(t)
	fsys := fstest.MapFS{
		"20261016090000_create.sql": migration("CREATE TABLE counter (n int);", "DROP TABLE counter;"),
		"20261016100000_insert.sql": migration("INSERT INTO counter VALUES (1);", "DELETE FROM counter;"),
	}
	const callers = 6

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		applied []int64
		errs    []error
	)
	for range callers {
		wg.Go(func() {
			got, err := store.MigrateFS(t.Context(), pool, fsys)
			mu.Lock()
			defer mu.Unlock()
			applied = append(applied, got...)
			errs = append(errs, err)
		})
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		t.Fatalf("MigrateFS() errors: %v", err)
	}
	slices.Sort(applied)
	if want := []int64{20261016090000, 20261016100000}; !slices.Equal(applied, want) {
		t.Errorf("MigrateFS() applied %v across %d callers, want %v", applied, callers, want)
	}
	var rows int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM counter").Scan(&rows); err != nil {
		t.Fatalf("count counter: %v", err)
	}
	if rows != 1 {
		t.Errorf("counter rows = %d, want 1", rows)
	}
}

func TestStoretestDisablesTelemetry(t *testing.T) {
	pool := storetest.NewPool(t)

	var got string
	if err := pool.QueryRow(t.Context(), "SHOW timescaledb.telemetry_level").Scan(&got); err != nil {
		t.Fatalf("SHOW timescaledb.telemetry_level: %v", err)
	}
	if got != "off" {
		t.Errorf("timescaledb.telemetry_level = %q, want %q", got, "off")
	}
}

func TestPoolRunsTypedQuery(t *testing.T) {
	pool := storetest.NewPool(t)

	got, err := dbgen.New(pool).TimescaleDBVersion(t.Context())

	if err != nil {
		t.Fatalf("TimescaleDBVersion() error = %v", err)
	}
	if got != "2.30.2" {
		t.Errorf("TimescaleDBVersion() = %q, want %q", got, "2.30.2")
	}
}

func TestOpenErrors(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantPrefix string
	}{
		{
			name:       "malformed url",
			url:        "postgres://dfm:dfm@localhost:notaport/dfm",
			wantPrefix: "store: parse database url: ",
		},
		{
			name:       "nothing listening",
			url:        "postgres://dfm:dfm@127.0.0.1:1/dfm?sslmode=disable&connect_timeout=2",
			wantPrefix: "store: ping database: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := store.Open(t.Context(), tt.url)
			if err == nil {
				pool.Close()
				t.Fatalf("Open() error = nil, want prefix %q", tt.wantPrefix)
			}
			if !strings.HasPrefix(err.Error(), tt.wantPrefix) {
				t.Errorf("Open() error = %q, want prefix %q", err, tt.wantPrefix)
			}
		})
	}
}
