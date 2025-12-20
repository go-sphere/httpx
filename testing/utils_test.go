package testing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestEqualSlices(t *testing.T) {
	// Test equal slices
	a := []int{1, 2, 3}
	b := []int{1, 2, 3}
	if !EqualSlices(a, b) {
		t.Error("Expected equal slices to return true")
	}

	// Test different length slices
	c := []int{1, 2}
	if EqualSlices(a, c) {
		t.Error("Expected slices of different lengths to return false")
	}

	// Test different content slices
	d := []int{1, 2, 4}
	if EqualSlices(a, d) {
		t.Error("Expected slices with different content to return false")
	}

	// Test empty slices
	e := []int{}
	f := []int{}
	if !EqualSlices(e, f) {
		t.Error("Expected empty slices to be equal")
	}

	// Test string slices
	g := []string{"hello", "world"}
	h := []string{"hello", "world"}
	if !EqualSlices(g, h) {
		t.Error("Expected equal string slices to return true")
	}
}

func TestMakeRequest(t *testing.T) {
	// Test basic request creation
	req, err := MakeRequest("GET", "http://example.com", nil, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	
	if req.Method != "GET" {
		t.Errorf("Expected method GET, got %s", req.Method)
	}
	
	if req.URL.String() != "http://example.com" {
		t.Errorf("Expected URL http://example.com, got %s", req.URL.String())
	}

	// Test request with headers
	headers := map[string]string{
		"Content-Type": "application/json",
		"Authorization": "Bearer token",
	}
	
	req, err = MakeRequest("POST", "http://example.com", strings.NewReader("test body"), headers)
	if err != nil {
		t.Fatalf("Failed to create request with headers: %v", err)
	}
	
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type header to be application/json, got %s", req.Header.Get("Content-Type"))
	}
	
	if req.Header.Get("Authorization") != "Bearer token" {
		t.Errorf("Expected Authorization header to be Bearer token, got %s", req.Header.Get("Authorization"))
	}
}

func TestAssertResponse(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	// Make a request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Test assertion (this should pass)
	AssertResponse(t, resp, http.StatusOK, "test response")
}

func TestAssertHeader(t *testing.T) {
	// Create a test server with custom header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "test-value")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Make a request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Test header assertion
	AssertHeader(t, resp, "X-Test-Header", "test-value")
}

func TestAssertCookie(t *testing.T) {
	// Create a test server with cookie
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:  "test-cookie",
			Value: "test-value",
		}
		http.SetCookie(w, cookie)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Make a request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Test cookie assertion
	AssertCookie(t, resp, "test-cookie", "test-value")
}

// Property-based test for EqualSlices function
// Feature: httpx-testing-framework, Property 7: EqualSlices 比较正确性
// Validates: Requirements 10.1, 10.2, 10.3, 10.4
func TestEqualSlicesProperty(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// Property 1: EqualSlices should return true for identical slices
	properties.Property("identical slices should be equal", prop.ForAll(
		func(slice []int) bool {
			// Create a copy of the slice
			copy := make([]int, len(slice))
			for i, v := range slice {
				copy[i] = v
			}
			return EqualSlices(slice, copy)
		},
		gen.SliceOf(gen.Int()),
	))

	// Property 2: EqualSlices should return false for slices of different lengths
	properties.Property("slices of different lengths should not be equal", prop.ForAll(
		func(slice1, slice2 []int) bool {
			if len(slice1) == len(slice2) {
				return true // Skip this case as lengths are equal
			}
			return !EqualSlices(slice1, slice2)
		},
		gen.SliceOf(gen.Int()),
		gen.SliceOf(gen.Int()),
	))

	// Property 3: EqualSlices should return false when elements differ
	properties.Property("slices with different elements should not be equal", prop.ForAll(
		func(slice []int, index int, newValue int) bool {
			if len(slice) == 0 {
				return true // Skip empty slices
			}
			
			// Normalize index to valid range
			index = index % len(slice)
			if index < 0 {
				index = -index
			}
			
			// Create a copy and modify one element
			modified := make([]int, len(slice))
			copy(modified, slice)
			
			// Only modify if the new value is different
			if modified[index] == newValue {
				return true // Skip if values are the same
			}
			
			modified[index] = newValue
			return !EqualSlices(slice, modified)
		},
		gen.SliceOf(gen.Int()),
		gen.Int(),
		gen.Int(),
	))

	// Property 4: EqualSlices should be reflexive (a slice equals itself)
	properties.Property("slice should equal itself (reflexive)", prop.ForAll(
		func(slice []int) bool {
			return EqualSlices(slice, slice)
		},
		gen.SliceOf(gen.Int()),
	))

	// Property 5: EqualSlices should be symmetric (if a equals b, then b equals a)
	properties.Property("EqualSlices should be symmetric", prop.ForAll(
		func(slice1, slice2 []int) bool {
			result1 := EqualSlices(slice1, slice2)
			result2 := EqualSlices(slice2, slice1)
			return result1 == result2
		},
		gen.SliceOf(gen.Int()),
		gen.SliceOf(gen.Int()),
	))

	// Property 6: Empty slices should be equal
	properties.Property("empty slices should be equal", prop.ForAll(
		func() bool {
			empty1 := []int{}
			empty2 := []int{}
			return EqualSlices(empty1, empty2)
		},
	))

	// Run all properties with at least 100 iterations
	properties.TestingRun(t, gopter.ConsoleReporter(false))
}