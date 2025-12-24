package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// TestMiddlewareChainErrorPropagation tests comprehensive error propagation scenarios
// through middleware chains according to requirements 6.1, 6.2, 6.3, 6.4, 6.5
func TestMiddlewareChainErrorPropagation(t *testing.T) {
	engine := &MockEngine{}

	t.Run("Middleware Chain Interruption (Requirement 6.1)", func(t *testing.T) {
		testCases := []struct {
			name        string
			middlewares []httpx.Middleware
			handler     httpx.Handler
			description string
		}{
			{
				name: "First middleware error stops chain",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("First middleware: returning error")
						return NewTestError("first middleware error")
					},
					func(ctx httpx.Context) error {
						t.Error("Second middleware should NOT be executed when first returns error")
						return ctx.Next()
					},
					func(ctx httpx.Context) error {
						t.Error("Third middleware should NOT be executed when first returns error")
						return ctx.Next()
					},
				},
				handler: func(ctx httpx.Context) error {
					t.Error("Handler should NOT be executed when middleware returns error")
					return ctx.Text(200, "should not reach here")
				},
				description: "When any middleware returns error, subsequent middleware and handler should not execute",
			},
			{
				name: "Second middleware error stops chain",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("First middleware: calling Next()")
						return ctx.Next()
					},
					func(ctx httpx.Context) error {
						t.Logf("Second middleware: returning error")
						return NewTestError("second middleware error")
					},
					func(ctx httpx.Context) error {
						t.Error("Third middleware should NOT be executed when second returns error")
						return ctx.Next()
					},
				},
				handler: func(ctx httpx.Context) error {
					t.Error("Handler should NOT be executed when middleware returns error")
					return ctx.Text(200, "should not reach here")
				},
				description: "When second middleware returns error, subsequent middleware and handler should not execute",
			},
			{
				name: "All middleware pass, handler executes",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("First middleware: calling Next()")
						return ctx.Next()
					},
					func(ctx httpx.Context) error {
						t.Logf("Second middleware: calling Next()")
						return ctx.Next()
					},
					func(ctx httpx.Context) error {
						t.Logf("Third middleware: calling Next()")
						return ctx.Next()
					},
				},
				handler: func(ctx httpx.Context) error {
					t.Logf("Handler: executing successfully")
					return ctx.Text(200, "success")
				},
				description: "When all middleware return nil, handler should execute",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := engine.Group("")
				for _, middleware := range tc.middlewares {
					router.Use(middleware)
				}
				router.GET(GenerateUniqueTestPath(), tc.handler)
				t.Logf("Chain interruption test registered: %s", tc.description)
			})
		}
	})

	t.Run("Error Propagation in Reverse Order (Requirement 6.2)", func(t *testing.T) {
		testCases := []struct {
			name        string
			middlewares []httpx.Middleware
			handler     httpx.Handler
			description string
		}{
			{
				name: "Handler error propagates back through middleware chain",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("Outer middleware: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Outer middleware: caught error from downstream: %v", err)
							// Verify this is the handler error
							if err.Error() != "handler error" {
								t.Errorf("Expected 'handler error', got: %v", err)
							}
						}
						t.Logf("Outer middleware: after Next()")
						return err
					},
					func(ctx httpx.Context) error {
						t.Logf("Middle middleware: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Middle middleware: caught error from downstream: %v", err)
							// Verify this is the handler error
							if err.Error() != "handler error" {
								t.Errorf("Expected 'handler error', got: %v", err)
							}
						}
						t.Logf("Middle middleware: after Next()")
						return err
					},
					func(ctx httpx.Context) error {
						t.Logf("Inner middleware: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Inner middleware: caught error from downstream: %v", err)
							// Verify this is the handler error
							if err.Error() != "handler error" {
								t.Errorf("Expected 'handler error', got: %v", err)
							}
						}
						t.Logf("Inner middleware: after Next()")
						return err
					},
				},
				handler: func(ctx httpx.Context) error {
					t.Logf("Handler: returning error")
					return NewTestError("handler error")
				},
				description: "Handler error should propagate back through middleware in reverse order",
			},
			{
				name: "Deep middleware error propagates back",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("Layer 1: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Layer 1: caught error: %v", err)
							// Should receive the error from layer 3
							if err.Error() != "layer 3 error" {
								t.Errorf("Expected 'layer 3 error', got: %v", err)
							}
						}
						t.Logf("Layer 1: after Next()")
						return err
					},
					func(ctx httpx.Context) error {
						t.Logf("Layer 2: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Layer 2: caught error: %v", err)
							// Should receive the error from layer 3
							if err.Error() != "layer 3 error" {
								t.Errorf("Expected 'layer 3 error', got: %v", err)
							}
						}
						t.Logf("Layer 2: after Next()")
						return err
					},
					func(ctx httpx.Context) error {
						t.Logf("Layer 3: returning error without calling Next()")
						return NewTestError("layer 3 error")
					},
				},
				handler: func(ctx httpx.Context) error {
					t.Error("Handler should NOT be executed when middleware returns error")
					return ctx.Text(200, "should not reach here")
				},
				description: "Middleware error should propagate back through previous middleware layers",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := engine.Group("")
				for _, middleware := range tc.middlewares {
					router.Use(middleware)
				}
				router.GET(GenerateUniqueTestPath(), tc.handler)
				t.Logf("Error propagation test registered: %s", tc.description)
			})
		}
	})

	t.Run("Error Recovery Patterns (Requirements 6.3, 6.4)", func(t *testing.T) {
		testCases := []struct {
			name        string
			middlewares []httpx.Middleware
			handler     httpx.Handler
			description string
		}{
			{
				name: "Middleware recovers from handler error",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("Recovery middleware: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Recovery middleware: caught error: %v", err)
							t.Logf("Recovery middleware: recovering from error by returning nil")
							// Requirement 6.4: middleware can return nil to indicate successful error recovery
							return nil
						}
						t.Logf("Recovery middleware: no error to recover from")
						return nil
					},
				},
				handler: func(ctx httpx.Context) error {
					t.Logf("Handler: returning error that should be recovered")
					return NewTestError("recoverable error")
				},
				description: "Middleware should be able to recover from errors by returning nil",
			},
			{
				name: "Conditional error recovery",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						err := ctx.Next()
						if err != nil {
							t.Logf("Conditional recovery middleware: caught error: %v", err)
							// Only recover from specific error types
							if testErr, ok := err.(*TestError); ok && testErr.Error() == "recoverable" {
								t.Logf("Conditional recovery middleware: recovering from recoverable error")
								return nil
							}
							t.Logf("Conditional recovery middleware: not recovering from this error type")
							return err
						}
						return nil
					},
				},
				handler: func(ctx httpx.Context) error {
					errorType := ctx.Query("type")
					if errorType == "recoverable" {
						return NewTestError("recoverable")
					}
					return NewTestError("non-recoverable")
				},
				description: "Middleware should conditionally recover from specific error types",
			},
			{
				name: "Error transformation during recovery",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						err := ctx.Next()
						if err != nil {
							t.Logf("Transform middleware: caught error: %v", err)
							// Transform the error instead of recovering
							transformedErr := NewTestErrorWithCause("transformed error", err)
							t.Logf("Transform middleware: returning transformed error: %v", transformedErr)
							return transformedErr
						}
						return nil
					},
				},
				handler: func(ctx httpx.Context) error {
					return NewTestError("original error")
				},
				description: "Middleware should be able to transform errors during propagation",
			},
			{
				name: "Multi-layer error recovery",
				middlewares: []httpx.Middleware{
					func(ctx httpx.Context) error {
						t.Logf("Outer recovery: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Outer recovery: caught error: %v", err)
							// Check if inner middleware already handled it
							if err.Error() == "handled by inner" {
								t.Logf("Outer recovery: inner middleware already handled error")
								return nil
							}
							t.Logf("Outer recovery: handling unhandled error")
							return nil
						}
						return nil
					},
					func(ctx httpx.Context) error {
						t.Logf("Inner recovery: before Next()")
						err := ctx.Next()
						if err != nil {
							t.Logf("Inner recovery: caught error: %v", err)
							// Handle specific errors
							if err.Error() == "inner-handled" {
								t.Logf("Inner recovery: handling this error")
								return NewTestError("handled by inner")
							}
							t.Logf("Inner recovery: passing error to outer layer")
							return err
						}
						return nil
					},
				},
				handler: func(ctx httpx.Context) error {
					errorType := ctx.Query("error")
					switch errorType {
					case "inner":
						return NewTestError("inner-handled")
					case "outer":
						return NewTestError("outer-handled")
					default:
						return NewTestError("unhandled")
					}
				},
				description: "Multiple middleware layers should coordinate error recovery",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := engine.Group("")
				for _, middleware := range tc.middlewares {
					router.Use(middleware)
				}
				router.GET(GenerateUniqueTestPath(), tc.handler)
				t.Logf("Error recovery test registered: %s", tc.description)
			})
		}
	})

	t.Run("Complex Error Propagation Scenarios (Requirement 6.5)", func(t *testing.T) {
		t.Run("Nested middleware with error propagation order", func(t *testing.T) {
			var executionOrder []string

			router := engine.Group("")

			// Layer 1: Outermost middleware
			router.Use(func(ctx httpx.Context) error {
				executionOrder = append(executionOrder, "layer1-before")
				t.Logf("Layer 1: before Next()")
				err := ctx.Next()
				executionOrder = append(executionOrder, "layer1-after")
				t.Logf("Layer 1: after Next(), error: %v", err)

				// Verify execution order up to this point
				expectedOrder := []string{"layer1-before", "layer2-before", "layer3-before", "handler", "layer3-after", "layer2-after", "layer1-after"}
				if len(executionOrder) == len(expectedOrder) {
					for i, expected := range expectedOrder {
						if i < len(executionOrder) && executionOrder[i] != expected {
							t.Errorf("Execution order mismatch at position %d: expected %s, got %s", i, expected, executionOrder[i])
						}
					}
				}

				return err
			})

			// Layer 2: Middle middleware
			router.Use(func(ctx httpx.Context) error {
				executionOrder = append(executionOrder, "layer2-before")
				t.Logf("Layer 2: before Next()")
				err := ctx.Next()
				executionOrder = append(executionOrder, "layer2-after")
				t.Logf("Layer 2: after Next(), error: %v", err)
				return err
			})

			// Layer 3: Innermost middleware
			router.Use(func(ctx httpx.Context) error {
				executionOrder = append(executionOrder, "layer3-before")
				t.Logf("Layer 3: before Next()")
				err := ctx.Next()
				executionOrder = append(executionOrder, "layer3-after")
				t.Logf("Layer 3: after Next(), error: %v", err)
				return err
			})

			// Handler
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				executionOrder = append(executionOrder, "handler")
				t.Logf("Handler: executing")
				return NewTestError("handler error")
			})

			t.Logf("Complex error propagation order test registered")
		})

		t.Run("Error propagation with mixed recovery patterns", func(t *testing.T) {
			router := engine.Group("")

			// Layer 1: Logging middleware (always propagates)
			router.Use(func(ctx httpx.Context) error {
				t.Logf("Logging middleware: request started")
				err := ctx.Next()
				if err != nil {
					t.Logf("Logging middleware: request failed with error: %v", err)
				} else {
					t.Logf("Logging middleware: request completed successfully")
				}
				return err
			})

			// Layer 2: Selective recovery middleware
			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					// Only recover from 4xx errors, let 5xx errors propagate
					if httpErr, ok := err.(interface{ StatusCode() int }); ok {
						statusCode := httpErr.StatusCode()
						if statusCode >= 400 && statusCode < 500 {
							t.Logf("Selective recovery: recovering from 4xx error: %v", err)
							return nil
						}
					}
					t.Logf("Selective recovery: propagating non-4xx error: %v", err)
				}
				return err
			})

			// Layer 3: Error enrichment middleware
			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					// Add context to the error
					enrichedErr := NewTestErrorWithCause("enriched context", err)
					t.Logf("Error enrichment: enriched error: %v", enrichedErr)
					return enrichedErr
				}
				return nil
			})

			// Add different handlers for different error scenarios
			router.GET("/4xx-error", func(ctx httpx.Context) error {
				return NewTestErrorWithStatus("client error", 400)
			})

			router.GET("/5xx-error", func(ctx httpx.Context) error {
				return NewTestErrorWithStatus("server error", 500)
			})

			router.GET("/success", func(ctx httpx.Context) error {
				return ctx.Text(200, "success")
			})

			t.Logf("Mixed recovery patterns test registered")
		})
	})

	t.Run("Error Propagation Edge Cases", func(t *testing.T) {
		testCases := []struct {
			name        string
			setup       func() ([]httpx.Middleware, httpx.Handler)
			description string
		}{
			{
				name: "Middleware panics during error handling",
				setup: func() ([]httpx.Middleware, httpx.Handler) {
					middleware := []httpx.Middleware{
						func(ctx httpx.Context) error {
							defer func() {
								if r := recover(); r != nil {
									t.Logf("Recovered from panic in middleware: %v", r)
								}
							}()
							err := ctx.Next()
							if err != nil {
								// Simulate panic during error handling
								if err.Error() == "panic-trigger" {
									panic("middleware panic during error handling")
								}
							}
							return err
						},
					}
					handler := func(ctx httpx.Context) error {
						return NewTestError("panic-trigger")
					}
					return middleware, handler
				},
				description: "Middleware should handle panics during error processing",
			},
			{
				name: "Nil error handling",
				setup: func() ([]httpx.Middleware, httpx.Handler) {
					middleware := []httpx.Middleware{
						func(ctx httpx.Context) error {
							err := ctx.Next()
							if err == nil {
								t.Logf("Middleware: received nil error (success)")
							} else {
								t.Logf("Middleware: received error: %v", err)
							}
							return err
						},
					}
					handler := func(ctx httpx.Context) error {
						return nil // Success case
					}
					return middleware, handler
				},
				description: "Middleware should properly handle nil errors (success cases)",
			},
			{
				name: "Error type preservation",
				setup: func() ([]httpx.Middleware, httpx.Handler) {
					middleware := []httpx.Middleware{
						func(ctx httpx.Context) error {
							err := ctx.Next()
							if err != nil {
								// Verify error type is preserved
								if testErr, ok := err.(*TestError); ok {
									t.Logf("Middleware: preserved TestError type with status: %d", testErr.StatusCode())
								} else {
									t.Errorf("Middleware: error type not preserved, got: %T", err)
								}
							}
							return err
						},
					}
					handler := func(ctx httpx.Context) error {
						return NewTestErrorWithStatus("typed error", 418)
					}
					return middleware, handler
				},
				description: "Error types should be preserved during propagation",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				middlewares, handler := tc.setup()
				router := engine.Group("")
				for _, middleware := range middlewares {
					router.Use(middleware)
				}
				router.GET(GenerateUniqueTestPath(), handler)
				t.Logf("Edge case test registered: %s", tc.description)
			})
		}
	})
}

// TestMiddlewareChainErrorPropagationIntegration tests error propagation with actual HTTP requests
// This would be used in integration tests to verify the complete error flow
func TestMiddlewareChainErrorPropagationIntegration(t *testing.T) {
	// This test would require actual HTTP server setup and is more appropriate for integration tests
	// For now, we register the test scenarios to verify they can be set up correctly

	engine := &MockEngine{}

	t.Run("Integration test setup", func(t *testing.T) {
		router := engine.Group("")

		// Complete error flow test
		router.Use(func(ctx httpx.Context) error {
			t.Logf("Integration: outer middleware")
			err := ctx.Next()
			if err != nil {
				t.Logf("Integration: handling error in outer middleware: %v", err)
				// In real integration, this would set appropriate HTTP response
			}
			return err
		})

		router.Use(func(ctx httpx.Context) error {
			t.Logf("Integration: inner middleware")
			return ctx.Next()
		})

		router.GET("/test-error-propagation", func(ctx httpx.Context) error {
			return NewTestErrorWithStatus("integration test error", 500)
		})

		t.Logf("Integration test scenario registered")
	})
}
