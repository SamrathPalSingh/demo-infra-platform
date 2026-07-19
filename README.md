# Demo Shared Infrastructure Platform

A realistic but non-deploying infrastructure repository for the Infra-App Contract Testing POC. Every infrastructure pull request fans out across all application contracts in the shared registry and uses Gemini to produce a structured, auditable verdict.

## Demo architecture

```text
infra PR diff ──┐
                ├── Go contract-check ── Gemini ── PR comment + required check
registry repo ──┘                            │
                    affected owners ◀───────┘
```

This repository deliberately models cloud state as Terraform/YAML and never calls a cloud API or runs `terraform apply`.

## What the check does

1. Reads the exact base-to-head pull request diff.
2. Fetches `registry.json` and every service contract through the GitHub Contents API.
3. Sends only that declared ground truth plus the diff to Gemini with a strict JSON schema.
4. Validates the response, updates one durable PR comment, creates a named check-run, writes `.contract-report.json`, and exits non-zero on violations.

The model is instructed to ignore instructions embedded in diffs and not invent generic best-practice findings. This is still an AI-assisted POC; production should add policy-as-code checks for hard guarantees and human review for high-risk changes.

## One-time GitHub setup

Create and push the registry repository first. Then, from this directory:

```bash
git init
git add .
git commit -m "Initial infra contract POC"
gh repo create demo-infra-platform --public --source=. --remote=origin --push
```

Configure repository settings:

1. Add Actions secret `GEMINI_API_KEY` from Google AI Studio.
2. Add Actions secret `REGISTRY_WRITE_TOKEN`: a fine-grained GitHub token with `Contents: read and write` access to `demo-contract-registry`.
3. Public registries need no read token. For a private registry, also add `REGISTRY_READ_TOKEN` with read access.
4. If the repos do not share the same GitHub owner/name convention, set variable `CONTRACT_REGISTRY_REPO` to `OWNER/demo-contract-registry`.
5. Optionally set `GEMINI_MODEL`; it defaults to `gemini-3.5-flash`.
6. Under **Settings → Branches → Branch protection**, require the workflow job and/or `infra-app-contracts` check before merge.

The workflow uses `pull_request_target` but checks out only the trusted base commit, fetches the PR diff through the API, and never executes pull-request code. This supports fork PRs without handing untrusted code the Gemini or GitHub tokens. Do not change the workflow to check out the PR head while retaining secrets.

## Local verification without API spend

Go is the only language toolchain. The commands use only the standard library, so there are no modules to download:

```bash
go test ./...
go run ./cmd/contract-check --direction infra-to-service --diff fixtures/safe-infra.diff --registry-dir ../demo-contract-registry --engine deterministic --dry-run
go run ./cmd/contract-check --direction infra-to-service --diff fixtures/breaking-infra.diff --registry-dir ../demo-contract-registry --engine deterministic --dry-run
```

`demo:breaking` is expected to exit with status 1 and name the search and checkout owners. The deterministic engine exists only for repeatable fixtures and local development; `.github/workflows/contract-check.yml` explicitly selects Gemini.

To exercise Gemini locally:

```bash
export GEMINI_API_KEY="your-key"
go run ./cmd/contract-check \
  --direction infra-to-service \
  --diff fixtures/breaking-infra.diff \
  --registry-dir ../demo-contract-registry \
  --engine gemini \
  --dry-run
```

PowerShell equivalent:

```powershell
$env:GEMINI_API_KEY = "your-key"
go run ./cmd/contract-check --direction infra-to-service --diff fixtures/breaking-infra.diff --registry-dir ../demo-contract-registry --engine gemini --dry-run
```

Never commit the API key. Gemini structured outputs are schema-constrained, but the script also performs semantic consistency checks before trusting the verdict.

## Bad PR demo

Create a branch and change `infra/network-state.yaml` from `allocation_method: static` to `allocation_method: ephemeral`, removing the static addresses. Open a PR. The workflow should:

- block the PR;
- identify Search and Checkout as affected;
- cite their NAT rules and owners;
- recommend restoring static allocation.

## Contract publication

After changes land on `main`, `publish-contract.yml` combines `platform-invariants.md`, `platform-contract.json`, and the modeled state files, then commits the versioned platform contract to the registry through the GitHub Contents API.

## License

MIT
