// Package documents keeps each user's documents, such as workspace layouts
// and chart templates, on the server so they follow the user to any browser.
//
// A document is identified by its owner, its kind, and its name. Its body is
// a JSON object the server does not interpret, tagged with the version of the
// client's schema for it. Its strong entity tag is a hash of the schema
// version and the body, so clients can update and delete it only if it is
// still the version they saw.
package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/store/dbgen"
)

// MaxBodyBytes caps the size of a document body.
const MaxBodyBytes = 256 << 10

var (
	// ErrNotFound means the user has no document of that kind and name.
	ErrNotFound = errors.New("documents: document not found")
	// ErrExists is returned by Create when the document already exists.
	ErrExists = errors.New("documents: document already exists")
	// ErrPreconditionFailed means the document does not exist or its entity
	// tag does not satisfy the If-Match precondition.
	ErrPreconditionFailed = errors.New("documents: precondition failed")
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9 _.-]{0,62}[A-Za-z0-9_.-])?$`)

// ParseName returns s if it is a document name: 1 to 64 characters of
// A-Z, a-z, 0-9, space, '_', '.' and '-', starting with a letter or digit and
// not ending with a space. Names are case-sensitive.
func ParseName(s string) (string, error) {
	if !namePattern.MatchString(s) {
		return "", fmt.Errorf("%w document name: %q must be 1 to 64 characters of A-Z, a-z, 0-9, space, '_', '.' and '-', starting with a letter or digit and not ending with a space", domain.ErrInvalid, s)
	}
	return s, nil
}

// Document is a stored document.
type Document struct {
	SchemaVersion int32
	Body          json.RawMessage
	// ETag is the quoted strong entity tag, ready for the ETag header.
	ETag string
}

// Info describes a stored document without its body.
type Info struct {
	Name string
	ETag string
	// UpdatedAt is the time of the last write, in UTC.
	UpdatedAt time.Time
}

// IfMatch is the precondition of an update or delete. It holds when the
// document exists and either Any is set or its entity tag is one of ETags.
type IfMatch struct {
	Any   bool
	ETags []string
}

var errBadIfMatch = fmt.Errorf("%w If-Match: must be * or a comma-separated list of quoted entity tags", domain.ErrInvalid)

// ParseIfMatch parses the value of an HTTP If-Match header: "*" or a
// comma-separated list of quoted entity tags. Weak tags (W/"...") are
// dropped, because If-Match uses the strong comparison.
func ParseIfMatch(header string) (IfMatch, error) {
	h := strings.Trim(header, " \t")
	if h == "*" {
		return IfMatch{Any: true}, nil
	}
	m := IfMatch{ETags: []string{}}
	for {
		weak := strings.HasPrefix(h, "W/")
		h = strings.TrimPrefix(h, "W/")
		if !strings.HasPrefix(h, `"`) {
			return IfMatch{}, errBadIfMatch
		}
		end := strings.IndexByte(h[1:], '"')
		if end < 0 {
			return IfMatch{}, errBadIfMatch
		}
		if !weak {
			m.ETags = append(m.ETags, h[:end+2])
		}
		h = strings.TrimLeft(h[end+2:], " \t")
		if h == "" {
			return m, nil
		}
		if h[0] != ',' {
			return IfMatch{}, errBadIfMatch
		}
		h = strings.TrimLeft(h[1:], " \t")
	}
}

// Store keeps documents in the user_documents table.
type Store struct {
	q   *dbgen.Queries
	now func() time.Time
}

// NewStore returns a Store on db that stamps writes with now.
func NewStore(db dbgen.DBTX, now func() time.Time) *Store {
	return &Store{q: dbgen.New(db), now: now}
}

// Get returns userID's document of kind and name, or ErrNotFound.
func (s *Store) Get(ctx context.Context, userID int64, kind domain.DocumentKind, name string) (Document, error) {
	row, err := s.q.GetDocument(ctx, dbgen.GetDocumentParams{UserID: userID, Kind: kind, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("documents: get: %w", err)
	}
	return Document{SchemaVersion: row.SchemaVersion, Body: row.Body, ETag: row.Etag}, nil
}

// List describes userID's documents of kind, ordered by name.
func (s *Store) List(ctx context.Context, userID int64, kind domain.DocumentKind) ([]Info, error) {
	rows, err := s.q.ListDocuments(ctx, dbgen.ListDocumentsParams{UserID: userID, Kind: kind})
	if err != nil {
		return nil, fmt.Errorf("documents: list: %w", err)
	}
	infos := make([]Info, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, Info{Name: r.Name, ETag: r.Etag, UpdatedAt: r.UpdatedAt.UTC()})
	}
	return infos, nil
}

