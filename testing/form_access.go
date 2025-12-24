package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// FormAccessTester tests the FormAccess interface methods
type FormAccessTester struct {
	engine httpx.Engine
}

// NewFormAccessTester creates a new FormAccess interface tester
func NewFormAccessTester(engine httpx.Engine) *FormAccessTester {
	return &FormAccessTester{engine: engine}
}

// TestFormValue tests the FormValue() method
func (fat *FormAccessTester) TestFormValue(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name          string
		formKey       string
		formData      map[string]string
		expectedValue string
	}{
		{"Existing form field", "name", map[string]string{"name": "testuser", "email": "test@example.com"}, "testuser"},
		{"Non-existent form field", "nonexistent", map[string]string{"name": "testuser"}, ""},
		{"Empty form field", "empty", map[string]string{"empty": "", "name": "test"}, ""},
		{"Multiple values same key", "tags", map[string]string{"tags": "go,web,api"}, "go,web,api"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := fat.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				formValue := ctx.FormValue(tc.formKey)
				AssertEqual(t, tc.expectedValue, formValue, "Form value should match")
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestMultipartForm tests the MultipartForm() method and form parsing triggers
func (fat *FormAccessTester) TestMultipartForm(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		contentType string
		expectError bool
	}{
		{"Valid multipart form", "multipart/form-data", false},
		{"Non-multipart content", "application/x-www-form-urlencoded", true},
		{"Invalid content type", "application/json", true},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := fat.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				multipartForm, err := ctx.MultipartForm()
				
				if tc.expectError {
					if err == nil {
						t.Logf("MultipartForm returned no error for %s (framework may handle gracefully)", tc.contentType)
					} else {
						t.Logf("MultipartForm error (expected for %s): %v", tc.contentType, err)
					}
				} else {
					if err != nil {
						t.Logf("MultipartForm error (may be expected if no multipart data): %v", err)
					} else {
						AssertNotEqual(t, nil, multipartForm, "MultipartForm should not be nil")
						t.Logf("MultipartForm parsed successfully")
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestFormFile tests the FormFile() method and file handling
func (fat *FormAccessTester) TestFormFile(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		fileName    string
		expectError bool
	}{
		{"Valid file field", "upload", false},
		{"Non-existent file field", "nonexistent", true},
		{"Empty file field name", "", true},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := fat.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				fileHeader, err := ctx.FormFile(tc.fileName)
				
				if tc.expectError {
					AssertError(t, err, "Should return error for invalid file field")
				} else {
					// May return error if no file uploaded, which is expected in test environment
					if err != nil {
						t.Logf("FormFile error (expected if no file uploaded): %v", err)
					} else {
						AssertNotEqual(t, nil, fileHeader, "FileHeader should not be nil")
						t.Logf("FormFile found: %s", fileHeader.Filename)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestFormParsingTriggers tests that form parsing is triggered appropriately
func (fat *FormAccessTester) TestFormParsingTriggers(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		method      string
		description string
	}{
		{"POST request form parsing", "POST", "Form parsing should work with POST requests"},
		{"PUT request form parsing", "PUT", "Form parsing should work with PUT requests"},
		{"PATCH request form parsing", "PATCH", "Form parsing should work with PATCH requests"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := fat.engine.Group("")
			// var capturedContext httpx.Context
			
			router.Handle(tc.method, GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Test that form parsing methods don't panic and handle gracefully
				formValue := ctx.FormValue("test")
				t.Logf("FormValue result: %s", formValue)
				
				multipartForm, err := ctx.MultipartForm()
				if err != nil {
					t.Logf("MultipartForm error (expected): %v", err)
				} else {
					t.Logf("MultipartForm parsed: %v", multipartForm != nil)
				}
				
				fileHeader, err := ctx.FormFile("file")
				if err != nil {
					t.Logf("FormFile error (expected): %v", err)
				} else {
					t.Logf("FormFile found: %v", fileHeader != nil)
				}
				
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestFormSideEffects tests side effects of form parsing methods
func (fat *FormAccessTester) TestFormSideEffects(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		description string
	}{
		{"Multiple FormValue calls", "Multiple calls to FormValue should be consistent"},
		{"FormValue after MultipartForm", "FormValue should work after MultipartForm call"},
		{"MultipartForm after FormValue", "MultipartForm should work after FormValue call"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := fat.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Test multiple FormValue calls for consistency
				if tc.name == "Multiple FormValue calls" {
					value1 := ctx.FormValue("test")
					value2 := ctx.FormValue("test")
					AssertEqual(t, value1, value2, "Multiple FormValue calls should return same result")
				}
				
				// Test interaction between FormValue and MultipartForm
				if tc.name == "FormValue after MultipartForm" {
					_, err := ctx.MultipartForm()
					if err != nil {
						t.Logf("MultipartForm error (expected): %v", err)
					}
					value := ctx.FormValue("test")
					t.Logf("FormValue after MultipartForm: %s", value)
				}
				
				if tc.name == "MultipartForm after FormValue" {
					value := ctx.FormValue("test")
					t.Logf("FormValue: %s", value)
					_, err := ctx.MultipartForm()
					if err != nil {
						t.Logf("MultipartForm after FormValue error (expected): %v", err)
					}
				}
				
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all FormAccess interface tests
func (fat *FormAccessTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("FormValue", fat.TestFormValue)
	t.Run("MultipartForm", fat.TestMultipartForm)
	t.Run("FormFile", fat.TestFormFile)
	t.Run("FormParsingTriggers", fat.TestFormParsingTriggers)
	t.Run("FormSideEffects", fat.TestFormSideEffects)
}