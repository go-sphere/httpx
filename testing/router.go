package testing

import (
	"os"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// RouterTester provides comprehensive testing tools for the Router interface.
type RouterTester struct {
	engine httpx.Engine
}

// NewRouterTester creates a new RouterTester instance.
func NewRouterTester(engine httpx.Engine) *RouterTester {
	return &RouterTester{
		engine: engine,
	}
}

// TestHandle tests the Handle() method for registering route handlers.
func (rt *RouterTester) TestHandle(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	// Test registering handlers for different HTTP methods
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	for _, method := range methods {
		path := "/handle-" + strings.ToLower(method) + "-test"

		router.Handle(method, path, func(ctx httpx.Context) {
			ctx.Text(200, "OK "+method)
		})

		// In a real implementation, we would make HTTP requests to verify
		// that the routes were registered correctly. For now, we verify
		// that the Handle method can be called without errors.
	}

	// Test registering handler with path parameters
	router.Handle("GET", "/handle-users/:id", func(ctx httpx.Context) {
		id := ctx.Param("id")
		ctx.Text(200, "User ID: "+id)
	})

	// Test registering handler with wildcard
	router.Handle("GET", "/handle-files/*filepath", func(ctx httpx.Context) {
		filepath := ctx.Param("filepath")
		ctx.Text(200, "File: "+filepath)
	})
}

// TestHTTPMethods tests the HTTP method shortcuts (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS).
func (rt *RouterTester) TestHTTPMethods(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	// Test GET method
	router.GET("/get-test", func(ctx httpx.Context) {
		ctx.Text(200, "GET OK")
	})

	// Test POST method
	router.POST("/post-test", func(ctx httpx.Context) {
		ctx.Text(200, "POST OK")
	})

	// Test PUT method
	router.PUT("/put-test", func(ctx httpx.Context) {
		ctx.Text(200, "PUT OK")
	})

	// Test DELETE method
	router.DELETE("/delete-test", func(ctx httpx.Context) {
		ctx.Text(200, "DELETE OK")
	})

	// Test PATCH method
	router.PATCH("/patch-test", func(ctx httpx.Context) {
		ctx.Text(200, "PATCH OK")
	})

	// Test HEAD method
	router.HEAD("/head-test", func(ctx httpx.Context) {
		ctx.Status(200)
	})

	// Test OPTIONS method
	router.OPTIONS("/options-test", func(ctx httpx.Context) {
		ctx.Text(200, "OPTIONS OK")
	})

	// Test method shortcuts with path parameters
	router.GET("/users/:id", func(ctx httpx.Context) {
		id := ctx.Param("id")
		ctx.Text(200, "User: "+id)
	})

	router.POST("/users/:id/posts", func(ctx httpx.Context) {
		id := ctx.Param("id")
		ctx.Text(201, "Created post for user: "+id)
	})
}

// TestAny tests the Any() method for registering handlers for all HTTP methods.
func (rt *RouterTester) TestAny(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	// Test Any method registration
	router.Any("/any-test", func(ctx httpx.Context) {
		method := ctx.Method()
		ctx.Text(200, "Any method: "+method)
	})

	// Test Any with path parameters
	router.Any("/any-users/:id", func(ctx httpx.Context) {
		method := ctx.Method()
		id := ctx.Param("id")
		ctx.Text(200, "Any method "+method+" for user: "+id)
	})

	// In a real implementation, we would test that the handler responds
	// to all HTTP methods (GET, POST, PUT, DELETE, etc.)
}

// TestGroup tests the Group() method for creating route groups.
func (rt *RouterTester) TestGroup(t *testing.T) {
	t.Helper()

	// Test creating a basic group
	apiGroup := rt.engine.Group("/api")

	// Test that the group has the correct base path
	if basePath := apiGroup.BasePath(); basePath != "/api" {
		t.Errorf("Expected base path '/api', got '%s'", basePath)
	}

	// Test registering routes in the group
	apiGroup.GET("/users", func(ctx httpx.Context) {
		ctx.Text(200, "API Users")
	})

	apiGroup.POST("/users", func(ctx httpx.Context) {
		ctx.Text(201, "Created User")
	})

	// Test creating nested groups
	v1Group := apiGroup.Group("/v1")
	if basePath := v1Group.BasePath(); basePath != "/api/v1" {
		t.Errorf("Expected nested base path '/api/v1', got '%s'", basePath)
	}

	v1Group.GET("/posts", func(ctx httpx.Context) {
		ctx.Text(200, "V1 Posts")
	})

	// Test creating group with middleware
	authGroup := rt.engine.Group("/auth", func(ctx httpx.Context) {
		// Auth middleware
		token := ctx.Header("Authorization")
		if token == "" {
			ctx.Status(401)
			ctx.Text(401, "Unauthorized")
			ctx.Abort()
			return
		}
		ctx.Next()
	})

	if basePath := authGroup.BasePath(); basePath != "/auth" {
		t.Errorf("Expected auth base path '/auth', got '%s'", basePath)
	}

	authGroup.GET("/profile", func(ctx httpx.Context) {
		ctx.Text(200, "User Profile")
	})
}

// TestMiddleware tests the Use() method for registering middleware.
func (rt *RouterTester) TestMiddleware(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	// Test registering global middleware
	router.Use(func(ctx httpx.Context) {
		ctx.SetHeader("X-Global-Middleware", "true")
		ctx.Next()
	})

	// Test registering multiple middleware
	router.Use(
		func(ctx httpx.Context) {
			ctx.SetHeader("X-First-Middleware", "true")
			ctx.Next()
		},
		func(ctx httpx.Context) {
			ctx.SetHeader("X-Second-Middleware", "true")
			ctx.Next()
		},
	)

	// Register a test route
	router.GET("/middleware-test", func(ctx httpx.Context) {
		ctx.Text(200, "Middleware Test")
	})

	// Test group-specific middleware
	apiGroup := rt.engine.Group("/api")
	apiGroup.Use(func(ctx httpx.Context) {
		ctx.SetHeader("X-API-Middleware", "true")
		ctx.Next()
	})

	apiGroup.GET("/test", func(ctx httpx.Context) {
		ctx.Text(200, "API Test")
	})

	// In a real implementation, we would make HTTP requests and verify
	// that the middleware headers are present in the responses
}

// TestBasePath tests the BasePath() method for returning correct base paths.
func (rt *RouterTester) TestBasePath(t *testing.T) {
	t.Helper()

	// Test root group base path - ginx returns "/" for root group
	rootGroup := rt.engine.Group("")
	if basePath := rootGroup.BasePath(); basePath != "/" {
		t.Errorf("Expected base path '/' for root group, got '%s'", basePath)
	}

	// Test simple group base path
	apiGroup := rt.engine.Group("/api")
	if basePath := apiGroup.BasePath(); basePath != "/api" {
		t.Errorf("Expected base path '/api', got '%s'", basePath)
	}

	// Test nested group base paths
	v1Group := apiGroup.Group("/v1")
	if basePath := v1Group.BasePath(); basePath != "/api/v1" {
		t.Errorf("Expected nested base path '/api/v1', got '%s'", basePath)
	}

	v2Group := apiGroup.Group("/v2")
	if basePath := v2Group.BasePath(); basePath != "/api/v2" {
		t.Errorf("Expected nested base path '/api/v2', got '%s'", basePath)
	}

	// Test deeply nested groups
	usersGroup := v1Group.Group("/users")
	if basePath := usersGroup.BasePath(); basePath != "/api/v1/users" {
		t.Errorf("Expected deeply nested base path '/api/v1/users', got '%s'", basePath)
	}

	// Test group with trailing slash
	trailingGroup := rt.engine.Group("/trailing/")
	expectedPath := "/trailing/" // or "/trailing" depending on implementation
	if basePath := trailingGroup.BasePath(); basePath != expectedPath && basePath != "/trailing" {
		t.Errorf("Expected base path '/trailing/' or '/trailing', got '%s'", basePath)
	}
}

// TestStatic tests the Static() and StaticFS() methods for serving static files.
func (rt *RouterTester) TestStatic(t *testing.T) {
	t.Helper()

	// Test basic static file serving without middleware
	t.Run("BasicStatic", func(t *testing.T) {
		router := rt.engine.Group("")

		// Test Static method for serving files from a directory
		router.Static("/static", "./testdata")

		// Test StaticFS method for serving files from an embedded filesystem
		// Note: In a real implementation, we would use embed.FS or similar
		// For now, we just test that the method can be called with a proper fs.FS
		// Using os.DirFS which implements fs.FS
		router.StaticFS("/assets", os.DirFS("./assets"))

		// Test static routes with different prefixes
		router.Static("/images", "./images")
		router.Static("/css", "./css")
		router.Static("/js", "./js")
	})

	// Test static file serving with authentication middleware
	t.Run("StaticWithAuth", func(t *testing.T) {
		// Create a group with authentication middleware
		authGroup := rt.engine.Group("/protected")

		// Add authentication middleware that checks for Authorization header
		authGroup.Use(func(c httpx.Context) {
			auth := c.Header("Authorization")
			if auth == "" {
				c.Status(401)
				c.Abort()
				return
			}

			// Simple token validation (in real app, this would be more sophisticated)
			if auth != "Bearer valid-token" {
				c.Status(403)
				c.Abort()
				return
			}

			c.Next()
		})

		// Add static routes to the protected group
		authGroup.Static("/files", "./protected-files")
		authGroup.StaticFS("/docs", os.DirFS("./protected-docs"))

		// Test scenarios:
		// 1. Request without Authorization header should return 401
		// 2. Request with invalid token should return 403
		// 3. Request with valid token should serve the file
	})

	// Test static file serving with role-based access control
	t.Run("StaticWithRBAC", func(t *testing.T) {
		// Create groups for different access levels
		adminGroup := rt.engine.Group("/admin")
		userGroup := rt.engine.Group("/user")

		// Admin middleware - requires admin role
		adminGroup.Use(func(c httpx.Context) {
			role := c.Header("X-User-Role")
			if role != "admin" {
				c.Status(403)
				c.Abort()
				return
			}
			c.Next()
		})

		// User middleware - requires user or admin role
		userGroup.Use(func(c httpx.Context) {
			role := c.Header("X-User-Role")
			if role != "user" && role != "admin" {
				c.Status(403)
				c.Abort()
				return
			}
			c.Next()
		})

		// Add static routes with different access levels
		adminGroup.Static("/config", "./admin-config")
		adminGroup.StaticFS("/logs", os.DirFS("./admin-logs"))
		userGroup.Static("/uploads", "./user-uploads")
		userGroup.StaticFS("/templates", os.DirFS("./user-templates"))
	})

	// Test static file serving with custom middleware chain
	t.Run("StaticWithMiddlewareChain", func(t *testing.T) {
		protectedGroup := rt.engine.Group("/secure")

		// Add multiple middleware layers
		protectedGroup.Use(
			// Rate limiting middleware
			func(c httpx.Context) {
				// Mock rate limiting check
				if c.Header("X-Rate-Limit-Exceeded") == "true" {
					c.Status(429)
					c.Abort()
					return
				}
				c.Next()
			},
			// IP whitelist middleware
			func(c httpx.Context) {
				clientIP := c.Header("X-Forwarded-For")
				if clientIP == "" {
					_ = c.ClientIP() // We don't need to use this value, just call it for testing
				}

				// Mock IP whitelist check
				if c.Header("X-Blocked-IP") == "true" {
					c.Status(403)
					c.Abort()
					return
				}
				c.Next()
			},
			// Authentication middleware
			func(c httpx.Context) {
				token := c.Header("X-API-Key")
				if token != "secret-api-key" {
					c.Status(401)
					c.Abort()
					return
				}
				c.Next()
			},
		)

		// Add static routes to the protected group
		protectedGroup.Static("/private", "./private-files")
		protectedGroup.StaticFS("/confidential", os.DirFS("./confidential"))
	})

	// Test static file serving with conditional middleware
	t.Run("StaticWithConditionalAuth", func(t *testing.T) {
		conditionalGroup := rt.engine.Group("/conditional")

		// Middleware that only applies auth to certain file types
		conditionalGroup.Use(func(c httpx.Context) {
			path := c.Path()

			// Only require auth for sensitive file types
			sensitiveExtensions := []string{".pdf", ".doc", ".xlsx", ".zip"}
			requiresAuth := false

			for _, ext := range sensitiveExtensions {
				if strings.HasSuffix(path, ext) {
					requiresAuth = true
					break
				}
			}

			if requiresAuth {
				auth := c.Header("Authorization")
				if auth != "Bearer document-access-token" {
					c.Status(401)
					c.Abort()
					return
				}
			}

			c.Next()
		})

		conditionalGroup.Static("/mixed", "./mixed-content")
		conditionalGroup.StaticFS("/library", os.DirFS("./document-library"))
	})

	// In a real implementation, we would:
	// 1. Create test files in the directories
	// 2. Make HTTP requests to the static routes with different headers
	// 3. Verify that the correct files are served or access is denied
	// 4. Test that directory traversal is prevented
	// 5. Test that proper MIME types are set
	// 6. Verify middleware execution order
	// 7. Test error handling in middleware chain
}

// RunAllTests runs all Router interface tests.
func (rt *RouterTester) RunAllTests(t *testing.T) {
	t.Helper()

	t.Run("Handle", rt.TestHandle)
	t.Run("HTTPMethods", rt.TestHTTPMethods)
	t.Run("Any", rt.TestAny)
	t.Run("Group", rt.TestGroup)
	t.Run("Middleware", rt.TestMiddleware)
	t.Run("BasePath", rt.TestBasePath)
	t.Run("Static", rt.TestStatic)
}

// Helper functions for creating test scenarios
