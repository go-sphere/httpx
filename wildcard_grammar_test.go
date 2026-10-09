package httpx

import (
	"os"
	"strings"
	"testing"
)

// grammarCategoryMessage maps a fixture violation category to the fragment
// ValidateWildcardPath puts in its error.
var grammarCategoryMessage = map[string]string{
	"not-segment-start": "must start its own path segment",
	"not-final":         "must be the final path segment",
	"multiple":          "only one wildcard",
	"anonymous":         "must be named",
}

type grammarCase struct {
	valid    bool
	category string
	path     string
}

// loadGrammarCases parses testdata/route_grammar.golden.
func loadGrammarCases(t *testing.T) []grammarCase {
	t.Helper()
	data, err := os.ReadFile("testdata/route_grammar.golden")
	if err != nil {
		t.Fatal(err)
	}
	var cases []grammarCase
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 || (fields[0] != "valid" && fields[0] != "invalid") {
			t.Fatalf("malformed fixture line %q", line)
		}
		cases = append(cases, grammarCase{valid: fields[0] == "valid", category: fields[1], path: fields[2]})
	}
	if len(cases) == 0 {
		t.Fatal("route grammar fixture is empty")
	}
	return cases
}

func TestValidateWildcardPathGrammarFixture(t *testing.T) {
	for _, c := range loadGrammarCases(t) {
		t.Run(c.path, func(t *testing.T) {
			err := ValidateWildcardPath(c.path)
			if c.valid {
				if c.category != "ok" {
					t.Fatalf("valid case must use category ok, got %q", c.category)
				}
				if err != nil {
					t.Fatalf("ValidateWildcardPath(%q) = %v, want nil", c.path, err)
				}
				return
			}
			want, known := grammarCategoryMessage[c.category]
			if !known {
				t.Fatalf("unknown violation category %q", c.category)
			}
			if err == nil {
				t.Fatalf("ValidateWildcardPath(%q) = nil, want %s violation", c.path, c.category)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("ValidateWildcardPath(%q) = %q, want %s violation (%q)", c.path, err, c.category, want)
			}
		})
	}
}
