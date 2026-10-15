// Package paramtest is a server generated from a test-only contract,
// paramtest.yaml. Router tests use it to check that path, query, and body
// values bind to the domain types and that bad values get 400 bad_request,
// and that the auth middleware enforces each operation's security.
// Nothing outside tests imports it.
package paramtest

//go:generate go tool oapi-codegen -config oapi-codegen.yaml paramtest.yaml
