package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/shopspring/decimal"

	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// server implements every operation in api/openapi.yaml. Each feature puts
// its handler methods in its own file in this package.
type server struct{}

var _ gen.StrictServerInterface = server{}

// writeError sends the contract's Error body.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}

// paramError is a parameter value a handler could not parse. handlerError
// answers it with 400 bad_request instead of 500 internal.
type paramError struct {
	name string
	err  error
}

func (e *paramError) Error() string { return "parameter " + e.name + ": " + detail(e.err) }

func (e *paramError) Unwrap() error { return e.err }

// invalidParam marks err as a bad value for the parameter called name.
func invalidParam(name string, err error) error { return &paramError{name: name, err: err} }

// parseDecimalParam parses the decimal parameter called name. Decimal
// parameters are plain strings in the spec, because the generated code cannot
// bind a query string to decimal.Decimal.
func parseDecimalParam(name, s string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, invalidParam(name, fmt.Errorf("%q is not a decimal number", s))
	}
	return d, nil
}

// paramBindError answers requests whose path, query, or header parameters do
// not bind. It is the generated router's ErrorHandlerFunc.
func paramBindError(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusBadRequest, "bad_request", paramBindMessage(err))
}

// paramBindMessage describes err without the Go type names that the generated
// and runtime errors carry. Every generated package declares its own copies of
// the parameter error types, all with a ParamName field, so they are matched
// by type name rather than by type.
func paramBindMessage(err error) string {
	v := reflect.ValueOf(err)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return "invalid parameters"
	}
	field := v.Elem().FieldByName("ParamName")
	if !field.IsValid() || field.Kind() != reflect.String {
		return "invalid parameters"
	}
	name := field.String()
	switch v.Elem().Type().Name() {
	case "RequiredParamError":
		return "parameter " + name + " is required"
	case "RequiredHeaderError":
		return "header " + name + " is required"
	case "TooManyValuesForParamError":
		return "parameter " + name + " must be given once"
	}
	if errors.Is(err, domain.ErrInvalid) {
		return "parameter " + name + ": " + detail(err)
	}
	return "parameter " + name + " has an invalid value"
}

// bodyDecodeError answers requests whose body does not decode. It is the
// strict server's RequestErrorHandlerFunc.
func bodyDecodeError(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusBadRequest, "bad_request", bodyDecodeMessage(err))
}

func bodyDecodeMessage(err error) string {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.Is(err, domain.ErrInvalid):
		return "request body: " + detail(err)
	case errors.As(err, &typeErr) && typeErr.Field != "":
		return "request body: field " + typeErr.Field + " has the wrong type"
	case errors.As(err, &syntaxErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "request body is not valid JSON"
	}
	return "request body is invalid"
}

// detail returns the message of the domain validation error in err's chain,
// without the "domain: " prefix, or err's own message when there is none.
func detail(err error) string {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if errors.Unwrap(e) == domain.ErrInvalid { //nolint:errorlint // looks for the error that wraps ErrInvalid itself
			return strings.TrimPrefix(e.Error(), "domain: ")
		}
	}
	return err.Error()
}

// handlerError answers when a handler returns an error. A paramError is the
// client's fault and gets 400; anything else gets 500 and its cause is
// logged, not sent.
func handlerError(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if pe, ok := errors.AsType[*paramError](err); ok {
			writeError(w, http.StatusBadRequest, "bad_request", pe.Error())
			return
		}
		logger.LogAttrs(r.Context(), slog.LevelError, "http handler failed",
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}
