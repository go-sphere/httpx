package httpx

import (
	"strings"
	"testing"
	"time"
)

func TestSSEWriterSend(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		event SSEEvent
		want  string
	}{
		{
			name:  "data only",
			event: SSEEvent{Data: "hello"},
			want:  "data: hello\n\n",
		},
		{
			name:  "all fields",
			event: SSEEvent{ID: "42", Event: "update", Data: "hello", Retry: 3 * time.Second},
			want:  "event: update\nid: 42\nretry: 3000\ndata: hello\n\n",
		},
		{
			name:  "multiline data LF",
			event: SSEEvent{Data: "one\ntwo"},
			want:  "data: one\ndata: two\n\n",
		},
		{
			name:  "multiline data CRLF and CR",
			event: SSEEvent{Data: "one\r\ntwo\rthree"},
			want:  "data: one\ndata: two\ndata: three\n\n",
		},
		{
			name:  "trailing newline yields empty data line",
			event: SSEEvent{Data: "one\n"},
			want:  "data: one\ndata: \n\n",
		},
		{
			name:  "id and retry without data",
			event: SSEEvent{ID: "7", Retry: 1500 * time.Millisecond},
			want:  "id: 7\nretry: 1500\n\n",
		},
		{
			name:  "sub-millisecond retry omitted",
			event: SSEEvent{Data: "x", Retry: 500 * time.Microsecond},
			want:  "data: x\n\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			if err := NewSSEWriter(&sb).Send(&tc.event); err != nil {
				t.Fatalf("Send() = %v, want nil", err)
			}
			if got := sb.String(); got != tc.want {
				t.Fatalf("Send() wrote %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSSEWriterSendInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		event SSEEvent
	}{
		{name: "empty event", event: SSEEvent{}},
		{name: "negative retry only", event: SSEEvent{Retry: -time.Second}},
		{name: "newline in id", event: SSEEvent{ID: "a\nb", Data: "x"}},
		{name: "carriage return in id", event: SSEEvent{ID: "a\rb", Data: "x"}},
		{name: "NUL in id", event: SSEEvent{ID: "a\x00b", Data: "x"}},
		{name: "newline in event type", event: SSEEvent{Event: "a\nb", Data: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			if err := NewSSEWriter(&sb).Send(&tc.event); err == nil {
				t.Fatal("Send() = nil, want error")
			}
			if sb.Len() != 0 {
				t.Fatalf("invalid event wrote %q, want nothing", sb.String())
			}
		})
	}
}

func TestSSEWriterSendData(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	if err := NewSSEWriter(&sb).SendData("ping"); err != nil {
		t.Fatalf("SendData() = %v, want nil", err)
	}
	if got := sb.String(); got != "data: ping\n\n" {
		t.Fatalf("SendData() wrote %q", got)
	}
}

func TestSSEWriterSendJSON(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	w := NewSSEWriter(&sb)
	if err := w.SendJSON("delta", map[string]string{"text": "line one\nline two"}); err != nil {
		t.Fatalf("SendJSON() = %v, want nil", err)
	}
	want := "event: delta\ndata: {\"text\":\"line one\\nline two\"}\n\n"
	if got := sb.String(); got != want {
		t.Fatalf("SendJSON() wrote %q, want %q", got, want)
	}

	if err := w.SendJSON("", func() {}); err == nil {
		t.Fatal("SendJSON() with unmarshalable value = nil, want error")
	}
}

func TestSSEWriterComment(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	w := NewSSEWriter(&sb)
	if err := w.Comment(""); err != nil {
		t.Fatalf("Comment() = %v, want nil", err)
	}
	if got := sb.String(); got != ": \n\n" {
		t.Fatalf("Comment(\"\") wrote %q", got)
	}

	sb.Reset()
	if err := w.Comment("keep\nalive"); err != nil {
		t.Fatalf("Comment() = %v, want nil", err)
	}
	if got := sb.String(); got != ": keep\n: alive\n\n" {
		t.Fatalf("Comment() wrote %q", got)
	}
}

func TestSSEWriterSequentialEvents(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	w := NewSSEWriter(&sb)
	if err := w.SendData("one"); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	if err := w.Send(&SSEEvent{ID: "2", Data: "two"}); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	want := "data: one\n\nid: 2\ndata: two\n\n"
	if got := sb.String(); got != want {
		t.Fatalf("stream = %q, want %q", got, want)
	}
}
