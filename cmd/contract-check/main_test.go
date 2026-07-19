package main

import "testing"

func testContracts() []loadedContract {
	return []loadedContract{{contract: contract{ID: "search", Owner: "search-team", Rules: []rule{{ID: "SEARCH-NET-001", Severity: "critical", Description: "NAT stays static", Signals: []string{"allocation_method:\\s*ephemeral"}, Remediation: "Restore static allocation."}}}}}
}

func TestDeterministicViolation(t *testing.T) {
	got, err := analyzeDeterministically("+    allocation_method: ephemeral", testContracts(), "infra-to-service")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "fail" || len(got.Violations) != 1 || got.Violations[0].RuleID != "SEARCH-NET-001" {
		t.Fatalf("unexpected verdict: %#v", got)
	}
}

func TestDeterministicPass(t *testing.T) {
	got, err := analyzeDeterministically("+  replication: regional", testContracts(), "infra-to-service")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pass" || len(got.Violations) != 0 {
		t.Fatalf("unexpected verdict: %#v", got)
	}
}

func TestValidationRejectsInconsistentPass(t *testing.T) {
	err := validateVerdict(verdict{Status: "pass", Risk: "none", Summary: "x", AffectedServices: []string{}, Violations: []violation{{}}})
	if err == nil {
		t.Fatal("expected validation failure")
	}
}
