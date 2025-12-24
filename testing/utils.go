package testing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Global counter for generating unique paths
var pathCounter int64

// TestHelper provides common utilities for testing
type TestHelper struct {
	config *TestConfig
}

// NewTestHelper creates a new test helper with the given configuration
func NewTestHelper(config *TestConfig) *TestHelper {
	if config == nil {
		config = DefaultTestConfig()
	}
	return &TestHelper{config: config}
}

// LogWithContext logs a message with test context information
func (h *TestHelper) LogWithContext(t *testing.T, ctx *TestContext, format string, args ...interface{}) {
	t.Helper()
	message := fmt.Sprintf(format, args...)
	contextualMessage := fmt.Sprintf("[%s/%s.%s] %s", ctx.Framework, ctx.Interface, ctx.Method, message)
	t.Log(contextualMessage)
}

// LogVerboseWithContext logs a verbose message with test context information
func (h *TestHelper) LogVerboseWithContext(t *testing.T, ctx *TestContext, format string, args ...interface{}) {
	t.Helper()
	if h.config.VerboseLogging {
		h.LogWithContext(t, ctx, format, args...)
	}
}

// LogTestStart logs the start of a test with context
func (h *TestHelper) LogTestStart(t *testing.T, ctx *TestContext) {
	t.Helper()
	h.LogVerboseWithContext(t, ctx, "Starting test: %s", ctx.TestCase)
}

// LogTestComplete logs the completion of a test with context
func (h *TestHelper) LogTestComplete(t *testing.T, ctx *TestContext) {
	t.Helper()
	h.LogVerboseWithContext(t, ctx, "Completed test: %s", ctx.TestCase)
}

// LogTestSkipped logs when a test is skipped with context
func (h *TestHelper) LogTestSkipped(t *testing.T, ctx *TestContext, reason string) {
	t.Helper()
	skipMsg := fmt.Sprintf("[%s/%s.%s] SKIPPED: %s (Test: %s)", 
		ctx.Framework, ctx.Interface, ctx.Method, reason, ctx.TestCase)
	t.Log(skipMsg)
}

// ReportTestFailure reports a test failure with enhanced context
func (h *TestHelper) ReportTestFailure(t *testing.T, ctx *TestContext, failure string, details ...interface{}) {
	t.Helper()
	var detailStr string
	if len(details) > 0 {
		detailStr = fmt.Sprintf(" - Details: %v", details)
	}
	
	errorMsg := fmt.Sprintf("[FAILURE] %s/%s.%s: %s%s (Test: %s)", 
		ctx.Framework, ctx.Interface, ctx.Method, failure, detailStr, ctx.TestCase)
	t.Error(errorMsg)
}

// ReportInterfaceTestStart logs the start of interface testing
func (h *TestHelper) ReportInterfaceTestStart(t *testing.T, framework, interfaceName string) {
	t.Helper()
	t.Logf("[%s] Starting %s interface tests", framework, interfaceName)
}

// ReportInterfaceTestComplete logs the completion of interface testing
func (h *TestHelper) ReportInterfaceTestComplete(t *testing.T, framework, interfaceName string, passed, failed int) {
	t.Helper()
	status := "PASSED"
	if failed > 0 {
		status = "FAILED"
	}
	t.Logf("[%s] %s interface tests %s - Passed: %d, Failed: %d", 
		framework, interfaceName, status, passed, failed)
}

// ReportFrameworkTestSummary logs a summary of all framework tests
func (h *TestHelper) ReportFrameworkTestSummary(t *testing.T, framework string, results map[string]TestResult) {
	t.Helper()
	
	totalPassed := 0
	totalFailed := 0
	
	t.Logf("[%s] Framework Test Summary:", framework)
	for interfaceName, result := range results {
		totalPassed += result.Passed
		totalFailed += result.Failed
		status := "PASSED"
		if result.Failed > 0 {
			status = "FAILED"
		}
		t.Logf("  %s: %s (Passed: %d, Failed: %d)", interfaceName, status, result.Passed, result.Failed)
	}
	
	overallStatus := "PASSED"
	if totalFailed > 0 {
		overallStatus = "FAILED"
	}
	t.Logf("[%s] Overall: %s - Total Passed: %d, Total Failed: %d", 
		framework, overallStatus, totalPassed, totalFailed)
}

// TestResult represents the result of running tests for an interface
type TestResult struct {
	Interface string
	Passed    int
	Failed    int
	Skipped   int
}

