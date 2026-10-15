// Package api holds the HTTP contract of the service: openapi.yaml, which the
// server in internal/httpapi/gen is generated from and the frontend generates
// its client from.
package api

import _ "embed"

// OpenAPI is the contents of api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
