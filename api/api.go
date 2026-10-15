// Package api holds the HTTP contract of the service: openapi.yaml, which the
// server in internal/httpapi/gen is generated from and the frontend generates
// its client from.
package api

import _ "embed"

// OpenAPI is the contents of api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte

// PublicOperations holds the IDs of the operations in openapi.yaml that need
// no session. The auth middleware requires a session for every other
// operation, whatever its security requirements say, so an operation is
// public only if it is listed here. Each listed operation declares
// `security: []`; TestSpecPublicOperations keeps the two in step.
var PublicOperations = []string{"getHealth", "getOpenAPISpec", "login"}
