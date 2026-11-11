package httpxtest

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestGoldenLargeIntegers(t *testing.T) {
	a := canonicalJSON(t, `{"id":9007199254740992}`)
	b := canonicalJSON(t, `{"id":9007199254740993}`)
	if a == b {
		t.Errorf("distinct IDs collapse into the same contract: %s", a)
	}
}

func TestGoldenTrailingNewline(t *testing.T) {
	a := contractOf(t, response{Status: 200, Headers: http.Header{"Content-Type": []string{"text/plain"}}, Body: "x"}).render()
	b := contractOf(t, response{Status: 200, Headers: http.Header{"Content-Type": []string{"text/plain"}}, Body: "x\n"}).render()
	if a == b {
		t.Errorf("different text bodies collapse into the same contract: %q", a)
	}
}

func TestGoldenLedgerOrphans(t *testing.T) {
	l := &goldenLedger{used: map[string]bool{"Group__Used.txt": true}}
	l.skip("Group__Skipped")
	got := l.orphans([]string{
		"Group__Used.txt",
		"Group__Skipped.txt",
		"Group__Skipped__Nested.txt",
		// Shares the skipped stem as a string prefix but is a sibling case.
		"Group__SkippedSibling.txt",
		"Group__Gone.txt",
	})
	want := []string{"Group__Gone.txt", "Group__SkippedSibling.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("orphans = %q, want %q", got, want)
	}
}

// The orphan check counts a golden file below a skipped case as accounted for
// only if the skip went through runner.skipf, so a case calling t.Skip directly
// would make its golden files look orphaned on the adapters that skip it.
func TestCasesSkipThroughRunner(t *testing.T) {
	direct := regexp.MustCompile(`\bt\.Skip(f|Now)?\(`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		// suite.go defines skipf and the orphan check's own skips.
		if strings.HasSuffix(file, "_test.go") || file == "suite.go" {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if direct.MatchString(line) {
				t.Errorf("%s:%d: skip a case with r.skipf, not t.Skip: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}
