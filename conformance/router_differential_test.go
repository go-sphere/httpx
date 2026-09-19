package conformance

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/stdx"
)

// stdx is the only router written in this repository, so its matching is
// checked against the frameworks'. The bar is "never contradict a unanimous
// answer": the four disagree on overlapping tables, and a case without a
// consensus has no contract to conform to. The seed is fixed for reproducibility.
func TestRouterMatchesFrameworkConsensus(t *testing.T) {
	const (
		tables       = 60
		routesPerSet = 10
		probesPerSet = 40
	)
	rng := rand.New(rand.NewSource(20260918))

	var compared, skipped, disputed int
	for table := range tables {
		patterns := randomRouteTable(rng, routesPerSet)

		reference := make(map[string]httpx.Engine, len(conformanceFrameworks))
		usable := true
		for _, framework := range conformanceFrameworks {
			engine, ok := buildProbeEngine(t, framework, patterns)
			if !ok {
				// A framework that refuses the table cannot vote on it.
				usable = false
				break
			}
			reference[framework] = engine
		}
		if !usable {
			skipped++
			continue
		}
		subject, ok := buildProbeEngine(t, "stdx", patterns)
		if !ok {
			t.Fatalf("table %d: stdx refused a table every framework accepted: %v", table, patterns)
		}

		for range probesPerSet {
			path := randomPath(rng, patterns)
			consensus, agreed := "", true
			for i, framework := range conformanceFrameworks {
				answer := probeRoute(t, reference[framework], path)
				if i == 0 {
					consensus = answer
					continue
				}
				if answer != consensus {
					agreed = false
					break
				}
			}
			if !agreed {
				disputed++
				continue
			}
			compared++
			if got := probeRoute(t, subject, path); got != consensus {
				t.Fatalf("table %d: GET %s\n  frameworks (unanimous): %s\n  stdx: %s\n  routes: %v",
					table, path, consensus, got, patterns)
			}
		}
	}
	t.Logf("%d probes compared against a unanimous answer, %d disputed between frameworks, %d tables skipped",
		compared, disputed, skipped)
	if compared == 0 {
		t.Fatal("no probe reached a unanimous answer: the test is not testing anything")
	}
}

// Names are fixed per depth: every router requires one name per parameter position.
func randomRouteTable(rng *rand.Rand, n int) []string {
	staticSegments := []string{"users", "posts", "v1", "files", "a", "b"}
	seen := make(map[string]bool, n)

	patterns := make([]string, 0, n)
	for attempts := 0; len(patterns) < n && attempts < n*40; attempts++ {
		depth := 1 + rng.Intn(3)
		var b strings.Builder
		for d := range depth {
			b.WriteByte('/')
			switch rng.Intn(4) {
			case 0:
				b.WriteString(":p" + strconv.Itoa(d))
			case 1:
				if d == depth-1 && d > 0 {
					b.WriteString("*rest")
				} else {
					b.WriteString(staticSegments[rng.Intn(len(staticSegments))])
				}
			default:
				b.WriteString(staticSegments[rng.Intn(len(staticSegments))])
			}
		}
		pattern := b.String()
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}
	return patterns
}

func randomPath(rng *rand.Rand, patterns []string) string {
	// Half the probes follow a registered shape with random values (matching),
	// half are free-form (misses and backtracking).
	if rng.Intn(2) == 0 {
		pattern := patterns[rng.Intn(len(patterns))]
		var b strings.Builder
		for _, seg := range strings.Split(strings.TrimPrefix(pattern, "/"), "/") {
			b.WriteByte('/')
			switch {
			case strings.HasPrefix(seg, ":"):
				b.WriteString("v" + strconv.Itoa(rng.Intn(3)))
			case strings.HasPrefix(seg, "*"):
				b.WriteString("x/y")
			default:
				b.WriteString(seg)
			}
		}
		return b.String()
	}
	segments := []string{"users", "posts", "v1", "files", "a", "b", "v0", "v1", "zzz"}
	depth := 1 + rng.Intn(4)
	var b strings.Builder
	for range depth {
		b.WriteByte('/')
		b.WriteString(segments[rng.Intn(len(segments))])
	}
	return b.String()
}

// Registration panics are a legitimate answer ("this router refuses the table"), not a failure.
func buildProbeEngine(t *testing.T, framework string, patterns []string) (engine httpx.Engine, ok bool) {
	t.Helper()
	defer func() {
		if recover() != nil {
			engine, ok = nil, false
		}
	}()
	if framework == "stdx" {
		engine = stdx.New()
	} else {
		engine = newHarnessTB(t, framework).Engine
	}
	router := engine.Group("")
	for i, pattern := range patterns {
		router.GET(pattern, routeProbe(i))
	}
	return engine, true
}

// The index identifies the route portably: echox and fiberx rewrite a named
// wildcard at registration, so their FullPath is not comparable.
func routeProbe(index int) httpx.Handler {
	return func(ctx httpx.Context) error {
		parts := []string{"route=" + strconv.Itoa(index)}
		params := ctx.Params()
		for _, key := range sortedKeys(params) {
			parts = append(parts, key+"="+params[key])
		}
		return ctx.Text(http.StatusOK, strings.Join(parts, " "))
	}
}

func probeRoute(t *testing.T, engine httpx.Engine, path string) string {
	t.Helper()
	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatal("engine does not support httpx.TestRequester")
	}
	resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "http://example.com"+path, nil))
	if err != nil {
		t.Fatalf("serve %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// A miss must be a miss everywhere, but its rendering is each adapter's
		// business: only the status is comparable.
		return "status=" + strconv.Itoa(resp.StatusCode)
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
