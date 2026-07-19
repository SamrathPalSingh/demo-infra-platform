import test from "node:test";
import assert from "node:assert/strict";
import { analyzeDeterministically, validateVerdict } from "../scripts/contract-check.mjs";

const contract = {
  id: "search",
  name: "Search API",
  owner: "search-team",
  rules: [{
    id: "SEARCH-NET-001",
    severity: "critical",
    description: "NAT must stay static.",
    signals: ["allocation_method:\\s*ephemeral"],
    remediation: "Restore static allocation."
  }]
};

test("deterministic demo engine blocks a matching added line", () => {
  const verdict = analyzeDeterministically("+    allocation_method: ephemeral", [contract], "infra-to-service");
  assert.equal(verdict.status, "fail");
  assert.equal(verdict.violations[0].rule_id, "SEARCH-NET-001");
  assert.deepEqual(verdict.affected_services, ["search"]);
});

test("deterministic demo engine passes unrelated changes", () => {
  const verdict = analyzeDeterministically("+  replication: regional", [contract], "infra-to-service");
  assert.equal(verdict.status, "pass");
  assert.equal(verdict.violations.length, 0);
});

test("verdict validation rejects inconsistent pass results", () => {
  assert.throws(() => validateVerdict({ status: "pass", risk: "none", summary: "x", affected_services: [], violations: [{}] }));
});

