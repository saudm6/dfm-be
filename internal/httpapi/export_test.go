package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/httpapi/paramtest"
)

// NewRouterWithServer lets tests serve the contract with a fake implementation.
var NewRouterWithServer = newRouter

// ParseDecimalParam lets fake servers parse decimal parameters as handlers do.
var ParseDecimalParam = parseDecimalParam

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
		Middlewares:      []paramtest.MiddlewareFunc{requireAuth(logger, sessions, paramtest.SessionCookieScopes)},
		ErrorHandlerFunc: paramBindError,
	})
	return r
}
