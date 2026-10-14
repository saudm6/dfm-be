package store_test

import (
	"slices"
	"strings"
	"testing"

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

	first, err := store.Migrate(t.Context(), pool)
	if err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if want := []int64{20261015120000}; !slices.Equal(first, want) {
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
