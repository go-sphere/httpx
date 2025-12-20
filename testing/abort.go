package testing

import (
	"sync"

	"github.com/go-sphere/httpx"
)

// AbortTracker tracks middleware execution flow and abort states for testing purposes.
// It provides thread-safe tracking of middleware execution steps and abort states.
type AbortTracker struct {
	Steps         []string
	AbortedStates []bool
	mu            sync.Mutex
}

// NewAbortTracker creates a new AbortTracker with empty tracking lists.
func NewAbortTracker() *AbortTracker {
	return &AbortTracker{
		Steps:         make([]string, 0),
		AbortedStates: make([]bool, 0),
	}
}

// Reset clears all tracking data, allowing the tracker to be reused.
func (t *AbortTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.Steps = make([]string, 0)
	t.AbortedStates = make([]bool, 0)
}

// recordStep safely records a middleware execution step and the current abort state.
func (t *AbortTracker) recordStep(step string, ctx httpx.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.Steps = append(t.Steps, step)
	t.AbortedStates = append(t.AbortedStates, ctx.IsAborted())
}

// AuthMiddleware is a test middleware that checks for authentication token.
// If no "Authorization" header is present, it aborts the request.
// Otherwise, it continues to the next middleware.
func (t *AbortTracker) AuthMiddleware(ctx httpx.Context) {
	t.recordStep("AuthMiddleware", ctx)
	
	token := ctx.Header("Authorization")
	if token == "" {
		ctx.Abort()
		t.recordStep("AuthMiddleware-Aborted", ctx)
		return
	}
	
	ctx.Next()
}

// SecondMiddleware is a test middleware that always continues execution.
// It records its execution step and calls the next middleware.
func (t *AbortTracker) SecondMiddleware(ctx httpx.Context) {
	t.recordStep("SecondMiddleware", ctx)
	ctx.Next()
}

// GroupMiddleware is a test middleware for route groups.
// It records its execution step and continues to the next middleware.
func (t *AbortTracker) GroupMiddleware(ctx httpx.Context) {
	t.recordStep("GroupMiddleware", ctx)
	ctx.Next()
}

// Handler is a test handler that records its execution.
// It responds with a simple JSON message.
func (t *AbortTracker) Handler(ctx httpx.Context) {
	t.recordStep("Handler", ctx)
	ctx.JSON(200, map[string]string{"message": "success"})
}

// SetupAbortEngine configures an engine with test middleware and routes for abort testing.
// It sets up global middleware, creates a route group with group middleware,
// and registers a test handler.
func SetupAbortEngine(engine httpx.Engine, tracker *AbortTracker) {
	// Set up global middleware
	engine.Use(tracker.AuthMiddleware)
	engine.Use(tracker.SecondMiddleware)
	
	// Create route group with group middleware
	group := engine.Group("/test", tracker.GroupMiddleware)
	
	// Register test handler to root path within the group
	group.GET("/", tracker.Handler)
}