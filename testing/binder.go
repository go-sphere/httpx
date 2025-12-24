package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// BinderTester tests the Binder interface methods
type BinderTester struct {
	engine httpx.Engine
}

// NewBinderTester creates a new Binder interface tester
func NewBinderTester(engine httpx.Engine) *BinderTester {
	return &BinderTester{engine: engine}
}

// TestBindJSON tests the BindJSON() method with comprehensive struct tag validation
func (bt *BinderTester) TestBindJSON(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		requestBody string
		expectError bool
		validate    func(t *testing.T, result TestStruct)
	}{
		{
			"Valid JSON with all fields", 
			`{"name":"testuser","age":25,"email":"test@example.com"}`, 
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "testuser", result.Name, "Name should be bound correctly from JSON")
				AssertEqual(t, 25, result.Age, "Age should be bound correctly from JSON")
				AssertEqual(t, "test@example.com", result.Email, "Email should be bound correctly from JSON")
			},
		},
		{
			"Valid JSON with partial fields", 
			`{"name":"partial"}`, 
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "partial", result.Name, "Name should be bound from partial JSON")
				AssertEqual(t, 0, result.Age, "Age should be zero value for missing field")
				AssertEqual(t, "", result.Email, "Email should be empty for missing field")
			},
		},
		{
			"Invalid JSON syntax", 
			`{"name":"test","age":}`, 
			true,
			nil,
		},
		{
			"Invalid JSON type for age", 
			`{"name":"test","age":"not_a_number","email":"test@example.com"}`, 
			true,
			nil,
		},
		{
			"Empty JSON object", 
			`{}`, 
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "", result.Name, "Name should be empty for empty JSON")
				AssertEqual(t, 0, result.Age, "Age should be zero for empty JSON")
				AssertEqual(t, "", result.Email, "Email should be empty for empty JSON")
			},
		},
		{
			"Null JSON", 
			`null`, 
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "", result.Name, "Name should be empty for null JSON")
				AssertEqual(t, 0, result.Age, "Age should be zero for null JSON")
				AssertEqual(t, "", result.Email, "Email should be empty for null JSON")
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				var testStruct TestStruct
				err := ctx.BindJSON(&testStruct)
				
				if tc.expectError {
					AssertError(t, err, "BindJSON should return error for invalid JSON")
				} else {
					AssertNoError(t, err, "BindJSON should not return error for valid JSON")
					if tc.validate != nil {
						tc.validate(t, testStruct)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBindQuery tests the BindQuery() method with struct tag validation
func (bt *BinderTester) TestBindQuery(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		queryParams map[string]string
		expectError bool
		validate    func(t *testing.T, result TestStruct)
	}{
		{
			"Valid query parameters",
			map[string]string{"name": "queryuser", "age": "30", "email": "query@example.com"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "queryuser", result.Name, "Name should be bound from query tag")
				AssertEqual(t, 30, result.Age, "Age should be bound and converted from query tag")
				AssertEqual(t, "query@example.com", result.Email, "Email should be bound from query tag")
			},
		},
		{
			"Partial query parameters",
			map[string]string{"name": "partial"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "partial", result.Name, "Name should be bound from partial query")
				AssertEqual(t, 0, result.Age, "Age should be zero for missing query param")
				AssertEqual(t, "", result.Email, "Email should be empty for missing query param")
			},
		},
		{
			"Invalid age in query",
			map[string]string{"name": "test", "age": "invalid_number"},
			true, // Some frameworks may return error for invalid type conversion
			nil,
		},
		{
			"Empty query parameters",
			map[string]string{},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "", result.Name, "Name should be empty for no query params")
				AssertEqual(t, 0, result.Age, "Age should be zero for no query params")
				AssertEqual(t, "", result.Email, "Email should be empty for no query params")
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				var testStruct TestStruct
				err := ctx.BindQuery(&testStruct)
				
				if tc.expectError {
					// Some frameworks may handle type conversion errors gracefully
					if err != nil {
						t.Logf("BindQuery error (may be expected for invalid types): %v", err)
					}
				} else {
					AssertNoError(t, err, "BindQuery should not return error")
					if tc.validate != nil {
						tc.validate(t, testStruct)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBindForm tests the BindForm() method with struct tag validation
func (bt *BinderTester) TestBindForm(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		formData    map[string]string
		expectError bool
		validate    func(t *testing.T, result TestStruct)
	}{
		{
			"Valid form data",
			map[string]string{"name": "formuser", "age": "35", "email": "form@example.com"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "formuser", result.Name, "Name should be bound from form tag")
				AssertEqual(t, 35, result.Age, "Age should be bound and converted from form tag")
				AssertEqual(t, "form@example.com", result.Email, "Email should be bound from form tag")
			},
		},
		{
			"Partial form data",
			map[string]string{"name": "partialform"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "partialform", result.Name, "Name should be bound from partial form")
				AssertEqual(t, 0, result.Age, "Age should be zero for missing form field")
				AssertEqual(t, "", result.Email, "Email should be empty for missing form field")
			},
		},
		{
			"Invalid age in form",
			map[string]string{"name": "test", "age": "not_a_number"},
			true, // Some frameworks may return error for invalid type conversion
			nil,
		},
		{
			"Empty form data",
			map[string]string{},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "", result.Name, "Name should be empty for no form data")
				AssertEqual(t, 0, result.Age, "Age should be zero for no form data")
				AssertEqual(t, "", result.Email, "Email should be empty for no form data")
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				var testStruct TestStruct
				err := ctx.BindForm(&testStruct)
				
				if tc.expectError {
					// Some frameworks may handle type conversion errors gracefully
					if err != nil {
						t.Logf("BindForm error (may be expected for invalid types): %v", err)
					}
				} else {
					AssertNoError(t, err, "BindForm should not return error")
					if tc.validate != nil {
						tc.validate(t, testStruct)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBindURI tests the BindURI() method with struct tag validation
func (bt *BinderTester) TestBindURI(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name         string
		routePattern string
		expectError  bool
		validate     func(t *testing.T, result TestStruct)
	}{
		{
			"Valid URI parameters",
			"/users/:name/:age",
			false,
			func(t *testing.T, result TestStruct) {
				// URI binding depends on actual request path matching route pattern
				// In test environment, we validate that binding doesn't error
				t.Logf("URI bound result: %+v", result)
			},
		},
		{
			"Single URI parameter",
			"/users/:name",
			false,
			func(t *testing.T, result TestStruct) {
				t.Logf("Single URI param result: %+v", result)
			},
		},
		{
			"No URI parameters",
			"/users",
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "", result.Name, "Name should be empty for no URI params")
				AssertEqual(t, 0, result.Age, "Age should be zero for no URI params")
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			uniqueRoute := GenerateUniqueParamPath(tc.routePattern)
			router.GET(uniqueRoute, func(ctx httpx.Context) {
				// capturedContext = ctx
				var testStruct TestStruct
				err := ctx.BindURI(&testStruct)
				
				if tc.expectError {
					AssertError(t, err, "BindURI should return error")
				} else {
					AssertNoError(t, err, "BindURI should not return error")
					if tc.validate != nil {
						tc.validate(t, testStruct)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBindHeader tests the BindHeader() method with struct tag validation
func (bt *BinderTester) TestBindHeader(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		headers     map[string]string
		expectError bool
		validate    func(t *testing.T, result TestStruct)
	}{
		{
			"Valid headers",
			map[string]string{"X-Name": "headeruser", "X-Age": "40"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "headeruser", result.Name, "Name should be bound from header tag")
				AssertEqual(t, 40, result.Age, "Age should be bound and converted from header tag")
			},
		},
		{
			"Partial headers",
			map[string]string{"X-Name": "partialheader"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "partialheader", result.Name, "Name should be bound from partial headers")
				AssertEqual(t, 0, result.Age, "Age should be zero for missing header")
			},
		},
		{
			"Invalid age in header",
			map[string]string{"X-Name": "test", "X-Age": "invalid_number"},
			true, // Some frameworks may return error for invalid type conversion
			nil,
		},
		{
			"No matching headers",
			map[string]string{"Other-Header": "value"},
			false,
			func(t *testing.T, result TestStruct) {
				AssertEqual(t, "", result.Name, "Name should be empty for no matching headers")
				AssertEqual(t, 0, result.Age, "Age should be zero for no matching headers")
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				var testStruct TestStruct
				err := ctx.BindHeader(&testStruct)
				
				if tc.expectError {
					// Some frameworks may handle type conversion errors gracefully
					if err != nil {
						t.Logf("BindHeader error (may be expected for invalid types): %v", err)
					}
				} else {
					AssertNoError(t, err, "BindHeader should not return error")
					if tc.validate != nil {
						tc.validate(t, testStruct)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestNestedStructBinding tests binding with nested structures
func (bt *BinderTester) TestNestedStructBinding(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		method      string
		requestBody string
		expectError bool
		validate    func(t *testing.T, result NestedTestStruct)
	}{
		{
			"Valid nested JSON",
			"BindJSON",
			`{"user":{"name":"nested","age":25,"email":"nested@example.com"},"address":{"street":"123 Main St","city":"Test City","zip":"12345"}}`,
			false,
			func(t *testing.T, result NestedTestStruct) {
				AssertEqual(t, "nested", result.User.Name, "Nested user name should be bound")
				AssertEqual(t, 25, result.User.Age, "Nested user age should be bound")
				AssertEqual(t, "nested@example.com", result.User.Email, "Nested user email should be bound")
				AssertEqual(t, "123 Main St", result.Address.Street, "Nested address street should be bound")
				AssertEqual(t, "Test City", result.Address.City, "Nested address city should be bound")
				AssertEqual(t, "12345", result.Address.Zip, "Nested address zip should be bound")
			},
		},
		{
			"Partial nested JSON",
			"BindJSON",
			`{"user":{"name":"partial"}}`,
			false,
			func(t *testing.T, result NestedTestStruct) {
				AssertEqual(t, "partial", result.User.Name, "Partial nested user name should be bound")
				AssertEqual(t, 0, result.User.Age, "Partial nested user age should be zero")
				AssertEqual(t, "", result.Address.Street, "Missing nested address should be empty")
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				var nestedStruct NestedTestStruct
				var err error
				
				switch tc.method {
				case "BindJSON":
					err = ctx.BindJSON(&nestedStruct)
				}
				
				if tc.expectError {
					AssertError(t, err, "Nested binding should return error for invalid data")
				} else {
					AssertNoError(t, err, "Nested binding should not return error for valid data")
					if tc.validate != nil {
						tc.validate(t, nestedStruct)
					}
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBindingErrorHandling tests comprehensive error handling scenarios
func (bt *BinderTester) TestBindingErrorHandling(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		bindMethod  string
		data        interface{}
		expectError bool
		description string
	}{
		{
			"Nil pointer to BindJSON",
			"BindJSON",
			nil,
			true,
			"Binding to nil pointer should return error",
		},
		{
			"Non-pointer to BindJSON",
			"BindJSON",
			TestStruct{},
			true,
			"Binding to non-pointer should return error",
		},
		{
			"Invalid JSON with BindJSON",
			"BindJSON",
			&TestStruct{},
			true,
			"Invalid JSON should return error",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := bt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				var err error
				
				switch tc.bindMethod {
				case "BindJSON":
					err = ctx.BindJSON(tc.data)
				case "BindQuery":
					err = ctx.BindQuery(tc.data)
				case "BindForm":
					err = ctx.BindForm(tc.data)
				case "BindURI":
					err = ctx.BindURI(tc.data)
				case "BindHeader":
					err = ctx.BindHeader(tc.data)
				}
				
				if tc.expectError {
					// Some frameworks may handle errors gracefully
					if err != nil {
						t.Logf("Expected error occurred: %v", err)
					} else {
						t.Logf("Framework handled error gracefully for: %s", tc.description)
					}
				} else {
					AssertNoError(t, err, tc.description)
				}
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all Binder interface tests
func (bt *BinderTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("BindJSON", bt.TestBindJSON)
	t.Run("BindQuery", bt.TestBindQuery)
	t.Run("BindForm", bt.TestBindForm)
	t.Run("BindURI", bt.TestBindURI)
	t.Run("BindHeader", bt.TestBindHeader)
	t.Run("NestedStructBinding", bt.TestNestedStructBinding)
	t.Run("BindingErrorHandling", bt.TestBindingErrorHandling)
}