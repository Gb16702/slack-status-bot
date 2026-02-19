package main

import (
	"testing"
)

func TestDetectTransitions_NoAlertBefore4Failures(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api", Env: "production"}, Up: false, Error: "http_503"},
	}

	for i := range 3 {
		transitions := detectTransitions(results, states)
		if len(transitions) != 0 {
			t.Errorf("cycle %d: expected 0 transitions, got %d", i+1, len(transitions))
		}
	}
}

func TestDetectTransitions_AlertAfter4Failures(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api", Env: "production"}, Up: false, Error: "http_503"},
	}

	var transitions []Transition
	for range failThreshold {
		transitions = detectTransitions(results, states)
	}

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	if transitions[0].Type != "down" {
		t.Errorf("expected transition type 'down', got '%s'", transitions[0].Type)
	}

	if transitions[0].ServiceName != "api (production)" {
		t.Errorf("expected service name 'api (production)', got '%s'", transitions[0].ServiceName)
	}

	if transitions[0].Error != "http_503" {
		t.Errorf("expected error 'http_503', got '%s'", transitions[0].Error)
	}
}

func TestDetectTransitions_NoDoubleAlert(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api", Env: "production"}, Up: false, Error: "http_503"},
	}

	for range failThreshold {
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
		{Service: Service{Name: "api", Env: "production"}, Up: false, Error: "http_503"},
	}

	for range failThreshold {
		detectTransitions(downResults, states)
	}

	upResults := []CheckResult{
		{Service: Service{Name: "api", Env: "production"}, Up: true},
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
		{Service: Service{Name: "api", Env: "production"}, Up: false, Error: "http_503"},
	}
	upResults := []CheckResult{
		{Service: Service{Name: "api", Env: "production"}, Up: true},
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
		{Service: Service{Name: "api-1", Env: "production"}, Up: false, Error: "http_503"},
		{Service: Service{Name: "api-2", Env: "production"}, Up: true},
	}

	var transitions []Transition
	for range failThreshold {
		transitions = detectTransitions(results, states)
	}

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	if transitions[0].ServiceName != "api-1 (production)" {
		t.Errorf("expected service 'api-1 (production)', got '%s'", transitions[0].ServiceName)
	}
}

func TestDetectTransitions_SameNameDifferentEnv(t *testing.T) {
	states := make(map[string]*ServiceState)

	results := []CheckResult{
		{Service: Service{Name: "api", Env: "production"}, Up: false, Error: "http_503"},
		{Service: Service{Name: "api", Env: "development"}, Up: true},
	}

	var transitions []Transition
	for range failThreshold {
		transitions = detectTransitions(results, states)
	}

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}

	if transitions[0].ServiceName != "api (production)" {
		t.Errorf("expected service 'api (production)', got '%s'", transitions[0].ServiceName)
	}
}
