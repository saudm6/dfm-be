package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/saudm6/dfm-be/internal/domain"
)

func TestDocumentKind(t *testing.T) {
	tests := []struct {
		name    string
		decode  func(string) (string, error)
		in      string
		want    string
		wantErr string
	}{
		{name: "parse layout", decode: wrap(domain.ParseDocumentKind), in: "layout", want: "layout"},
		{name: "parse chart template", decode: wrap(domain.ParseDocumentKind), in: "chart_template", want: "chart_template"},
		{name: "parse unknown", decode: wrap(domain.ParseDocumentKind), in: "chart-template", wantErr: `domain: invalid document kind: unknown value "chart-template"`},
		{name: "parse empty", decode: wrap(domain.ParseDocumentKind), in: "", wantErr: `domain: invalid document kind: unknown value ""`},
		{name: "unmarshal", decode: unmarshalText[domain.DocumentKind], in: "layout", want: "layout"},
		{name: "unmarshal unknown", decode: unmarshalText[domain.DocumentKind], in: "Layout", wantErr: `domain: invalid document kind: unknown value "Layout"`},
		{name: "bind", decode: bind[domain.DocumentKind], in: "chart_template", want: "chart_template"},
		{name: "bind unknown", decode: bind[domain.DocumentKind], in: "workspace", wantErr: `domain: invalid document kind: unknown value "workspace"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.decode(tt.in)
			checkErr(t, err, tt.wantErr)
			if got != tt.want {
				t.Errorf("decode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDocumentKindJSON(t *testing.T) {
	out, err := json.Marshal(domain.DocumentKindChartTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"chart_template"` {
		t.Errorf("Marshal = %s, want %q", out, "chart_template")
	}
	var k domain.DocumentKind
	err = json.Unmarshal([]byte(`"notes"`), &k)
	checkErr(t, err, `domain: invalid document kind: unknown value "notes"`)
}
