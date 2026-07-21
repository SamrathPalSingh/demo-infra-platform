package main

import "testing"

func TestEnabledRoutesFiltersIllustrativeEntries(t *testing.T) {
	services := []serviceRoute{{ID: "search", Repository: "demo-service-search"}, {ID: "node-inspector", Repository: "demo-service-node-inspector"}}
	services[1].ContractTest.Enabled = true
	routes, err := enabledRoutes(services)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].ID != "node-inspector" {
		t.Fatalf("unexpected routes: %#v", routes)
	}
}

func TestAggregateFailsWhenAnyServiceFails(t *testing.T) {
	results := []serviceResult{
		{ServiceID: "search", Status: "pass", Violations: []violation{}},
		{ServiceID: "node-inspector", Status: "fail", Violations: []violation{{RuleID: "NODE-SEC-001"}}},
	}
	report := aggregate(results, "test", "owner/infra", 7, "abc")
	if report.Status != "fail" || report.Summary != "1 of 2 service repositories rejected this infrastructure change." {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestCorrelationIsArtifactSafe(t *testing.T) {
	got := buildCorrelation("Owner/Infra Repo", 42, "ABCDEF1234567890", "9001", "2")
	want := "owner-infra-repo-pr42-abcdef123456-run9001-2"
	if got != want {
		t.Fatalf("correlation = %q, want %q", got, want)
	}
}
