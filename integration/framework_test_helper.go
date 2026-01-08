package integration

import (
	"testing"

	"github.com/go-sphere/httpx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// AllInterfaceNames returns the list of all httpx interface names for testing.
// This centralizes the interface list to avoid duplication across test files.
var AllInterfaceNames = []string{
	"RequestInfo",
	"Request",
	"BodyAccess",
	"FormAccess",
	"Binder",
	"Responder",
	"StateStore",
	"Router",
	"Engine",
}

// RunFrameworkIntegrationTests runs the standard integration test suite for a framework.
// This eliminates duplicated test structure across ginx, fiberx, echox, and hertzx test files.
func RunFrameworkIntegrationTests(t *testing.T, frameworkName string, engine httpx.Engine, skipManager *TestSkipManager) {
	t.Helper()

	tc := NewTestCases(frameworkName, engine)

	// Validate framework integration first
	t.Run("ValidateIntegration", func(t *testing.T) {
		tc.ValidateFrameworkIntegration(t)
	})

	// Run all interface tests
	t.Run("AllInterfaceTests", func(t *testing.T) {
		tc.RunAllInterfaceTests(t)
	})

	// Run individual interface tests with skip support
	if skipManager != nil {
		t.Run("IndividualInterfaceTestsWithSkipSupport", func(t *testing.T) {
			tc.RunIndividualInterfaceTestsWithSkipSupport(t, skipManager)
		})
	} else {
		t.Run("IndividualInterfaceTests", func(t *testing.T) {
			tc.RunIndividualInterfaceTests(t)
		})
	}
}

// RunFrameworkSpecificInterfaceTests runs tests for specific interfaces individually.
// This eliminates duplicated test loops across framework test files.
func RunFrameworkSpecificInterfaceTests(t *testing.T, frameworkName string, engine httpx.Engine, skipManager *TestSkipManager) {
	t.Helper()

	tc := NewTestCases(frameworkName, engine)

	for _, interfaceName := range AllInterfaceNames {
		interfaceName := interfaceName // Capture for closure
		t.Run(interfaceName, func(t *testing.T) {
			if skipManager != nil {
				tc.RunWithSkipSupport(t, skipManager, interfaceName, func(t *testing.T) {
					tc.RunSpecificInterfaceTest(t, interfaceName)
				})
			} else {
				tc.RunSpecificInterfaceTest(t, interfaceName)
			}
		})
	}
}

// RunFrameworkWithCustomConfig runs tests with custom configuration.
// This eliminates duplicated custom config test patterns.
func RunFrameworkWithCustomConfig(t *testing.T, frameworkName string, engine httpx.Engine, config *httptesting.TestConfig, skipManager *TestSkipManager) {
	t.Helper()

	tc := NewTestCasesWithConfig(frameworkName, engine, config)

	t.Run("CustomConfigTests", func(t *testing.T) {
		if skipManager != nil {
			tc.RunIndividualInterfaceTestsWithSkipSupport(t, skipManager)
		} else {
			tc.RunAllInterfaceTests(t)
		}
	})
}
