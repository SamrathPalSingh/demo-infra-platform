# Shared platform infrastructure contract

## Contract metadata

- Schema version: `1.0`
- Kind: `platform-invariants`
- Contract ID: `shared-platform`
- Name: `Shared Kubernetes Platform`
- Owner: `platform-engineering`
- Repository: `demo-infra-platform`
- Contact: `#platform-engineering`

Severity is declared by the section containing each rule. Application pull requests are verified against these platform guarantees and constraints before merge. This Markdown file is the only hand-maintained contract source.

## Critical severity

### PLAT-ING-001 - Require the supported public ingress configuration

#### Requirements

- Public ingress must use TLS and the `public-nginx` ingress class.

#### Remediation

- Use `ingressClassName: public-nginx` and configure TLS.

#### Deterministic signals

- `ingressClassName:\s*(private-nginx|internal-nginx)`
- `tls:\s*disabled`

### PLAT-NET-001 - Preserve addresses required by published service contracts

#### Requirements

- The platform provides the stable NAT addresses `34.120.10.10` and `34.120.10.11` and stable ingress address `34.98.20.5` while a published service contract depends on them.

#### Remediation

- Preserve the published addresses or coordinate and complete consumer migrations before changing them.

#### Deterministic signals

- `allocation_method:\s*ephemeral`
- `nat_ip_mode:\s*dynamic`
- `address:\s*dynamic`

## High severity

### PLAT-STO-001 - Enforce the supported fast-ssd IOPS range

#### Requirements

- The `fast-ssd` StorageClass supports **1,000-5,000 provisioned IOPS** per claim.

#### Remediation

- Request between 1,000 and 5,000 IOPS or open a platform capacity request.

#### Deterministic signals

- `iops:\s*([6-9]\d{3}|[1-9]\d{4,})\b`
- `storage-iops:\s*([6-9]\d{3}|[1-9]\d{4,})\b`

### PLAT-DNS-001 - Require Kubernetes service discovery

#### Requirements

- Workloads must use Kubernetes DNS names, never hard-coded private ClusterIP addresses.

#### Remediation

- Replace private IP addresses with Kubernetes service DNS names.

#### Deterministic signals

- `(INVENTORY_URL|REDIS_URL):\s*https?://10\.`
- `value:\s*"?https?://10\.`
- `value:\s*"?10\.([0-9]{1,3}\.){2}[0-9]{1,3}`

## Medium severity

### PLAT-CPU-001 - Enforce per-container resource limits

#### Requirements

- A single container may request at most **4 CPU** and **8Gi memory** in this demo platform.

#### Remediation

- Reduce the container request to at most 4 CPU and 8Gi memory, or request a platform exception.

#### Deterministic signals

- `cpu:\s*"?([5-9]|[1-9]\d+)"?\s*$`
- `memory:\s*"?([9]\d*Gi|[1-9]\d+Gi)"?\s*$`

