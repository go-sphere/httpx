package httpxtest

import (
	"net/http"
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
