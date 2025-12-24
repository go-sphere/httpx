package testing

import (
	"io"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// BodyAccessTester tests the BodyAccess interface methods
type BodyAccessTester struct {
	engine httpx.Engine
}

// NewBodyAccessTester creates a new BodyAccess interface tester
func NewBodyAccessTester(engine httpx.Engine) *BodyAccessTester {
	return &BodyAccessTester{engine: engine}
}

// TestBodyRaw tests the BodyRaw() method
func (bat *BodyAccessTester) TestBodyRaw(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		requestBody string
		contentType string
	}{
		{"JSON body", `{"name":"test","age":25}`, "application/json"},
		{"Text body", "Hello, World!", "text/plain"},
		{"Empty body", "", "text/plain"},
		{"Large body", strings.Repeat("A", 1024), "text/plain"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bat.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				bodyBytes, err := ctx.BodyRaw()
				AssertNoError(t, err, "BodyRaw should not return error")
				AssertEqual(t, tc.requestBody, string(bodyBytes), "Body content should match")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBodyReader tests the BodyReader() method
func (bat *BodyAccessTester) TestBodyReader(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		requestBody string
	}{
		{"JSON body", `{"name":"test","age":25}`},
		{"Text body", "Hello, World!"},
		{"Empty body", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bat.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				bodyReader := ctx.BodyReader()
				AssertNotEqual(t, nil, bodyReader, "BodyReader should not be nil")

				// Read the body
				bodyBytes, err := io.ReadAll(bodyReader)
				AssertNoError(t, err, "Reading from BodyReader should not error")
				AssertEqual(t, tc.requestBody, string(bodyBytes), "Body content should match")

				// Close the reader
				if closer, ok := bodyReader.(io.Closer); ok {
					_ = closer.Close()
				}

				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBodyReusability tests that body can be read multiple times when possible
func (bat *BodyAccessTester) TestBodyReusability(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		requestBody string
	}{
		{"Reusable body", "test content"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bat.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// First read
				bodyBytes1, err1 := ctx.BodyRaw()
				AssertNoError(t, err1, "First BodyRaw should not return error")

				// Second read - may or may not work depending on implementation
				bodyBytes2, err2 := ctx.BodyRaw()
				if err2 == nil {
					AssertEqual(t, string(bodyBytes1), string(bodyBytes2), "Body should be reusable")
					t.Logf("Body is reusable")
				} else {
					t.Logf("Body is not reusable (expected behavior): %v", err2)
				}

				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all BodyAccess interface tests
func (bat *BodyAccessTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("BodyRaw", bat.TestBodyRaw)
	t.Run("BodyReader", bat.TestBodyReader)
	t.Run("BodyReusability", bat.TestBodyReusability)
}
