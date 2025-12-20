package testing

import (
	"testing"
	"time"
)

func TestTestError(t *testing.T) {
	err := NewTestError("TestComponent", "TestOperation", "expected", "actual", "test message")
	
	expectedMsg := "TestComponent.TestOperation: expected expected, got actual - test message"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
	}
	
	if err.Component != "TestComponent" {
		t.Errorf("Expected component TestComponent, got %s", err.Component)
	}
	
	if err.Operation != "TestOperation" {
		t.Errorf("Expected operation TestOperation, got %s", err.Operation)
	}
}

func TestDefaultTestConfig(t *testing.T) {
	config := DefaultTestConfig
	
	if config.ServerAddr != ":0" {
		t.Errorf("Expected ServerAddr :0, got %s", config.ServerAddr)
	}
	
	if config.RequestTimeout != 5*time.Second {
		t.Errorf("Expected RequestTimeout 5s, got %v", config.RequestTimeout)
	}
	
	if config.ConcurrentUsers != 10 {
		t.Errorf("Expected ConcurrentUsers 10, got %d", config.ConcurrentUsers)
	}
	
	if config.TestDataSize != 1024 {
		t.Errorf("Expected TestDataSize 1024, got %d", config.TestDataSize)
	}
}

func TestTestStruct(t *testing.T) {
	// Test that TestStruct can be created and has the expected fields
	ts := TestStruct{
		Name:  "John Doe",
		Age:   30,
		Email: "john@example.com",
	}
	
	if ts.Name != "John Doe" {
		t.Errorf("Expected Name John Doe, got %s", ts.Name)
	}
	
	if ts.Age != 30 {
		t.Errorf("Expected Age 30, got %d", ts.Age)
	}
	
	if ts.Email != "john@example.com" {
		t.Errorf("Expected Email john@example.com, got %s", ts.Email)
	}
}

func TestNestedTestStruct(t *testing.T) {
	// Test that NestedTestStruct can be created and has the expected fields
	nts := NestedTestStruct{
		User: TestStruct{
			Name:  "Jane Doe",
			Age:   25,
			Email: "jane@example.com",
		},
		Active: true,
		Tags:   []string{"admin", "user"},
	}
	
	if nts.User.Name != "Jane Doe" {
		t.Errorf("Expected User.Name Jane Doe, got %s", nts.User.Name)
	}
	
	if !nts.Active {
		t.Error("Expected Active to be true")
	}
	
	expectedTags := []string{"admin", "user"}
	if !EqualSlices(nts.Tags, expectedTags) {
		t.Errorf("Expected Tags %v, got %v", expectedTags, nts.Tags)
	}
}