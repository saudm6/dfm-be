package documents_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/documents"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/storetest"
)

func TestParseName(t *testing.T) {
	tests := []struct {
		in    string
		valid bool
	}{
		{in: "Morning", valid: true},
		{in: "a", valid: true},
		{in: "9", valid: true},
		{in: "My layout 2", valid: true},
		{in: "v1.2_final-B", valid: true},
		{in: "x.", valid: true},
		{in: strings.Repeat("a", 64), valid: true},
		{in: strings.Repeat("a", 65)},
		{in: ""},
		{in: " lead"},
		{in: "trail "},
		{in: ".."},
		{in: "-x"},
		{in: "a/b"},
		{in: "a%20b"},
		{in: "naïve"},
		{in: "tab\there"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := documents.ParseName(tt.in)
			if !tt.valid {
				if err == nil || !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("ParseName(%q) error = %v, want one wrapping domain.ErrInvalid", tt.in, err)
				}
				return
			}
			if err != nil || got != tt.in {
				t.Fatalf("ParseName(%q) = %q, %v, want %q, nil", tt.in, got, err, tt.in)
			}
		})
	}
}

func TestParseIfMatch(t *testing.T) {
	tests := []struct {
		in      string
		want    documents.IfMatch
		wantErr bool
	}{
		{in: `*`, want: documents.IfMatch{Any: true}},
		{in: ` * `, want: documents.IfMatch{Any: true}},
		{in: `"abc"`, want: documents.IfMatch{ETags: []string{`"abc"`}}},
		{in: `"a", "b",	"c"`, want: documents.IfMatch{ETags: []string{`"a"`, `"b"`, `"c"`}}},
		{in: `"a,b"`, want: documents.IfMatch{ETags: []string{`"a,b"`}}},
		{in: `W/"weak", "strong"`, want: documents.IfMatch{ETags: []string{`"strong"`}}},
		{in: `W/"weak"`, want: documents.IfMatch{ETags: []string{}}},
		{in: `""`, want: documents.IfMatch{ETags: []string{`""`}}},
		{in: ``, wantErr: true},
		{in: `abc`, wantErr: true},
		{in: `"abc`, wantErr: true},
		{in: `"a" "b"`, wantErr: true},
		{in: `"a",`, wantErr: true},
		{in: `*, "a"`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := documents.ParseIfMatch(tt.in)
			if tt.wantErr {
				if err == nil || !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("ParseIfMatch(%q) error = %v, want one wrapping domain.ErrInvalid", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseIfMatch(%q) error = %v", tt.in, err)
			}
			if got.Any != tt.want.Any || !slices.Equal(got.ETags, tt.want.ETags) {
				t.Errorf("ParseIfMatch(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// fixture is a store on a fresh database with the users "saud" and "amal",
// whose clock reads t0 until moved.
type fixture struct {
	store      *documents.Store
	now        *time.Time
	saud, amal int64
	exec       func(sql string)
}

var t0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := storetest.NewPool(t)
	now := t0
	accounts := auth.NewService(pool, auth.Options{})
	ids := map[string]int64{}
	for _, name := range []string{"saud", "amal"} {
		u, err := accounts.CreateUser(t.Context(), name, "correct horse battery", auth.Member)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = u.ID
	}
	return fixture{
		store: documents.NewStore(pool, func() time.Time { return now }),
		now:   &now,
		saud:  ids["saud"],
		amal:  ids["amal"],
		exec: func(sql string) {
			if _, err := pool.Exec(t.Context(), sql); err != nil {
				t.Fatal(err)
			}
		},
	}
}

const (
	layout = domain.DocumentKindLayout
	// etagV1 is the entity tag of schema version 1 with body {"panes":[1,2]}.
	etagV1 = `"0fb3fad7ded9711f4674bc1dcfaf1e2b"`
	// etagV2 is the entity tag of schema version 2 with the same body.
	etagV2 = `"66139c6db8fdce82ed8ccf2371011d6f"`
	// etagPanes3 is the entity tag of schema version 1 with body {"panes":[3]}.
	etagPanes3 = `"f3558a517958a17752040d4fa606d938"`
)

func TestStoreLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	info, err := f.store.Create(ctx, f.saud, layout, "Morning", 1, json.RawMessage("{ \"panes\" : [1, 2] }"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if want := (documents.Info{Name: "Morning", ETag: etagV1, UpdatedAt: t0}); !info.UpdatedAt.Equal(want.UpdatedAt) || info.Name != want.Name || info.ETag != want.ETag {
		t.Errorf("Create() = %+v, want %+v", info, want)
	}
	if _, err := f.store.Create(ctx, f.saud, layout, "Morning", 1, json.RawMessage(`{}`)); !errors.Is(err, documents.ErrExists) {
		t.Errorf("Create() of an existing document error = %v, want ErrExists", err)
	}

	doc, err := f.store.Get(ctx, f.saud, layout, "Morning")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if doc.SchemaVersion != 1 || string(doc.Body) != `{"panes":[1,2]}` || doc.ETag != etagV1 {
		t.Errorf("Get() = {%d %s %s}, want {1 {\"panes\":[1,2]} %s}", doc.SchemaVersion, doc.Body, doc.ETag, etagV1)
	}
	if _, err := f.store.Get(ctx, f.saud, domain.DocumentKindChartTemplate, "Morning"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("Get() of another kind error = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Get(ctx, f.saud, layout, "morning"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("Get() with other case error = %v, want ErrNotFound", err)
	}

	*f.now = t0.Add(time.Minute)
	stale := documents.IfMatch{ETags: []string{`"0000"`}}
	if _, err := f.store.Update(ctx, f.saud, layout, "Morning", stale, 1, json.RawMessage(`{"panes":[3]}`)); !errors.Is(err, documents.ErrPreconditionFailed) {
		t.Errorf("Update() with a stale tag error = %v, want ErrPreconditionFailed", err)
	}
	info, err = f.store.Update(ctx, f.saud, layout, "Morning", documents.IfMatch{ETags: []string{`"0000"`, etagV1}}, 2, json.RawMessage(`{"panes":[1,2]}`))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if info.ETag != etagV2 || !info.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("Update() = %+v, want ETag %s at %v", info, etagV2, t0.Add(time.Minute))
	}
	if _, err := f.store.Update(ctx, f.saud, layout, "Morning", documents.IfMatch{ETags: []string{etagV1}}, 1, json.RawMessage(`{"panes":[3]}`)); !errors.Is(err, documents.ErrPreconditionFailed) {
		t.Errorf("Update() with the replaced tag error = %v, want ErrPreconditionFailed", err)
	}
	if info, err = f.store.Update(ctx, f.saud, layout, "Morning", documents.IfMatch{Any: true}, 1, json.RawMessage(`{"panes":[3]}`)); err != nil || info.ETag != etagPanes3 {
		t.Errorf("Update(*) = %+v, %v, want ETag %s", info, err, etagPanes3)
	}
	if _, err := f.store.Update(ctx, f.saud, layout, "Evening", documents.IfMatch{Any: true}, 1, json.RawMessage(`{}`)); !errors.Is(err, documents.ErrPreconditionFailed) {
		t.Errorf("Update(*) of a missing document error = %v, want ErrPreconditionFailed", err)
	}

	if err := f.store.Delete(ctx, f.saud, layout, "Morning", documents.IfMatch{ETags: []string{etagV2}}); !errors.Is(err, documents.ErrPreconditionFailed) {
		t.Errorf("Delete() with a stale tag error = %v, want ErrPreconditionFailed", err)
	}
	if err := f.store.Delete(ctx, f.saud, layout, "Morning", documents.IfMatch{ETags: []string{etagPanes3}}); err != nil {
		t.Errorf("Delete() error = %v", err)
	}
	if err := f.store.Delete(ctx, f.saud, layout, "Morning", documents.IfMatch{Any: true}); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("Delete() of a deleted document error = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Get(ctx, f.saud, layout, "Morning"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("Get() of a deleted document error = %v, want ErrNotFound", err)
	}
}

func TestStoreKeepsUsersApart(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	if _, err := f.store.Create(ctx, f.saud, layout, "Morning", 1, json.RawMessage(`{"panes":[1,2]}`)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.Get(ctx, f.amal, layout, "Morning"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("Get() of another user's document error = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Update(ctx, f.amal, layout, "Morning", documents.IfMatch{ETags: []string{etagV1}}, 1, json.RawMessage(`{}`)); !errors.Is(err, documents.ErrPreconditionFailed) {
		t.Errorf("Update() of another user's document error = %v, want ErrPreconditionFailed", err)
	}
	if err := f.store.Delete(ctx, f.amal, layout, "Morning", documents.IfMatch{Any: true}); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("Delete() of another user's document error = %v, want ErrNotFound", err)
	}
	if list, err := f.store.List(ctx, f.amal, layout); err != nil || len(list) != 0 {
		t.Errorf("List() for amal = %+v, %v, want none", list, err)
	}

	if _, err := f.store.Create(ctx, f.amal, layout, "Morning", 1, json.RawMessage(`{"panes":[3]}`)); err != nil {
		t.Fatalf("Create() of the same name for another user error = %v", err)
	}
	doc, err := f.store.Get(ctx, f.saud, layout, "Morning")
	if err != nil || string(doc.Body) != `{"panes":[1,2]}` {
		t.Errorf("Get() for saud = %s, %v, want {\"panes\":[1,2]}", doc.Body, err)
	}

	f.exec(`DELETE FROM users WHERE username = 'amal'`)
	if list, err := f.store.List(ctx, f.amal, layout); err != nil || len(list) != 0 {
		t.Errorf("List() for a deleted user = %+v, %v, want none", list, err)
	}
	if list, err := f.store.List(ctx, f.saud, layout); err != nil || len(list) != 1 {
		t.Errorf("List() for saud after deleting amal = %+v, %v, want 1 document", list, err)
	}
}

func TestStoreList(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	for i, name := range []string{"b", "a", "C"} {
		*f.now = t0.Add(time.Duration(i) * time.Hour)
		if _, err := f.store.Create(ctx, f.saud, layout, name, 1, json.RawMessage(`{"panes":[1,2]}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.Create(ctx, f.saud, domain.DocumentKindChartTemplate, "x", 1, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.List(ctx, f.saud, layout)
	if err != nil {
		t.Fatal(err)
	}
	want := []documents.Info{
		{Name: "C", ETag: etagV1, UpdatedAt: t0.Add(2 * time.Hour)},
		{Name: "a", ETag: etagV1, UpdatedAt: t0.Add(time.Hour)},
		{Name: "b", ETag: etagV1, UpdatedAt: t0},
	}
	if !slices.EqualFunc(got, want, func(a, b documents.Info) bool {
		return a.Name == b.Name && a.ETag == b.ETag && a.UpdatedAt.Equal(b.UpdatedAt)
	}) {
		t.Errorf("List() = %+v, want %+v", got, want)
	}
}

func TestStoreRejectsInvalidWrites(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name          string
		kind          domain.DocumentKind
		docName       string
		schemaVersion int32
		body          string
		wantErr       string
	}{
		{name: "array body", kind: layout, docName: "a", schemaVersion: 1, body: `[1]`, wantErr: "invalid body: must be a JSON object"},
		{name: "null body", kind: layout, docName: "a", schemaVersion: 1, body: `null`, wantErr: "invalid body: must be a JSON object"},
		{name: "no body", kind: layout, docName: "a", schemaVersion: 1, body: ``, wantErr: "invalid body: must be a JSON object"},
		{name: "bad JSON", kind: layout, docName: "a", schemaVersion: 1, body: `{"a":}`, wantErr: "invalid body: must be a JSON object"},
		{name: "two values", kind: layout, docName: "a", schemaVersion: 1, body: `{} {}`, wantErr: "invalid body: must be a JSON object"},
		{name: "too large", kind: layout, docName: "a", schemaVersion: 1, body: `{"a":"` + strings.Repeat("x", documents.MaxBodyBytes) + `"}`, wantErr: "invalid body: larger than 262144 bytes"},
		{name: "zero schema version", kind: layout, docName: "a", schemaVersion: 0, body: `{}`, wantErr: "invalid schemaVersion: must be a positive integer, got 0"},
		{name: "negative schema version", kind: layout, docName: "a", schemaVersion: -1, body: `{}`, wantErr: "invalid schemaVersion: must be a positive integer, got -1"},
		{name: "unknown kind", kind: "notes", docName: "a", schemaVersion: 1, body: `{}`, wantErr: `invalid document kind: unknown value "notes"`},
		{name: "bad name", kind: layout, docName: "a/b", schemaVersion: 1, body: `{}`, wantErr: `invalid document name: "a/b" must be 1 to 64 characters of A-Z, a-z, 0-9, space, '_', '.' and '-', starting with a letter or digit and not ending with a space`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.store.Create(t.Context(), f.saud, tt.kind, tt.docName, tt.schemaVersion, json.RawMessage(tt.body))
			if err == nil || err.Error() != tt.wantErr || !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("Create() error = %v, want %q wrapping domain.ErrInvalid", err, tt.wantErr)
			}
			_, err = f.store.Update(t.Context(), f.saud, tt.kind, tt.docName, documents.IfMatch{Any: true}, tt.schemaVersion, json.RawMessage(tt.body))
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("Update() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
	if list, err := f.store.List(t.Context(), f.saud, layout); err != nil || len(list) != 0 {
		t.Errorf("List() after invalid writes = %+v, %v, want none", list, err)
	}
}

func TestStoreKeepsBodyAsWritten(t *testing.T) {
	f := newFixture(t)
	body := `{"z":1,"a":{"n":12345678901234567890.5,"s":"<\u0000>"},"z":2}`
	if _, err := f.store.Create(t.Context(), f.saud, layout, "raw", 1, json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
	doc, err := f.store.Get(t.Context(), f.saud, layout, "raw")
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Body) != body {
		t.Errorf("Get() body = %s, want %s", doc.Body, body)
	}
}
