package testutil

import (
	"net/http"

	"github.com/go-sphere/httpx"
)

type AbortTracker struct {
	Steps         []string
	AbortedStates []bool
}

func NewAbortTracker() *AbortTracker {
	return &AbortTracker{}
}

func (t *AbortTracker) Reset() {
	t.Steps = t.Steps[:0]
	t.AbortedStates = t.AbortedStates[:0]
}

func (t *AbortTracker) AuthMiddleware(context httpx.Context) {
	t.Steps = append(t.Steps, "before auth")
	if context.Header("token") == "" {
		context.Abort()
		context.Next()
		t.Steps = append(t.Steps, "after abort")
		t.AbortedStates = append(t.AbortedStates, context.IsAborted())
		return
	}
	context.Next()
	t.Steps = append(t.Steps, "after auth")
}

func (t *AbortTracker) SecondMiddleware(context httpx.Context) {
	t.Steps = append(t.Steps, "second middleware")
	context.Next()
}

func (t *AbortTracker) GroupMiddleware(context httpx.Context) {
	t.Steps = append(t.Steps, "group middleware")
	context.Next()
}

func (t *AbortTracker) Handler(context httpx.Context) {
	t.Steps = append(t.Steps, "handler")
	context.Text(http.StatusOK, "success")
}

// SetupAbortEngine wires the shared abort test middlewares/handler on a httpx.Engine.
func SetupAbortEngine(engine httpx.Engine, tracker *AbortTracker) {
	engine.Use(tracker.AuthMiddleware, tracker.SecondMiddleware)
	group := engine.Group("/", tracker.GroupMiddleware)
	group.Handle("GET", "/", tracker.Handler)
}

func EqualSlices[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
