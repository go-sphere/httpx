package testing

import (
	"testing"
)

// TestAbortTrackerInitialization verifies that a new AbortTracker is properly initialized.
func TestAbortTrackerInitialization(t *testing.T) {
	tracker := NewAbortTracker()
	
	if tracker == nil {
		t.Fatal("NewAbortTracker returned nil")
	}
	
	if tracker.Steps == nil {
		t.Error("Steps should be initialized, not nil")
	}
	
	if tracker.AbortedStates == nil {
		t.Error("AbortedStates should be initialized, not nil")
	}
	
	if len(tracker.Steps) != 0 {
		t.Errorf("Expected empty Steps, got length %d", len(tracker.Steps))
	}
	
	if len(tracker.AbortedStates) != 0 {
		t.Errorf("Expected empty AbortedStates, got length %d", len(tracker.AbortedStates))
	}
}

// TestAbortTrackerReset verifies that Reset clears all tracking data.
func TestAbortTrackerReset(t *testing.T) {
	tracker := NewAbortTracker()
	
	// Manually add some data
	tracker.Steps = append(tracker.Steps, "step1", "step2")
	tracker.AbortedStates = append(tracker.AbortedStates, false, true)
	
	// Verify data was added
	if len(tracker.Steps) != 2 {
		t.Fatalf("Expected 2 steps before reset, got %d", len(tracker.Steps))
	}
	
	// Reset
	tracker.Reset()
	
	// Verify data was cleared
	if len(tracker.Steps) != 0 {
		t.Errorf("Expected empty Steps after reset, got length %d", len(tracker.Steps))
	}
	
	if len(tracker.AbortedStates) != 0 {
		t.Errorf("Expected empty AbortedStates after reset, got length %d", len(tracker.AbortedStates))
	}
}

// TestAbortTrackerReuse verifies that a tracker can be reused after reset.
func TestAbortTrackerReuse(t *testing.T) {
	tracker := NewAbortTracker()
	
	// First use
	tracker.Steps = append(tracker.Steps, "step1")
	tracker.AbortedStates = append(tracker.AbortedStates, false)
	
	// Reset
	tracker.Reset()
	
	// Second use
	tracker.Steps = append(tracker.Steps, "step2")
	tracker.AbortedStates = append(tracker.AbortedStates, true)
	
	// Verify second use works correctly
	if len(tracker.Steps) != 1 {
		t.Errorf("Expected 1 step after reuse, got %d", len(tracker.Steps))
	}
	
	if tracker.Steps[0] != "step2" {
		t.Errorf("Expected step2, got %s", tracker.Steps[0])
	}
	
	if len(tracker.AbortedStates) != 1 {
		t.Errorf("Expected 1 aborted state after reuse, got %d", len(tracker.AbortedStates))
	}
	
	if tracker.AbortedStates[0] != true {
		t.Errorf("Expected true, got %v", tracker.AbortedStates[0])
	}
}
