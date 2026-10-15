package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// marketDataScope is the sessionCookie scope of operations that only roles
// with market data may call.
const marketDataScope = "marketData"

// maxLoginBody caps the login request body, so oversized input is refused
// before it reaches the password hash.
const maxLoginBody = 4 << 10

// noStore keeps caches from storing responses that carry the CSRF token.
const noStore = "no-store"

type priorSessionKey struct{}

// loginGuard checks POST /auth/login before the generated code decodes its
// body. It refuses any body that is not application/json, so a cross-site
// form cannot sign a browser in, and caps the body at maxLoginBody bytes. It
// passes the request's session cookie, if any, on to the handler, which ends
// that session when the login succeeds.
type loginGuard struct{ gen.ServerInterface }

func (g loginGuard) Login(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "send the login request as application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	if c, err := r.Cookie(auth.CookieName); err == nil {
		r = r.WithContext(context.WithValue(r.Context(), priorSessionKey{}, c.Value))
	}
	g.ServerInterface.Login(w, r)
}

func (s server) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	prior, _ := ctx.Value(priorSessionKey{}).(string)
	sess, cookie, err := s.sessions.Login(ctx, req.Body.Username, req.Body.Password, prior)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return gen.Login401JSONResponse{Code: "unauthorized", Message: "invalid username or password"}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.Login200JSONResponse{
		Body:    me(sess),
		Headers: gen.Login200ResponseHeaders{SetCookie: cookie.String(), CacheControl: noStore},
	}, nil
}

func (s server) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	sess, ok := auth.SessionFromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	cookie, err := s.sessions.Logout(ctx, sess)
	if err != nil {
		return nil, err
	}
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: cookie.String()}}, nil
}

func (server) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	sess, ok := auth.SessionFromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	return gen.GetMe200JSONResponse{Body: me(sess), Headers: gen.GetMe200ResponseHeaders{CacheControl: noStore}}, nil
}

func me(sess auth.Session) gen.Me {
	return gen.Me{ID: sess.User.ID, Username: sess.User.Username, Role: sess.User.Role, CSRFToken: sess.CSRFToken}
}

// requireAuth lets requests to the routes in public through and requires a
// live session for every other route, whatever the operation's security
// requirements say. Requests with an unsafe method also need the session's
// CSRF token, and the session's role must grant every sessionCookie scope
// that the generated wrapper put on the request context under sessionKey.
// The session is then put on the request context.
func requireAuth(logger *slog.Logger, sessions *auth.Service, sessionKey any, public map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// RoutePattern() trims a trailing "/", which would make /x/ as
			// public as /x; the joined patterns keep it.
			if public[r.Method+" "+strings.Join(chi.RouteContext(r.Context()).RoutePatterns, "")] {
				next.ServeHTTP(w, r)
				return
			}
			scopes, _ := r.Context().Value(sessionKey).([]string)
			sess, err := sessions.UserFromRequest(r)
			if errors.Is(err, auth.ErrUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to use "+r.URL.Path)
				return
			}
			if err != nil {
				logger.LogAttrs(r.Context(), slog.LevelError, "authenticate request",
					slog.String("path", r.URL.Path),
					slog.String("error", err.Error()),
					slog.String("request_id", middleware.GetReqID(r.Context())),
				)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			if !safeMethod(r.Method) {
				if err := auth.CheckCSRF(r, sess); err != nil {
					writeError(w, http.StatusForbidden, "forbidden", "missing or invalid "+auth.CSRFHeader+" header")
					return
				}
			}
			ctx := auth.WithSession(r.Context(), sess)
			for _, scope := range scopes {
				switch scope {
				case marketDataScope:
					if err := auth.RequireMarketData(ctx); err != nil {
						writeError(w, http.StatusForbidden, "forbidden", "account "+sess.User.Username+" has no access to market data")
						return
					}
				default:
					logger.LogAttrs(r.Context(), slog.LevelError, "unknown security scope",
						slog.String("path", r.URL.Path),
						slog.String("scope", scope),
						slog.String("request_id", middleware.GetReqID(r.Context())),
					)
					writeError(w, http.StatusInternalServerError, "internal", "internal server error")
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// publicRoutes returns "METHOD /path" for each operation of spec whose ID is
// in ids. It fails when spec does not parse or lacks one of the IDs.
func publicRoutes(spec []byte, ids []string) (map[string]bool, error) {
	doc, err := openapi3.NewLoader().LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	routes := map[string]bool{}
	found := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if slices.Contains(ids, op.OperationID) {
				routes[method+" "+path] = true
				found[op.OperationID] = true
			}
		}
	}
	for _, id := range ids {
		if !found[id] {
			return nil, fmt.Errorf("public operation %q is not in the spec", id)
		}
	}
	return routes, nil
}

func mustPublicRoutes(spec []byte, ids []string) map[string]bool {
	routes, err := publicRoutes(spec, ids)
	if err != nil {
		panic("httpapi: " + err.Error())
	}
	return routes
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
