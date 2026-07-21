# Demo Shared Infrastructure Platform

This repository owns modeled cluster infrastructure and the distributed
fan-out/fan-in contract gate. It does not apply cloud resources.

## What the infra workflow owns

On an infra PR, `.github/workflows/contract-check.yml` runs trusted code from
the PR base branch and starts `cmd/fanout-contract-test`.

The Go orchestrator:

1. reads `registry.json` and selects every service whose
   `contract_test.enabled` is true;
2. creates a correlation ID from the infra repository, PR number, and head SHA;
3. dispatches `infra_contract_test` to every selected service with the
   correlation, service ID, infra PR identity, registry location, and truncated
   PR title/body;
4. polls each target repository for the exact correlation-named result artifact;
5. downloads and validates every `result.json`;
6. aggregates all verdicts into `.contract-report.json` and one durable PR
   comment; and
7. exits non-zero if any service fails or a result is missing/invalid.

The full diff is not embedded in the dispatch payload. Each service fetches it
from GitHub using the PR identity, avoiding the `repository_dispatch` payload
size limit and making the source of truth unambiguous.

## Files that matter

- `infra/cluster-security-defaults.yaml` — current AppArmor/seccomp baseline
- `infra/network-state.yaml` and `infra/storage-class.yaml` — other demo state
- `platform-invariants.md` — platform-owned contract for service PR checks
- `cmd/fanout-contract-test` — distributed orchestrator and aggregator
- `cmd/publish-contract` — generates and publishes the platform contract
- `fixtures/enable-security-defaults.diff` — two-service fan-out demo

## GitHub configuration

Add these Actions secrets:

- `CONTRACT_FANOUT_TOKEN`: fine-grained token covering all POC repos with
  Contents read/write, Actions read, and Pull requests read.
- `REGISTRY_WRITE_TOKEN`: token with Contents read/write on the registry only.

Optional Actions variables:

- `CONTRACT_REGISTRY_REPO` if it is not
  `OWNER/demo-contract-registry`.
- `CONTRACT_REGISTRY_REF` if it is not `main`.
- `FANOUT_TIMEOUT_SECONDS` is read by the Go runner as an environment variable;
  the default is 600 seconds.

The workflow's built-in `GITHUB_TOKEN` writes only the aggregate PR comment.
The fan-out token is never exposed to PR code because `pull_request_target`
checks out the trusted base SHA and never checks out the head branch.

## Demo the AppArmor/seccomp break

After all service workflows are present on their default branches, create a new
infra branch from `main` and change:

```yaml
spec:
  seccomp_default: true
  apparmor_default: true
```

Open a PR. Search should accept the change. Node Inspector should reject it on
`NODE-SEC-001`, and this repository's aggregate `verify` job should block the
PR. The result comment links back to the two service workflow runs.

## Local checks

```powershell
go test ./...
```

The fan-in runner also has a local simulation mode:

```powershell
go run ./cmd/fanout-contract-test `
  --registry-dir ../demo-contract-registry `
  --results-dir PATH_TO_SERVICE_RESULTS `
  --dry-run
```

Files in the results directory must be named `search.json` and
`node-inspector.json`. This exercises registry discovery, correlation
validation, aggregation, Markdown rendering, and pass/fail behavior without
GitHub API calls.

## Contract publication

After a platform change lands on `main`, `publish-contract.yml` parses
`platform-invariants.md`, captures the modeled infra files including the
security-default baseline, and commits generated JSON to the registry. The
Markdown file remains the only hand-maintained platform contract.

## Adding more services

Do not edit the infra workflow. Give the new service an `infra_contract_test`
dispatch workflow and published contract, then add one enabled route to the
registry. The orchestrator discovers it automatically.

## License

MIT
