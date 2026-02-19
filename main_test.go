package main

import (
	"testing"
)

func TestDetectTransitions_NoAlertBefore3Failures(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api"}, Up: false, Error: "http_503"},
	}

	for i := range 2 {
		transitions := detectTransitions(results, states)
		if len(transitions) != 0 {
			t.Errorf("cycle %d: expected 0 transitions, got %d", i+1, len(transitions))
		}
	}
}

func TestDetectTransitions_AlertAfter3Failures(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api"}, Up: false, Error: "http_503"},
	}

	var transitions []Transition
	for range 3 {
		transitions = detectTransitions(results, states)
	}

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	if transitions[0].Type != "down" {
		t.Errorf("expected transition type 'down', got '%s'", transitions[0].Type)
	}

	if transitions[0].ServiceName != "api" {
		t.Errorf("expected service name 'api', got '%s'", transitions[0].ServiceName)
	}

	if transitions[0].Error != "http_503" {
		t.Errorf("expected error 'http_503', got '%s'", transitions[0].Error)
	}
}

func TestDetectTransitions_NoDoubleAlert(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api"}, Up: false, Error: "http_503"},
	}

	for range 3 {
		detectTransitions(results, states)
	}

	transitions := detectTransitions(results, states)
	if len(transitions) != 0 {
		t.Errorf("expected 0 transitions after already alerting, got %d", len(transitions))
	}
}

func TestDetectTransitions_RecoveryAlert(t *testing.T) {
	states := make(map[string]*ServiceState)

	downResults := []CheckResult{
		{Service: Service{Name: "api"}, Up: false, Error: "http_503"},
	}

	for range 3 {
		detectTransitions(downResults, states)
	}

	upResults := []CheckResult{
		{Service: Service{Name: "api"}, Up: true},
	}

	transitions := detectTransitions(upResults, states)

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	if transitions[0].Type != "up" {
		t.Errorf("expected transition type 'up', got '%s'", transitions[0].Type)
	}
}

func TestDetectTransitions_ResetCounterOnSuccess(t *testing.T) {
	states := make(map[string]*ServiceState)

	downResults := []CheckResult{
		{Service: Service{Name: "api"}, Up: false, Error: "http_503"},
	}
	upResults := []CheckResult{
		{Service: Service{Name: "api"}, Up: true},
	}

	detectTransitions(downResults, states)
	detectTransitions(downResults, states)

	detectTransitions(upResults, states)

	detectTransitions(downResults, states)
	transitions := detectTransitions(downResults, states)

	if len(transitions) != 0 {
		t.Errorf("expected 0 transitions (counter was reset), got %d", len(transitions))
	}
}

func TestDetectTransitions_MultipleServices(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api-1"}, Up: false, Error: "http_503"},
		{Service: Service{Name: "api-2"}, Up: true},
	}

	var transitions []Transition
	for range 3 {
		transitions = detectTransitions(results, states)
	}

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	if transitions[0].ServiceName != "api-1" {
		t.Errorf("expected service 'api-1', got '%s'", transitions[0].ServiceName)
	}
}
