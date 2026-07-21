package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRepositoryContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "platform-invariants.md"))
	if err != nil {
		t.Fatal(err)
	}

	document, err := parseContractMarkdown(string(data), contractDocument{})
	if err != nil {
		t.Fatal(err)
	}
	if document.ID != "shared-platform" || document.Kind != "platform-invariants" {
		t.Fatalf("unexpected contract identity: %#v", document)
	}
	if len(document.Rules) != 5 {
		t.Fatalf("got %d rules, want 5", len(document.Rules))
	}

	ingress := ruleByID(t, document.Rules, "PLAT-ING-001")
	if ingress.Severity != "critical" {
		t.Fatalf("ingress severity = %q, want critical", ingress.Severity)
	}
	cpu := ruleByID(t, document.Rules, "PLAT-CPU-001")
	if cpu.Severity != "medium" {
		t.Fatalf("CPU severity = %q, want medium", cpu.Severity)
	}
}

func TestParseRejectsRuleWithoutRemediation(t *testing.T) {
	markdown := `## Contract metadata
- Schema version: 1.0
- Kind: platform-invariants
- Contract ID: test
- Name: Test
- Owner: test-team
- Repository: test-repo

## High severity
### TEST-001 - Test rule
#### Requirements
- Preserve this requirement.
`
	_, err := parseContractMarkdown(markdown, contractDocument{})
	if err == nil || !strings.Contains(err.Error(), "has no remediation") {
		t.Fatalf("expected missing-remediation error, got %v", err)
	}
}

func ruleByID(t *testing.T, rules []publishedRule, id string) publishedRule {
	t.Helper()
	for _, rule := range rules {
		if rule.ID == id {
			return rule
		}
	}
	t.Fatalf("rule %s not found", id)
	return publishedRule{}
}