// Create stores a new document for userID, or returns ErrExists if userID
// already has one of that kind and name. The body must be a JSON object of
// at most MaxBodyBytes and schemaVersion must be positive; otherwise the
// error wraps domain.ErrInvalid.
func (s *Store) Create(ctx context.Context, userID int64, kind domain.DocumentKind, name string, schemaVersion int32, body json.RawMessage) (Info, error) {
	compact, etag, err := prepare(kind, name, schemaVersion, body)
	if err != nil {
		return Info{}, err
	}
	at, err := s.q.InsertDocument(ctx, dbgen.InsertDocumentParams{
		UserID: userID, Kind: kind, Name: name,
		Body: compact, SchemaVersion: schemaVersion, Etag: etag,
		CreatedAt: s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Info{}, ErrExists
	}
	if err != nil {
		return Info{}, fmt.Errorf("documents: insert: %w", err)
	}
	return Info{Name: name, ETag: etag, UpdatedAt: at.UTC()}, nil
}

// Update replaces userID's document of kind and name if cond holds, or
// returns ErrPreconditionFailed, also when there is no such document. The
// body and schemaVersion are checked as in Create.
func (s *Store) Update(ctx context.Context, userID int64, kind domain.DocumentKind, name string, cond IfMatch, schemaVersion int32, body json.RawMessage) (Info, error) {
	compact, etag, err := prepare(kind, name, schemaVersion, body)
	if err != nil {
		return Info{}, err
	}
	at, err := s.q.UpdateDocument(ctx, dbgen.UpdateDocumentParams{
		UserID: userID, Kind: kind, Name: name,
		AnyEtag: cond.Any, IfMatch: nonNil(cond.ETags),
		Body: compact, SchemaVersion: schemaVersion, Etag: etag,
		UpdatedAt: s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Info{}, ErrPreconditionFailed
	}
	if err != nil {
		return Info{}, fmt.Errorf("documents: update: %w", err)
	}
	return Info{Name: name, ETag: etag, UpdatedAt: at.UTC()}, nil
}

// Delete removes userID's document of kind and name if cond holds. It
// returns ErrNotFound if there is no such document and ErrPreconditionFailed
// if cond does not hold.
func (s *Store) Delete(ctx context.Context, userID int64, kind domain.DocumentKind, name string, cond IfMatch) error {
	n, err := s.q.DeleteDocument(ctx, dbgen.DeleteDocumentParams{
		UserID: userID, Kind: kind, Name: name,
		AnyEtag: cond.Any, IfMatch: nonNil(cond.ETags),
	})
	if err != nil {
		return fmt.Errorf("documents: delete: %w", err)
	}
	if n > 0 {
		return nil
	}
	exists, err := s.q.DocumentExists(ctx, dbgen.DocumentExistsParams{UserID: userID, Kind: kind, Name: name})
	if err != nil {
		return fmt.Errorf("documents: delete: %w", err)
	}
	if exists {
		return ErrPreconditionFailed
	}
	return ErrNotFound
}

// prepare validates a write and returns the compacted body and its entity
// tag.
func prepare(kind domain.DocumentKind, name string, schemaVersion int32, body json.RawMessage) ([]byte, string, error) {
	if !kind.Valid() {
		return nil, "", fmt.Errorf("%w document kind: unknown value %q", domain.ErrInvalid, kind)
	}
	if _, err := ParseName(name); err != nil {
		return nil, "", err
	}
	if schemaVersion < 1 {
		return nil, "", fmt.Errorf("%w schemaVersion: must be a positive integer, got %d", domain.ErrInvalid, schemaVersion)
	}
	if len(body) > MaxBodyBytes {
		return nil, "", fmt.Errorf("%w body: larger than %d bytes", domain.ErrInvalid, MaxBodyBytes)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil || compact.Len() == 0 || compact.Bytes()[0] != '{' {
		return nil, "", fmt.Errorf("%w body: must be a JSON object", domain.ErrInvalid)
	}
	sum := sha256.Sum256(append([]byte(strconv.Itoa(int(schemaVersion))+"\n"), compact.Bytes()...))
	return compact.Bytes(), `"` + hex.EncodeToString(sum[:16]) + `"`, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
