// Package auth keeps local accounts and their server-side sessions.
//
// A session is a row in the sessions table found through a random token in
// the HttpOnly cookie CookieName. The table stores only the SHA-256 of the
// token. Each session also has a CSRF token that clients send in the
// CSRFHeader header on requests that change state.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/saudm6/dfm-be/internal/store/dbgen"
)

const (
	// CookieName is the name of the session cookie.
	CookieName = "dfm_session"
	// CSRFHeader is the request header that carries the session's CSRF token.
	CSRFHeader = "X-CSRF-Token"
	// MinPasswordLength is the shortest password CreateUser accepts.
	MinPasswordLength = 8

	tokenLen = 32
)

var (
	// ErrInvalidCredentials is returned by Login for an unknown username and
	// for a wrong password alike.
	ErrInvalidCredentials = errors.New("auth: invalid username or password")
	// ErrUnauthenticated means the request has no live session.
	ErrUnauthenticated = errors.New("auth: no valid session")
	// ErrForbidden means the session's role does not allow the request.
	ErrForbidden = errors.New("auth: forbidden")
	// ErrCSRF means a state-changing request lacks the session's CSRF token.
	ErrCSRF = errors.New("auth: missing or invalid CSRF token")
	// ErrUserExists is returned by CreateUser for a taken username.
	ErrUserExists = errors.New("auth: username is taken")
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// User is an account.
type User struct {
	ID       int64
	Username string
	Role     Role
}

// Session is a signed-in user's live session.
type Session struct {
	User      User
	CSRFToken string
	tokenHash []byte
}

// ID identifies the session: two Sessions with the same ID are the same
// login. It is derived from the token's hash and is not a credential.
func (s Session) ID() string { return hex.EncodeToString(s.tokenHash) }

// Options configures a Service.
type Options struct {
	// IdleTimeout ends a session that has seen no request for this long.
	IdleTimeout time.Duration
	// MaxAge ends a session this long after login, however active it is.
	MaxAge time.Duration
	// InsecureCookie leaves the Secure attribute off the cookie, for local
	// development over plain http.
	InsecureCookie bool
	// Now returns the current time. It defaults to time.Now.
	Now func() time.Time
}

// Service creates accounts and signs them in and out.
type Service struct {
	q    *dbgen.Queries
	opts Options
	// dummyHash is verified against when a username is unknown, so an
	// unknown user and a wrong password take the same time. It is made
	// here so that the first failed login is not slower than the rest.
	dummyHash string
}

// NewService returns a Service storing accounts and sessions in db.
func NewService(db dbgen.DBTX, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	dummy, err := HashPassword(rand.Text())
	if err != nil {
		panic(err) // crypto/rand.Read does not return errors.
	}
	return &Service{q: dbgen.New(db), opts: opts, dummyHash: dummy}
}

// NormalizeUsername returns username trimmed and lower-cased, or an error
// unless it is 1 to 64 characters of a-z, 0-9, '.', '_' and '-' starting
// with a letter or digit.
func NormalizeUsername(username string) (string, error) {
	u := strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(u) {
		return "", fmt.Errorf("auth: username %q must be 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit", username)
	}
	return u, nil
}

// CreateUser adds an account with the given role.
func (s *Service) CreateUser(ctx context.Context, username, password string, role Role) (User, error) {
	u, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	if !role.Valid() {
		return User{}, fmt.Errorf("auth: unknown role %q", role)
	}
	if len(password) < MinPasswordLength {
		return User{}, fmt.Errorf("auth: password must be at least %d characters", MinPasswordLength)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	id, err := s.q.InsertUser(ctx, dbgen.InsertUserParams{
		Username:     u,
		PasswordHash: hash,
		Role:         string(role),
		CreatedAt:    s.opts.Now(),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return User{}, fmt.Errorf("%w: %s", ErrUserExists, u)
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: insert user: %w", err)
	}
	return User{ID: id, Username: u, Role: role}, nil
}

// Login checks username and password and starts a session. It returns the
// session and the cookie that carries its token. An unknown username and a
// wrong password both give ErrInvalidCredentials after the same work.
// prior is the value of the session cookie the request already carried, or
// empty; a successful login deletes the session it names.
func (s *Service) Login(ctx context.Context, username, password, prior string) (Session, *http.Cookie, error) {
	row, err := s.q.UserByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, _ = VerifyPassword(password, s.dummyHash)
		return Session{}, nil, ErrInvalidCredentials
	case err != nil:
		return Session{}, nil, fmt.Errorf("auth: find user: %w", err)
	}
	ok, err := VerifyPassword(password, row.PasswordHash)
	if err != nil {
		return Session{}, nil, fmt.Errorf("auth: user %d: %w", row.ID, err)
	}
	if !ok {
		return Session{}, nil, ErrInvalidCredentials
	}
	role, err := ParseRole(row.Role)
	if err != nil {
		return Session{}, nil, fmt.Errorf("auth: user %d: %w", row.ID, err)
	}

	token, err := randomToken()
	if err != nil {
		return Session{}, nil, err
	}
	csrfBytes, err := randomToken()
	if err != nil {
		return Session{}, nil, err
	}
	csrf := base64.RawURLEncoding.EncodeToString(csrfBytes)
	now := s.opts.Now()
	if err := s.q.DeleteExpiredSessions(ctx, now); err != nil {
		return Session{}, nil, fmt.Errorf("auth: delete expired sessions: %w", err)
	}
	if priorToken, err := base64.RawURLEncoding.DecodeString(prior); err == nil && len(priorToken) == tokenLen {
		priorHash := hashToken(priorToken)
		if err := s.q.DeleteSession(ctx, priorHash[:]); err != nil {
			return Session{}, nil, fmt.Errorf("auth: delete prior session: %w", err)
		}
	}
	hash := hashToken(token)
	absolute := now.Add(s.opts.MaxAge)
	if err := s.q.InsertSession(ctx, dbgen.InsertSessionParams{
		TokenHash:         hash[:],
		UserID:            row.ID,
		CsrfToken:         csrf,
		CreatedAt:         now,
		IdleExpiresAt:     minTime(now.Add(s.opts.IdleTimeout), absolute),
		AbsoluteExpiresAt: absolute,
	}); err != nil {
		return Session{}, nil, fmt.Errorf("auth: insert session: %w", err)
	}
	sess := Session{
		User:      User{ID: row.ID, Username: row.Username, Role: role},
		CSRFToken: csrf,
		tokenHash: hash[:],
	}
	return sess, s.cookie(base64.RawURLEncoding.EncodeToString(token), int(s.opts.MaxAge/time.Second)), nil
}

// Logout deletes sess and returns a cookie that expires the browser's copy.
func (s *Service) Logout(ctx context.Context, sess Session) (*http.Cookie, error) {
	if err := s.q.DeleteSession(ctx, sess.tokenHash); err != nil {
		return nil, fmt.Errorf("auth: delete session: %w", err)
	}
	return s.cookie("", -1), nil
}

// UserFromRequest returns the live session named by r's session cookie and
// extends its idle expiry. It returns ErrUnauthenticated when the cookie is
// missing or names no live session. The HTTP middleware and the WebSocket
// upgrade both use it.
func (s *Service) UserFromRequest(r *http.Request) (Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, ErrUnauthenticated
	}
	token, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(token) != tokenLen {
		return Session{}, ErrUnauthenticated
	}
	hash := hashToken(token)
	return s.touch(r.Context(), hash[:])
}

// Revalidate looks sess up again by its token hash, under the same idle and
// absolute expiry rules as UserFromRequest, and extends its idle expiry as a
// request would. It returns the session with its user's current role, or
// ErrUnauthenticated when the session was logged out or has expired. An open
// WebSocket calls it periodically.
func (s *Service) Revalidate(ctx context.Context, sess Session) (Session, error) {
	if len(sess.tokenHash) != sha256.Size {
		return Session{}, ErrUnauthenticated
	}
	return s.touch(ctx, sess.tokenHash)
}

func (s *Service) touch(ctx context.Context, hash []byte) (Session, error) {
	now := s.opts.Now()
	row, err := s.q.TouchSession(ctx, dbgen.TouchSessionParams{
		IdleExpiresAt: now.Add(s.opts.IdleTimeout),
		TokenHash:     hash,
		Now:           now,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Session{}, ErrUnauthenticated
	case err != nil:
		return Session{}, fmt.Errorf("auth: find session: %w", err)
	}
	role, err := ParseRole(row.Role)
	if err != nil {
		return Session{}, fmt.Errorf("auth: user %d: %w", row.ID, err)
	}
	return Session{
		User:      User{ID: row.ID, Username: row.Username, Role: role},
		CSRFToken: row.CsrfToken,
		tokenHash: hash,
	}, nil
}

// CheckCSRF returns ErrCSRF unless r's CSRFHeader holds sess's CSRF token.
func CheckCSRF(r *http.Request, sess Session) error {
	got := r.Header.Get(CSRFHeader)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
		return ErrCSRF
	}
	return nil
}

type sessionKey struct{}

// WithSession returns ctx carrying sess.
func WithSession(ctx context.Context, sess Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, sess)
}

// SessionFromContext returns the session stored by WithSession.
func SessionFromContext(ctx context.Context) (Session, bool) {
	sess, ok := ctx.Value(sessionKey{}).(Session)
	return sess, ok
}

// RequireMarketData returns nil when ctx carries a session whose role may
// use market data and trading (quotes, history, orders, positions). It
// returns ErrUnauthenticated without a session and ErrForbidden for a role
// without that permission.
func RequireMarketData(ctx context.Context) error {
	sess, ok := SessionFromContext(ctx)
	if !ok {
		return ErrUnauthenticated
	}
	if !sess.User.Role.MarketData() {
		return ErrForbidden
	}
	return nil
}

func (s *Service) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !s.opts.InsecureCookie,
		SameSite: http.SameSiteStrictMode,
	}
}

func randomToken() ([]byte, error) {
	b := make([]byte, tokenLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("auth: read random token: %w", err)
	}
	return b, nil
}

func hashToken(token []byte) [sha256.Size]byte { return sha256.Sum256(token) }

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
