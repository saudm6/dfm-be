package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// marketDataScope is the sessionCookie scope of operations that only roles
// with market data may call.
const marketDataScope = "marketData"

func (s server) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	sess, cookie, err := s.sessions.Login(ctx, req.Body.Username, req.Body.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return gen.Login401JSONResponse{Code: "unauthorized", Message: "invalid username or password"}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.Login200JSONResponse{Body: me(sess), Headers: gen.Login200ResponseHeaders{SetCookie: cookie.String()}}, nil
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
	return gen.GetMe200JSONResponse(me(sess)), nil
}

func me(sess auth.Session) gen.Me {
	return gen.Me{ID: sess.User.ID, Username: sess.User.Username, Role: sess.User.Role, CSRFToken: sess.CSRFToken}
}

// requireAuth enforces the security requirements that the generated
// wrapper put on the request context under sessionKey: the context value
// holds the operation's sessionCookie scopes, and is absent for operations
// declared with `security: []`. Requests to other operations need a live
// session, a matching CSRF token unless the method is safe, and a role that
// grants every scope. The session is then put on the request context.
func requireAuth(logger *slog.Logger, sessions *auth.Service, sessionKey any) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scopes, protected := r.Context().Value(sessionKey).([]string)
			if !protected {
				next.ServeHTTP(w, r)
				return
			}
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

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
