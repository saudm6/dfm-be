package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi/paramtest"
)

// NewBaseRouter lets tests mount their own handlers behind the router's
// middleware.
func NewBaseRouter(logger *slog.Logger) *chi.Mux { return newBaseRouter(logger) }

// NewRouterWithServer lets tests serve the contract with a fake implementation.
var NewRouterWithServer = newRouter

// ParseDecimalParam lets fake servers parse decimal parameters as handlers do.
var ParseDecimalParam = parseDecimalParam

var paramTestPublicRoutes = mustPublicRoutes(paramtest.Spec, paramtest.PublicOperations)

// PublicRoutes lets tests check how public operation IDs resolve to routes.
var PublicRoutes = publicRoutes

// NewParamTestRouter serves the test-only contract in paramtest with ssi,
// set up exactly as newRouter sets up api/openapi.yaml.
func NewParamTestRouter(logger *slog.Logger, ssi paramtest.StrictServerInterface, sessions *auth.Service) http.Handler {
	r := newBaseRouter(logger)
	h := paramtest.NewStrictHandlerWithOptions(ssi, nil, paramtest.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  bodyDecodeError,
		ResponseErrorHandlerFunc: handlerError(logger),
	})
	paramtest.HandlerWithOptions(h, paramtest.ChiServerOptions{
		BaseRouter:       r,
		Middlewares:      []paramtest.MiddlewareFunc{requireAuth(logger, sessions, paramtest.SessionCookieScopes, paramTestPublicRoutes)},
		ErrorHandlerFunc: paramBindError,
	})
	return r
}
