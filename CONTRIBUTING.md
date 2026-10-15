# Contributing

## The API contract

`api/openapi.yaml` is the one HTTP contract. The server types and routing in
`internal/httpapi/gen` are generated from it, and dfm-fe generates its client
from `GET /openapi.yaml`. The WebSocket protocol is in `api/websocket.md`.

### Add or change an endpoint

1. Edit `api/openapi.yaml`. Put paths and schemas only inside your feature's
   section (the `# ==== <tag> (<feature>) ====` banners under `paths` and
   `components.schemas`), and tag each operation with that feature's tag.
   Schemas shared by several features go in the shared section at the top of
   `components.schemas`.
2. Regenerate: `go generate ./...` (or `make generate`). This runs the
   oapi-codegen version pinned by the `tool` directive in `go.mod`.
   Then run `go mod tidy`: generated code for a new kind of operation can
   import oapi-codegen runtime packages that `go.mod` does not list yet (the
   first parameter pulled in `github.com/apapsch/go-jsonmerge/v2`). Commit
   `go.mod` and `go.sum` with the generated code.
3. Implement the new method of `gen.StrictServerInterface` on the `server`
   type in `internal/httpapi`, in a file for your feature (for example
   `internal/httpapi/instruments.go`). The build fails until every operation
   in the spec has a method. A dependency the handler needs (a store, a
   feed) becomes a field of `server` and a parameter of `NewRouter`, set in
   `cmd/dfm/main.go`. Parallel PRs that add dependencies conflict there;
   keep both sides.
4. Commit the spec and the regenerated `internal/httpapi/gen` together. The CI
   `generate` job fails when the committed output differs from what the spec
   produces.

### Types and validation

Enum schemas that mirror `internal/domain` use `x-go-type` so the generated
types are the domain types. Optional enum fields are pointers. The generated
code validates them through the domain type's methods: `UnmarshalText` for
JSON bodies and path parameters, `Bind` for query parameters. So every domain
enum used in the API must have both methods, each calling its `ParseX`
(which wraps `domain.ErrInvalid`). A new enum without `Bind` lets any query
value through to the handler. An unknown value then gets
`400 {"code":"bad_request", ...}` before the handler runs.

Decimals use the `Decimal` schema (a string on the wire) in bodies only. A
decimal query or path parameter is a plain `type: string` schema, because the
generated code cannot bind a parameter to `decimal.Decimal`. Parse it in the
handler with `parseDecimalParam`, which returns an error that the router
answers with 400. Symbols are plain strings in generated code: parse them with
`domain.ParseSymbol` in the handler, and wrap a failure with `invalidParam` so
it also gets 400.

`internal/httpapi/paramtest` is a test-only contract with its own generated
server. `binding_test.go` uses it to check path, query, and body binding
through the real router setup. Extend it when you add a new kind of
parameter.

### Authentication and roles

Every operation needs a signed-in session by default: the spec's top-level
`security` applies the `sessionCookie` scheme. The auth middleware reads each
operation's requirements from the request context, where the generated code
puts them, so the spec decides what is protected:

- `security: []` makes an operation public. `api.TestSpecPublicOperations`
  lists the public operations, so adding one is a deliberate change.
- A POST, PUT, PATCH, or DELETE declares
  `security: [{sessionCookie: [], csrfToken: []}]`. The middleware checks the
  `X-CSRF-Token` header on those methods whatever the spec says;
  `api.TestSpecUnsafeOperationsNeedCSRFToken` keeps the spec in step.
- Market-data and trading operations (quotes, history, orders, positions)
  add the `marketData` scope: `sessionCookie: [marketData]`. Only the
  `owner` role has it; a `member` gets 403. Chat and layouts use no scope.

Handlers read the signed-in user with `auth.SessionFromContext(ctx)`. Code
outside the generated routes, such as the `/ws` upgrade, calls
`(*auth.Service).UserFromRequest(r)` and `auth.RequireMarketData(ctx)`.

### Errors

Every non-2xx response carries the `Error` body: 400 `bad_request` for
values that do not bind or parse, 404 `not_found` and 405
`method_not_allowed` from the router, 500 `internal` for a handler error or
panic (the cause is logged, never sent). A handler that needs another status
declares that response in the spec and returns it.

### Test fakes

A fake server in tests embeds the interface it fakes and overrides only the
methods the test calls:

```go
type failingServer struct {
	gen.StrictServerInterface
}
```

Then adding an operation to the spec does not break existing fakes, and
parallel PRs do not add stubs at the same place. Calling a method the fake
does not override panics, which the router answers with 500.

### Merge conflicts in generated code

Never hand-merge `internal/httpapi/gen`. Resolve the conflict in
`api/openapi.yaml`, run `go generate ./...`, and commit the regenerated
output.
