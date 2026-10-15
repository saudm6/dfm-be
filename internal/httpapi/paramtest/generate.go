// Package paramtest is a server generated from a test-only contract,
// paramtest.yaml. Router tests use it to check that path, query, and body
// values bind to the domain types and that bad values get 400 bad_request,
// and that the auth middleware enforces each operation's security.
// Nothing outside tests imports it.
package paramtest

import _ "embed"

//go:generate go tool oapi-codegen -config oapi-codegen.yaml paramtest.yaml

// Spec is the contents of paramtest.yaml.
//
//go:embed paramtest.yaml
var Spec []byte

// PublicOperations holds the IDs of the operations in paramtest.yaml that
// the test router serves without a session.
var PublicOperations = []string{"listBars", "placeOrder"}
