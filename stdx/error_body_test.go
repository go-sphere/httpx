package stdx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// appendErrorBody must produce exactly what encoding/json does, so switching
// the default error handler to it changes no response byte.
func TestAppendErrorBodyMatchesEncodingJSON(t *testing.T) {
	messages := []string{
		"", "Not Found", `quote " and backslash \`, "<script>&</script>",
		"tab\tnewline\ncr\rbs\bff\f", "\x00\x01\x1f\x7f", "unicode é 中文 🚀",
		"line\u2028para\u2029", "bad \xff utf8 \xc3", "\xed\xa0\x80 surrogate",
	}
	var all strings.Builder
	for b := 0; b < 256; b++ {
		all.WriteByte(byte(b))
		messages = append(messages, string([]byte{byte(b)}))
	}
	messages = append(messages, all.String())
	for _, code := range []int{0, 7, -1, 1 << 30} {
		for _, msg := range messages {
			body := httpx.ErrorBody{Code: code, Message: msg}
			want, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if got := appendErrorBody(nil, body); string(got) != string(want) {
				t.Fatalf("message %q code %d:\n got %s\nwant %s", msg, code, got, want)
			}
		}
	}
}

func FuzzAppendErrorBody(f *testing.F) {
	f.Add("Not Found", 0)
	f.Add("\xff<&>\u2028", -3)
	f.Fuzz(func(t *testing.T, msg string, code int) {
		body := httpx.ErrorBody{Code: code, Message: msg}
		want, err := json.Marshal(body)
		if err != nil {
			t.Skip()
		}
		if got := appendErrorBody(nil, body); string(got) != string(want) {
			t.Fatalf("message %q: got %s, want %s", msg, got, want)
		}
	})
}
