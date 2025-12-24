package integration

// skip_managers.go - Centralized skip manager configurations for all frameworks

// NewTestSkipManager creates a new TestSkipManager instance
func NewTestSkipManager() *TestSkipManager {
	return &TestSkipManager{
		skippedTests: make(map[string][]SkippableTest),
	}
}

// setupGinxSkipManager creates skip manager for Ginx (reference implementation, no skips)
func setupGinxSkipManager() *TestSkipManager {
	return NewTestSkipManager() // Ginx is reference, no skips
}

// setupFiberxSkipManager creates skip manager for Fiberx with known failing tests
func setupFiberxSkipManager() *TestSkipManager {
	skipManager := NewTestSkipManager()

	// Add known failing tests for fiberx - these should be updated as issues are fixed
	// Fiber doesn't support custom HTTP methods
	skipManager.AddSkippedTest("fiberx", "Router", "Handle", "Fiber doesn't support custom HTTP methods like CUSTOM")

	// Uncomment and adjust these as needed based on actual test failures:
	// skipManager.AddSkippedTest("fiberx", "Binder", "BindJSON", "Known issue with JSON binding in fiberx")
	// skipManager.AddSkippedTest("fiberx", "FormAccess", "FormFile", "Multipart form handling differences")
	// skipManager.AddSkippedTest("fiberx", "RequestInfo", "ClientIP", "Client IP detection differences")

	return skipManager
}

// setupEchoxSkipManager creates skip manager for Echox with known failing tests
func setupEchoxSkipManager() *TestSkipManager {
	skipManager := NewTestSkipManager()

	// Add known failing tests for echox - these should be updated as issues are fixed
	// Example skipped tests (these may need to be adjusted based on actual test results):

	// Uncomment and adjust these as needed based on actual test failures:
	// skipManager.AddSkippedTest("echox", "Binder", "BindURI", "URI parameter binding differences")
	// skipManager.AddSkippedTest("echox", "RequestInfo", "Params", "Parameter handling differences")
	// skipManager.AddSkippedTest("echox", "Responder", "SetCookie", "Cookie handling differences")

	return skipManager
}

// setupHertzxSkipManager creates skip manager for Hertzx with known failing tests
func setupHertzxSkipManager() *TestSkipManager {
	skipManager := NewTestSkipManager()

	// Add known failing tests for hertzx - these should be updated as issues are fixed

	// Currently no tests need to be skipped - framework-specific behaviors are handled in the tests

	// Uncomment and adjust these as needed based on actual test failures:
	// skipManager.AddSkippedTest("hertzx", "RequestInfo", "Headers", "Header handling differences")
	// skipManager.AddSkippedTest("hertzx", "Responder", "JSON", "JSON response differences")
	// skipManager.AddSkippedTest("hertzx", "Router", "Static", "Static file serving differences")

	return skipManager
}
