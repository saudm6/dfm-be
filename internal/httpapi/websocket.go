package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/ws"
)

// serveWebSocket answers GET /ws. The route is outside the generated
// server, so it checks the session itself: it refuses a cross-origin request
// with 403, a request without a live session with 401, and a request that is
// not a WebSocket upgrade with 426, each with the Error body, and hands the
// rest to gateway.
func serveWebSocket(logger *slog.Logger, sessions *auth.Service, gateway *ws.Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "forbidden", "origin "+r.Header.Get("Origin")+" may not open a WebSocket here")
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
		if !hasToken(r.Header, "Connection", "upgrade") || !hasToken(r.Header, "Upgrade", "websocket") {
			w.Header().Set("Upgrade", "websocket")
			writeError(w, http.StatusUpgradeRequired, "upgrade_required", r.URL.Path+" needs a WebSocket upgrade")
			return
		}
		gateway.Serve(w, r, sess)
	}
}

// sameOrigin reports whether r has no Origin header, as from a non-browser
// client, or an Origin whose host is the host r was sent to. Browsers send
// the cookie on cross-site WebSocket upgrades, so this check is what stops
// another site from opening a socket as the user.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

// hasToken reports whether the comma-separated header name contains token,
// ignoring case.
func hasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}