// CreateJSONRequest creates an HTTP request with JSON body
func (h *TestHelper) CreateJSONRequest(method, urlStr string, body interface{}) (*http.Request, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("failed to encode JSON: %w", err)
		}
	}
	
	req, err := http.NewRequest(method, urlStr, &buf)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// CreateFormRequest creates an HTTP request with form data
func (h *TestHelper) CreateFormRequest(method, urlStr string, formData map[string]string) (*http.Request, error) {
	form := make(url.Values)
	for key, value := range formData {
		form.Set(key, value)
	}
	
	req, err := http.NewRequest(method, urlStr, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// CreateMultipartRequest creates an HTTP request with multipart form data
func (h *TestHelper) CreateMultipartRequest(method, urlStr string, fields map[string]string, files map[string][]byte) (*http.Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	
	// Add form fields
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, fmt.Errorf("failed to write field %s: %w", key, err)
		}
	}
	
	// Add files
	for filename, content := range files {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create form file %s: %w", filename, err)
		}
		if _, err := part.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write file content for %s: %w", filename, err)
		}
	}
	
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}
	
	req, err := http.NewRequest(method, urlStr, &buf)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// TestContext holds context information for enhanced error reporting
type TestContext struct {
	Framework     string
	Interface     string
	Method        string
	TestCase      string
	FrameworkName string
}

// NewTestContext creates a new test context for enhanced error reporting
func NewTestContext(framework, interfaceName, method, testCase string) *TestContext {
	return &TestContext{
		Framework:     framework,
		Interface:     interfaceName,
		Method:        method,
		TestCase:      testCase,
		FrameworkName: framework,
	}
}

// FormatError formats an error message with context information
func (tc *TestContext) FormatError(message string, args ...interface{}) string {
	baseMessage := fmt.Sprintf(message, args...)
	return fmt.Sprintf("[%s/%s.%s] %s (Test: %s)", 
		tc.Framework, tc.Interface, tc.Method, baseMessage, tc.TestCase)
}

// AssertEqual checks if two values are equal and fails the test if not
func AssertEqual(t *testing.T, expected, actual interface{}, message string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", message, expected, actual)
	}
}

// AssertEqualWithContext checks if two values are equal with enhanced error reporting
func AssertEqualWithContext(t *testing.T, ctx *TestContext, expected, actual interface{}, message string) {
	t.Helper()
	if expected != actual {
		errorMsg := ctx.FormatError("%s: expected %v, got %v", message, expected, actual)
		t.Error(errorMsg)
	}
}

// AssertNotEqual checks if two values are not equal and fails the test if they are
func AssertNotEqual(t *testing.T, expected, actual interface{}, message string) {
	t.Helper()
	if expected == actual {
		t.Errorf("%s: expected values to be different, but both were %v", message, expected)
	}
}

// AssertNotEqualWithContext checks if two values are not equal with enhanced error reporting
func AssertNotEqualWithContext(t *testing.T, ctx *TestContext, expected, actual interface{}, message string) {
	t.Helper()
	if expected == actual {
		errorMsg := ctx.FormatError("%s: expected values to be different, but both were %v", message, expected)
		t.Error(errorMsg)
	}
}

// AssertNoError checks if an error is nil and fails the test if not
func AssertNoError(t *testing.T, err error, message string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", message, err)
	}
}

// AssertNoErrorWithContext checks if an error is nil with enhanced error reporting
func AssertNoErrorWithContext(t *testing.T, ctx *TestContext, err error, message string) {
	t.Helper()
	if err != nil {
		errorMsg := ctx.FormatError("%s: unexpected error: %v", message, err)
		t.Error(errorMsg)
	}
}

// AssertError checks if an error is not nil and fails the test if it is
func AssertError(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error but got nil", message)
	}
}

// AssertErrorWithContext checks if an error is not nil with enhanced error reporting
func AssertErrorWithContext(t *testing.T, ctx *TestContext, err error, message string) {
	t.Helper()
	if err == nil {
		errorMsg := ctx.FormatError("%s: expected error but got nil", message)
		t.Error(errorMsg)
	}
}

// AssertContains checks if a string contains a substring
func AssertContains(t *testing.T, haystack, needle, message string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: expected %q to contain %q", message, haystack, needle)
	}
}

// AssertContainsWithContext checks if a string contains a substring with enhanced error reporting
func AssertContainsWithContext(t *testing.T, ctx *TestContext, haystack, needle, message string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		errorMsg := ctx.FormatError("%s: expected %q to contain %q", message, haystack, needle)
		t.Error(errorMsg)
	}
}

// FailWithContext fails the test with enhanced context information
func FailWithContext(t *testing.T, ctx *TestContext, message string, args ...interface{}) {
	t.Helper()
	errorMsg := ctx.FormatError(message, args...)
	t.Error(errorMsg)
}

