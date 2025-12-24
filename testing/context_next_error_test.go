package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// TestContextNextErrorHandling tests various Context.Next() error handling scenarios
func TestContextNextErrorHandling(t *testing.T) {
	engine := &MockEngine{}
	utils := NewErrorTestingUtilities(engine)

	t.Run("Error propagation scenarios", func(t *testing.T) {
		testCases := []struct {
			name        string
			middleware  httpx.Middleware
			handler     httpx.Handler
			description string
		}{
			{
				name: "Middleware propagates handler error unchanged",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					// Log the error but propagate it unchanged
					if err != nil {
						t.Logf("Middleware caught error: %v", err)
					}
					return err
				},
				handler:     utils.CreateErrorHandler(NewTestError("handler error")),
				description: "Middleware should catch and propagate handler errors",
			},
			{
				name: "Middleware handles successful Next() call",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					if err != nil {
						t.Errorf("Expected no error from Next(), got: %v", err)
						return err
					}
					t.Logf("Next() completed successfully")
					return nil
				},
				handler:     utils.CreateSuccessHandler("success"),
				description: "Middleware should handle successful Next() calls",
			},
			{
				name: "Middleware transforms error from Next()",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					if err != nil {
						// Transform the error
						return NewTestErrorWithStatus("transformed: "+err.Error(), 400)
					}
					return nil
				},
				handler:     utils.CreateErrorHandler(NewTestError("original error")),
				description: "Middleware should be able to transform errors from Next()",
			},
			{
				name: "Middleware recovers from error",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					if err != nil {
						t.Logf("Recovering from error: %v", err)
						// Convert error to success by returning nil
						return nil
					}
					return nil
				},
				handler:     utils.CreateErrorHandler(NewTestError("recoverable error")),
				description: "Middleware should be able to recover from errors by returning nil",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := engine.Group("")
				router.Use(tc.middleware)
				router.GET(GenerateUniqueTestPath(), tc.handler)
				t.Logf("Error propagation test registered: %s", tc.description)
			})
		}
	})

	t.Run("Multiple middleware error handling", func(t *testing.T) {
		testCases := []struct {
			name        string
			middlewares []httpx.Middleware
			handler     httpx.Handler
			description string
		}{
			{
				name: "First middleware returns error, second not executed",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						return NewTestError("first middleware error")
					},
					func(ctx httpx.Context) error {
						t.Error("Second middleware should not be executed")
						return ctx.Next()
					},
				},
				handler:     utils.CreateSuccessHandler("success"),
				description: "First middleware error should prevent subsequent middleware execution",
			},
			{
				name: "First middleware passes, second returns error",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("First middleware executing")
						return ctx.Next()
					},
					func(ctx httpx.Context) error {
						return NewTestError("second middleware error")
					},
				},
				handler:     utils.CreateSuccessHandler("success"),
				description: "Second middleware error should be propagated back through first middleware",
			},
			{
				name: "All middleware pass, handler returns error",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						err := ctx.Next()
						if err != nil {
							t.Logf("First middleware caught handler error: %v", err)
						}
						return err
					},
					func(ctx httpx.Context) error {
						err := ctx.Next()
						if err != nil {
							t.Logf("Second middleware caught handler error: %v", err)
						}
						return err
					},
				},
				handler:     utils.CreateErrorHandler(NewTestError("handler error")),
				description: "Handler error should propagate back through all middleware",
			},
			{
				name: "Middleware chain with error recovery",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						err := ctx.Next()
						if err != nil {
							t.Logf("Outer middleware recovering from error: %v", err)
							// Recover from any downstream errors
							return nil
						}
						return nil
					},
					func(ctx httpx.Context) error {
						err := ctx.Next()
						if err != nil {
							t.Logf("Inner middleware propagating error: %v", err)
						}
						return err
					},
				},
				handler:     utils.CreateErrorHandler(NewTestError("handler error")),
				description: "Outer middleware should recover from errors caught by inner middleware",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := engine.Group("")
				for _, middleware := range tc.middlewares {
					router.Use(middleware)
				}
				router.GET(GenerateUniqueTestPath(), tc.handler)
				t.Logf("Multiple middleware test registered: %s", tc.description)
			})
		}
	})

	t.Run("Error recovery patterns", func(t *testing.T) {
		testCases := []struct {
			name        string
			middleware  httpx.Middleware
			handler     httpx.Handler
			description string
		}{
			{
				name: "Conditional error recovery",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					if err != nil {
						// Only recover from specific error types
						if testErr, ok := err.(*TestError); ok && testErr.StatusCode() == 400 {
							t.Logf("Recovering from 400 error: %v", err)
							return nil
						}
						// Propagate other errors
						return err
					}
					return nil
				},
				handler:     utils.CreateErrorHandler(NewTestErrorWithStatus("bad request", 400)),
				description: "Middleware should conditionally recover from specific error types",
			},
			{
				name: "Error wrapping",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					if err != nil {
						// Wrap the error with additional context
						return NewTestErrorWithCause("middleware context", err)
					}
					return nil
				},
				handler:     utils.CreateErrorHandler(NewTestError("original error")),
				description: "Middleware should be able to wrap errors with additional context",
			},
			{
				name: "Error logging and propagation",
				middleware: func(ctx httpx.Context) error {
					err := ctx.Next()
					if err != nil {
						// Log error details but propagate unchanged
						t.Logf("Error details - Type: %T, Message: %s", err, err.Error())
						if httpErr, ok := err.(interface{ StatusCode() int }); ok {
							t.Logf("HTTP Status Code: %d", httpErr.StatusCode())
						}
					}
					return err
				},
				handler:     utils.CreateErrorHandler(NewTestErrorWithStatus("logged error", 500)),
				description: "Middleware should log error details while propagating them",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := engine.Group("")
				router.Use(tc.middleware)
				router.GET(GenerateUniqueTestPath(), tc.handler)
				t.Logf("Error recovery pattern test registered: %s", tc.description)
			})
		}
	})

	t.Run("Complex error scenarios", func(t *testing.T) {
		t.Run("Nested error handling", func(t *testing.T) {
			router := engine.Group("")

			// Layer 1: Outer recovery middleware
			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					t.Logf("Outer layer caught error: %v", err)
					// Only recover from certain errors
					if testErr, ok := err.(*TestError); ok && testErr.message == "recoverable" {
						t.Logf("Outer layer recovering from recoverable error")
						return nil
					}
				}
				return err
			})

			// Layer 2: Logging middleware
			router.Use(func(ctx httpx.Context) error {
				t.Logf("Logging middleware: processing request")
				err := ctx.Next()
				if err != nil {
					t.Logf("Logging middleware: error occurred: %v", err)
				} else {
					t.Logf("Logging middleware: request completed successfully")
				}
				return err
			})

			// Layer 3: Validation middleware
			router.Use(func(ctx httpx.Context) error {
				// Simulate validation logic
				if ctx.Header("X-Invalid") == "true" {
					return NewTestError("validation failed")
				}
				return ctx.Next()
			})

			// Handler
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				errorType := ctx.Query("error")
				switch errorType {
				case "recoverable":
					return NewTestError("recoverable")
				case "fatal":
					return NewTestError("fatal error")
				default:
					return ctx.Text(200, "success")
				}
			})

			t.Logf("Complex nested error handling scenario registered")
		})

		t.Run("Error transformation chain", func(t *testing.T) {
			router := engine.Group("")

			// Each middleware transforms the error in some way
			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					return NewTestErrorWithCause("layer-1", err)
				}
				return nil
			})

			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					return NewTestErrorWithCause("layer-2", err)
				}
				return nil
			})

			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					return NewTestErrorWithCause("layer-3", err)
				}
				return nil
			})

			router.GET(GenerateUniqueTestPath(), utils.CreateErrorHandler(NewTestError("original")))

			t.Logf("Error transformation chain scenario registered")
		})
	})
}
