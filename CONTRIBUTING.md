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
3. Implement the new method of `gen.StrictServerInterface` on the `server`
   type in `internal/httpapi`, in a file for your feature (for example
   `internal/httpapi/instruments.go`). The build fails until every operation
   in the spec has a method.
4. Commit the spec and the regenerated `internal/httpapi/gen` together. The CI
   `generate` job fails when the committed output differs from what the spec
   produces.

Enum schemas that mirror `internal/domain` use `x-go-type` so the generated
types are the domain types; decoding them validates the value. Optional enum
fields are pointers. Symbols are plain strings in generated code: parse them
with `domain.ParseSymbol` in the handler.

### Merge conflicts in generated code

Never hand-merge `internal/httpapi/gen`. Resolve the conflict in
`api/openapi.yaml`, run `go generate ./...`, and commit the regenerated
output.
