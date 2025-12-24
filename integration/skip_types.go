package integration

// skip_types.go - Type definitions for test skip management

// SkippableTest represents a test that can be conditionally skipped
type SkippableTest struct {
	Name      string
	Framework string
	Interface string
	Method    string
	Reason    string
	Skip      bool
}

// TestSkipManager manages test skipping for known failing tests
type TestSkipManager struct {
	skippedTests map[string][]SkippableTest
}

// AddSkippedTest adds a test to be skipped for a specific framework
func (tsm *TestSkipManager) AddSkippedTest(framework, interfaceName, method, reason string) {
	test := SkippableTest{
		Name:      framework + "_" + interfaceName + "_" + method,
		Framework: framework,
		Interface: interfaceName,
		Method:    method,
		Reason:    reason,
		Skip:      true,
	}

	tsm.skippedTests[framework] = append(tsm.skippedTests[framework], test)
}

// ShouldSkipTest checks if a test should be skipped for a framework
func (tsm *TestSkipManager) ShouldSkipTest(framework, interfaceName, method string) (bool, string) {
	tests, exists := tsm.skippedTests[framework]
	if !exists {
		return false, ""
	}

	for _, test := range tests {
		if test.Interface == interfaceName && test.Method == method && test.Skip {
			return true, test.Reason
		}
	}

	return false, ""
}

// GetSkippedTests returns all skipped tests for a framework
func (tsm *TestSkipManager) GetSkippedTests(framework string) []SkippableTest {
	return tsm.skippedTests[framework]
}
