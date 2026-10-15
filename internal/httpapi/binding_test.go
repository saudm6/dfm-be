package httpapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oapi-codegen/runtime"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi"
	"github.com/saudm6/dfm-be/internal/httpapi/paramtest"
)

// The generated code validates a query parameter only through Bind, so every
// domain enum in api/openapi.yaml must implement runtime.Binder.
var (
	_ runtime.Binder = (*domain.Exchange)(nil)
	_ runtime.Binder = (*domain.Currency)(nil)
	_ runtime.Binder = (*domain.Freshness)(nil)
	_ runtime.Binder = (*domain.Source)(nil)
	_ runtime.Binder = (*domain.Interval)(nil)
	_ runtime.Binder = (*domain.InstrumentClass)(nil)
	_ runtime.Binder = (*domain.InstrumentStatus)(nil)
)

// bindingServer echoes what the generated code bound. Operations it does not
// override panic, which the router answers with 500.
type bindingServer struct {
	paramtest.StrictServerInterface
}

func (bindingServer) ListBars(_ context.Context, req paramtest.ListBarsRequestObject) (paramtest.ListBarsResponseObject, error) {
	out := paramtest.Bound{
		Interval:  req.Interval,
		Source:    req.Params.Source,
		Freshness: req.Params.Freshness,
	}
	if req.Params.MinPrice != nil {
		d, err := httpapi.ParseDecimalParam("minPrice", *req.Params.MinPrice)
		if err != nil {
			return nil, err
		}
		s := d.String()
		out.MinPrice = &s
	}
	return paramtest.ListBars200JSONResponse(out), nil
}

func (bindingServer) PlaceOrder(_ context.Context, req paramtest.PlaceOrderRequestObject) (paramtest.PlaceOrderResponseObject, error) {
	return paramtest.PlaceOrder200JSONResponse(*req.Body), nil
}

func TestBinding(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid path and query values",
			method:     http.MethodGet,
			target:     "/bars/1d?source=dfm-history&freshness=eod&minPrice=11.40",
			wantStatus: http.StatusOK,
			wantBody:   `{"freshness":"eod","interval":"1d","minPrice":"11.4","source":"dfm-history"}`,
		},
		{
			name:       "optional query values absent",
			method:     http.MethodGet,
			target:     "/bars/1m?source=ibkr",
			wantStatus: http.StatusOK,
			wantBody:   `{"interval":"1m","source":"ibkr"}`,
		},
		{
			name:       "unknown path enum",
			method:     http.MethodGet,
			target:     "/bars/5m?source=ibkr",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter interval: invalid interval: unknown value \"5m\""}`,
		},
		{
			name:       "unknown required query enum",
			method:     http.MethodGet,
			target:     "/bars/1d?source=bogus",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter source: invalid source: unknown value \"bogus\""}`,
		},
		{
			name:       "unknown optional query enum",
			method:     http.MethodGet,
			target:     "/bars/1d?source=ibkr&freshness=stale",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter freshness: invalid freshness: unknown value \"stale\""}`,
		},
		{
			name:       "empty query enum",
			method:     http.MethodGet,
			target:     "/bars/1d?source=",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter source: invalid source: unknown value \"\""}`,
		},
		{
			name:       "missing required query enum",
			method:     http.MethodGet,
			target:     "/bars/1d",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter source is required"}`,
		},
		{
			name:       "query enum given twice",
			method:     http.MethodGet,
			target:     "/bars/1d?source=ibkr&source=dfm-mw",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter source has an invalid value"}`,
		},
		{
			name:       "query decimal not a number",
			method:     http.MethodGet,
			target:     "/bars/1d?source=ibkr&minPrice=abc",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter minPrice: \"abc\" is not a decimal number"}`,
		},
		{
			name:       "query integer not a number",
			method:     http.MethodGet,
			target:     "/bars/1d?source=ibkr&limit=ten",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"parameter limit has an invalid value"}`,
		},
		{
			name:       "valid body",
			method:     http.MethodPost,
			target:     "/orders",
			body:       `{"currency":"AED","quantity":100}`,
			wantStatus: http.StatusOK,
			wantBody:   `{"currency":"AED","quantity":100}`,
		},
		{
			name:       "unknown body enum",
			method:     http.MethodPost,
			target:     "/orders",
			body:       `{"currency":"EUR","quantity":100}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"request body: invalid currency: unknown value \"EUR\""}`,
		},
		{
			name:       "body field of the wrong type",
			method:     http.MethodPost,
			target:     "/orders",
			body:       `{"currency":"AED","quantity":"100"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"request body: field quantity has the wrong type"}`,
		},
		{
			name:       "body not JSON",
			method:     http.MethodPost,
			target:     "/orders",
			body:       `{"currency":`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"bad_request","message":"request body is not valid JSON"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := httpapi.NewParamTestRouter(slog.New(slog.DiscardHandler), bindingServer{}, nil)
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want %q", got, "application/json")
			}
			if got := rec.Body.String(); got != tt.wantBody+"\n" {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
		})
	}
}