// SkipWithContext skips the test with enhanced context information
func SkipWithContext(t *testing.T, ctx *TestContext, reason string, args ...interface{}) {
	t.Helper()
	skipMsg := ctx.FormatError("SKIPPED: "+reason, args...)
	t.Skip(skipMsg)
}

// ReadBody reads and returns the body content as a string
func ReadBody(body io.Reader) (string, error) {
	if body == nil {
		return "", nil
	}
	
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	
	return string(data), nil
}

// GenerateTestData creates test data of the specified size
func GenerateTestData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	return data
}

// CreateTestStruct creates a TestStruct with default test values
func CreateTestStruct() TestStruct {
	return TestStruct{
		Name:  "testuser",
		Age:   25,
		Email: "test@example.com",
	}
}

// CreateNestedTestStruct creates a NestedTestStruct with default test values
func CreateNestedTestStruct() NestedTestStruct {
	return NestedTestStruct{
		User: CreateTestStruct(),
		Address: Address{
			Street: "123 Test St",
			City:   "Test City",
			Zip:    "12345",
		},
	}
}

// ValidateTestStruct validates that a TestStruct has expected values
func ValidateTestStruct(t *testing.T, actual TestStruct, expected TestStruct, message string) {
	t.Helper()
	AssertEqual(t, expected.Name, actual.Name, message+" - Name mismatch")
	AssertEqual(t, expected.Age, actual.Age, message+" - Age mismatch")
	AssertEqual(t, expected.Email, actual.Email, message+" - Email mismatch")
}

// CreateTestHeaders creates a map of test headers
func CreateTestHeaders() map[string]string {
	return map[string]string{
		"X-Test-Header": "test-value",
		"X-Name":        "testuser",
		"X-Age":         "25",
		"Content-Type":  "application/json",
	}
}

// CreateTestCookies creates a map of test cookies
func CreateTestCookies() map[string]string {
	return map[string]string{
		"session_id": "test-session-123",
		"user_pref":  "dark-mode",
		"csrf_token": "test-csrf-token",
	}
}

// ShouldSkipSlowTest checks if slow tests should be skipped based on configuration
func (h *TestHelper) ShouldSkipSlowTest() bool {
	return h.config.SkipSlowTests
}

// LogVerbose logs a message only if verbose logging is enabled
func (h *TestHelper) LogVerbose(t *testing.T, format string, args ...interface{}) {
	if h.config.VerboseLogging {
		t.Logf(format, args...)
	}
}

// GetTestDataSize returns the configured test data size
func (h *TestHelper) GetTestDataSize() int {
	return h.config.TestDataSize
}

// GetRequestTimeout returns the configured request timeout
func (h *TestHelper) GetRequestTimeout() time.Duration {
	return h.config.RequestTimeout
}

// GetMaxRetries returns the configured maximum retries
func (h *TestHelper) GetMaxRetries() int {
	return h.config.MaxRetries
}

// CreateTestDataWithSize creates test data using the configured size
func (h *TestHelper) CreateTestDataWithSize() []byte {
	return GenerateTestData(h.config.TestDataSize)
}

// GenerateUniquePath generates a unique path for testing to avoid route conflicts
func GenerateUniquePath(prefix string) string {
	counter := atomic.AddInt64(&pathCounter, 1)
	return fmt.Sprintf("/%s-%d", prefix, counter)
}

// GenerateUniqueTestPath generates a unique test path with a default prefix
func GenerateUniqueTestPath() string {
	return GenerateUniquePath("test")
}

// GenerateUniqueParamPath generates a unique parameterized path
func GenerateUniqueParamPath(pattern string) string {
	counter := atomic.AddInt64(&pathCounter, 1)
	// Replace common patterns with unique versions
	if strings.Contains(pattern, "/users/:id") {
		return fmt.Sprintf("/users-%d/:id", counter)
	}
	if strings.Contains(pattern, "/users/:userId/posts/:postId") {
		return fmt.Sprintf("/users-%d/:userId/posts/:postId", counter)
	}
	if strings.Contains(pattern, "/users/:name/:age") {
		return fmt.Sprintf("/users-%d/:name/:age", counter)
	}
	if strings.Contains(pattern, "/users/:name") {
		return fmt.Sprintf("/users-%d/:name", counter)
	}
	if strings.Contains(pattern, "/api/v1/users/:id/posts/:postId") {
		return fmt.Sprintf("/api/v1/users-%d/:id/posts/:postId", counter)
	}
	// Default: just add counter to the beginning
	return fmt.Sprintf("/route-%d%s", counter, pattern)
}