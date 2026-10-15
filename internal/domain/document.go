package domain

// DocumentKind is the kind of a document a user keeps on the server, such as
// a workspace layout.
type DocumentKind string

// The document kinds.
const (
	DocumentKindLayout        DocumentKind = "layout"
	DocumentKindChartTemplate DocumentKind = "chart_template"
)

// ParseDocumentKind returns the DocumentKind named s.
func ParseDocumentKind(s string) (DocumentKind, error) {
	return parseEnum[DocumentKind]("document kind", s)
}

// Valid reports whether k is a known document kind.
func (k DocumentKind) Valid() bool {
	switch k {
	case DocumentKindLayout, DocumentKindChartTemplate:
		return true
	}
	return false
}

// MarshalText returns k as text.
func (k DocumentKind) MarshalText() ([]byte, error) { return []byte(k), nil }

// UnmarshalText sets k to ParseDocumentKind(text), so decoding rejects
// unknown values.
func (k *DocumentKind) UnmarshalText(text []byte) error {
	return unmarshalText(k, ParseDocumentKind, text)
}

// Bind sets k to ParseDocumentKind(src). The generated HTTP server calls it
// for parameters, so unknown values are rejected there too.
func (k *DocumentKind) Bind(src string) error {
	return unmarshalText(k, ParseDocumentKind, []byte(src))
}
