package storetest

import (
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/saudm6/dfm-be/migrations"
)

// MigrationVersions returns the version of every migration in package
// migrations, oldest first: the versions a migration of an empty database
// applies.
func MigrationVersions(t testing.TB) []int64 {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("storetest: list migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("storetest: no migrations found")
	}
	versions := make([]int64, 0, len(names))
	for _, name := range names {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Fatalf("storetest: migration %s has no version prefix", name)
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("storetest: migration %s: %v", name, err)
		}
		versions = append(versions, v)
	}
	slices.Sort(versions)
	return versions
}
