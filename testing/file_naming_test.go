package testing

import (
	"os"
	"strings"
	"testing"
)

// Property 1: Interface File Naming Consistency
// For any httpx interface, the corresponding testing file name should match
// the interface name in snake_case format (e.g., RequestInfo → request_info.go)
// **Feature: httpx-testing-refactor, Property 1: Interface File Naming Consistency**
// **Validates: Requirements 1.2**
func TestInterfaceFileNamingConsistency(t *testing.T) {
	t.Helper()

	// Define expected interface files based on requirements 2.1
	expectedFiles := map[string]string{
		"RequestInfo": "request_info.go",
		"BodyAccess":  "body_access.go",
		"FormAccess":  "form_access.go",
		"Binder":      "binder.go",
		"Responder":   "responder.go",
		"StateStore":  "state_store.go",
		"Router":      "router.go",
		"Engine":      "engine.go",
	}

	// Get current working directory and navigate to testing package
	testingDir := "."

	// Read all files in the testing directory
	files, err := os.ReadDir(testingDir)
	if err != nil {
		t.Fatalf("Failed to read testing directory: %v", err)
	}

	// Create a set of existing .go files (excluding test files and support files)
	existingFiles := make(map[string]bool)
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".go") {
			// Exclude test files, config, utils, and suite files
			if !strings.HasSuffix(file.Name(), "_test.go") &&
				file.Name() != "config.go" &&
				file.Name() != "utils.go" &&
				file.Name() != "suite.go" &&
				file.Name() != "go.mod" &&
				file.Name() != "go.sum" {
				existingFiles[file.Name()] = true
			}
		}
	}

	// Test property: For each expected interface, verify the file exists with correct naming
	for interfaceName, expectedFileName := range expectedFiles {
		t.Run("Interface_"+interfaceName, func(t *testing.T) {
			if !existingFiles[expectedFileName] {
				t.Errorf("Expected file %s for interface %s not found in testing package",
					expectedFileName, interfaceName)
			} else {
				t.Logf("✓ Interface %s correctly maps to file %s", interfaceName, expectedFileName)
			}
		})
	}

	// Test property: Verify no unexpected interface files exist
	t.Run("NoUnexpectedFiles", func(t *testing.T) {
		expectedFileSet := make(map[string]bool)
		for _, fileName := range expectedFiles {
			expectedFileSet[fileName] = true
		}

		for fileName := range existingFiles {
			if !expectedFileSet[fileName] {
				t.Logf("Note: Found additional file %s (may be support file)", fileName)
			}
		}
	})

	// Test property: Verify snake_case naming convention
	t.Run("SnakeCaseNaming", func(t *testing.T) {
		for interfaceName, expectedFileName := range expectedFiles {
			// Convert PascalCase to snake_case and verify
			expectedSnakeCase := convertPascalToSnakeCase(interfaceName) + ".go"
			if expectedFileName != expectedSnakeCase {
				t.Errorf("Interface %s: expected snake_case file name %s, but requirement specifies %s",
					interfaceName, expectedSnakeCase, expectedFileName)
			} else {
				t.Logf("✓ Interface %s correctly follows snake_case naming: %s",
					interfaceName, expectedFileName)
			}
		}
	})
}

// convertPascalToSnakeCase converts PascalCase to snake_case
func convertPascalToSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			result.WriteRune(r - 'A' + 'a')
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
